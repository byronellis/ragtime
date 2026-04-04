package fs

// ragtimeImpl implements the cgofuse FileSystem interface.
// The virtual tree is rebuilt on every path resolution so the filesystem
// always reflects live daemon/RAG state.

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/byronellis/ragtime/internal/protocol"
	"github.com/byronellis/ragtime/internal/rag"
	"github.com/winfsp/cgofuse/fuse"
)

// ---------------------------------------------------------------------------
// ragtimeImpl (cgofuse entry point)
// ---------------------------------------------------------------------------

type ragtimeImpl struct {
	fuse.FileSystemBase
	rfs    *RagtimeFS
	notes  *notesStore
	fhSeq  atomic.Uint64
	// fh -> *shellWatcher (for output.log handles)
	watchers sync.Map
	// fh -> noteHandle (for notes/ file handles)
	noteHandles sync.Map
}

type noteHandle struct {
	shellID string
	name    string
}

func (impl *ragtimeImpl) Init() {}

// ---------------------------------------------------------------------------
// Stat
// ---------------------------------------------------------------------------

func (impl *ragtimeImpl) Getattr(path string, stat *fuse.Stat_t, fh uint64) int {
	e := impl.resolve(path)
	if e == nil {
		return -fuse.ENOENT
	}
	e.fillStat(impl.rfs, stat)
	return 0
}

// ---------------------------------------------------------------------------
// Directory operations
// ---------------------------------------------------------------------------

func (impl *ragtimeImpl) Readdir(path string,
	fill func(name string, stat *fuse.Stat_t, ofst int64) bool,
	ofst int64, fh uint64) int {

	e := impl.resolve(path)
	if e == nil {
		return -fuse.ENOENT
	}
	d, ok := e.(*dirEntry)
	if !ok {
		return -fuse.ENOTDIR
	}
	fill(".", nil, 0)
	fill("..", nil, 0)
	for _, child := range d.kids(impl.rfs) {
		var st fuse.Stat_t
		child.fillStat(impl.rfs, &st)
		fill(child.ename(), &st, 0)
	}
	return 0
}

// ---------------------------------------------------------------------------
// File open / read / release
// ---------------------------------------------------------------------------

func (impl *ragtimeImpl) Open(path string, flags int) (int, uint64) {
	e := impl.resolve(path)
	if e == nil {
		return -fuse.ENOENT, ^uint64(0)
	}

	switch node := e.(type) {
	case *fileEntry:
		if node.streaming {
			// Live output.log — open a shell watcher
			fh := impl.fhSeq.Add(1)
			w, err := newShellWatcher(impl.rfs.daemon.socketPath, node.shellID, 220, 50)
			if err != nil {
				// Fall back to static read if shell is gone
				return 0, 0
			}
			impl.watchers.Store(fh, w)
			return 0, fh
		}
		return 0, 0

	case *writeOnlyEntry:
		_ = node
		return 0, 0

	default:
		return -fuse.EISDIR, ^uint64(0)
	}
}

func (impl *ragtimeImpl) Read(path string, buf []byte, ofst int64, fh uint64) int {
	// Check if this is a streaming handle
	if w, ok := impl.watchers.Load(fh); ok {
		return w.(*shellWatcher).Read(buf, ofst)
	}

	e := impl.resolve(path)
	if e == nil {
		return -fuse.ENOENT
	}
	f, ok := e.(*fileEntry)
	if !ok {
		return -fuse.EISDIR
	}
	data := f.data(impl.rfs)
	if ofst >= int64(len(data)) {
		return 0
	}
	return copy(buf, data[ofst:])
}

func (impl *ragtimeImpl) Release(path string, fh uint64) int {
	if w, ok := impl.watchers.LoadAndDelete(fh); ok {
		w.(*shellWatcher).Close()
	}
	if _, ok := impl.noteHandles.LoadAndDelete(fh); ok {
		// Note handle closed without explicit flush — finalise
		impl.finaliseNote(path, fh)
	}
	return 0
}

// ---------------------------------------------------------------------------
// Write support (input file + notes/)
// ---------------------------------------------------------------------------

func (impl *ragtimeImpl) Write(path string, buf []byte, ofst int64, fh uint64) int {
	// notes/ write
	if nh, ok := impl.noteHandles.Load(fh); ok {
		h := nh.(noteHandle)
		impl.notes.write(h.shellID, h.name, buf, ofst)
		return len(buf)
	}

	// agents/<id>/input write
	e := impl.resolve(path)
	if e == nil {
		return -fuse.ENOENT
	}
	wo, ok := e.(*writeOnlyEntry)
	if !ok {
		return -fuse.EBADF
	}
	if err := impl.rfs.daemon.sendToShell(wo.shellID, buf); err != nil {
		return -fuse.EIO
	}
	return len(buf)
}

// ---------------------------------------------------------------------------
// notes/ directory — Create / Unlink / Read note files
// ---------------------------------------------------------------------------

func (impl *ragtimeImpl) Create(path string, flags int, mode uint32) (int, uint64) {
	shellID, name := parseNotePath(path)
	if shellID == "" {
		return -fuse.EPERM, ^uint64(0)
	}
	impl.notes.write(shellID, name, nil, 0) // create empty
	fh := impl.fhSeq.Add(1)
	impl.noteHandles.Store(fh, noteHandle{shellID: shellID, name: name})
	return 0, fh
}

func (impl *ragtimeImpl) Unlink(path string) int {
	shellID, name := parseNotePath(path)
	if shellID == "" {
		return -fuse.EPERM
	}
	impl.notes.delete(shellID, name)
	return 0
}

func (impl *ragtimeImpl) Truncate(path string, size int64, fh uint64) int {
	if nh, ok := impl.noteHandles.Load(fh); ok {
		h := nh.(noteHandle)
		impl.notes.truncate(h.shellID, h.name, size)
		return 0
	}
	return -fuse.EPERM
}

func (impl *ragtimeImpl) Flush(path string, fh uint64) int {
	if nh, ok := impl.noteHandles.Load(fh); ok {
		h := nh.(noteHandle)
		impl.notes.finalise(h.shellID, h.name, impl.rfs.rag)
		return 0
	}
	return 0
}

// finaliseNote indexes a note from path context (used in Release fallback).
func (impl *ragtimeImpl) finaliseNote(path string, fh uint64) {
	shellID, name := parseNotePath(path)
	if shellID != "" {
		impl.notes.finalise(shellID, name, impl.rfs.rag)
	}
}

// parseNotePath parses "agents/<shellID>/notes/<name>" and returns (shellID, name).
func parseNotePath(path string) (shellID, name string) {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	// agents/<id>/notes/<name>  or  shells/<id>/notes/<name>
	if len(parts) == 4 && (parts[0] == "agents" || parts[0] == "shells") && parts[2] == "notes" {
		return parts[1], parts[3]
	}
	return "", ""
}

// ---------------------------------------------------------------------------
// Symlink
// ---------------------------------------------------------------------------

func (impl *ragtimeImpl) Readlink(path string) (int, string) {
	e := impl.resolve(path)
	if e == nil {
		return -fuse.ENOENT, ""
	}
	sl, ok := e.(*linkEntry)
	if !ok {
		return -fuse.EINVAL, ""
	}
	return 0, sl.target
}

// ---------------------------------------------------------------------------
// Xattr stubs (suppress ENOSYS)
// ---------------------------------------------------------------------------

func (impl *ragtimeImpl) Getxattr(path string, name string) (int, []byte) {
	return -fuse.ENOTSUP, nil
}
func (impl *ragtimeImpl) Listxattr(path string, fill func(name string) bool) int { return 0 }
func (impl *ragtimeImpl) Setxattr(path string, name string, value []byte, flags int) int {
	return -fuse.ENOTSUP
}

// ---------------------------------------------------------------------------
// Path resolution — walks the virtual tree
// ---------------------------------------------------------------------------

func (impl *ragtimeImpl) resolve(path string) entry {
	root := impl.root()
	if path == "/" {
		return root
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	var cur entry = root
	for _, part := range parts {
		d, ok := cur.(*dirEntry)
		if !ok {
			return nil
		}
		var found entry
		for _, child := range d.kids(impl.rfs) {
			if child.ename() == part {
				found = child
				break
			}
		}
		if found == nil {
			return nil
		}
		cur = found
	}
	return cur
}

// ---------------------------------------------------------------------------
// Virtual tree node types
// ---------------------------------------------------------------------------

type entry interface {
	ename() string
	fillStat(rfs *RagtimeFS, out *fuse.Stat_t)
}

// dirEntry: a directory with dynamically-produced children.
type dirEntry struct {
	name  string
	mtime time.Time
	kids  func(rfs *RagtimeFS) []entry
}

func (d *dirEntry) ename() string { return d.name }
func (d *dirEntry) fillStat(_ *RagtimeFS, out *fuse.Stat_t) {
	out.Mode = fuse.S_IFDIR | 0o555
	out.Nlink = 2
	if !d.mtime.IsZero() {
		t := fuse.NewTimespec(d.mtime)
		out.Mtim, out.Ctim, out.Atim = t, t, t
	}
}

// writableDirEntry: a directory that allows creates (notes/).
type writableDirEntry struct {
	dirEntry
}

func (d *writableDirEntry) fillStat(rfs *RagtimeFS, out *fuse.Stat_t) {
	d.dirEntry.fillStat(rfs, out)
	out.Mode = fuse.S_IFDIR | 0o755
}

// fileEntry: a regular read-only file with lazily-computed content.
type fileEntry struct {
	name      string
	mtime     time.Time
	data      func(rfs *RagtimeFS) []byte
	streaming bool   // true for output.log — uses shellWatcher
	shellID   string // set when streaming=true
}

func (f *fileEntry) ename() string { return f.name }
func (f *fileEntry) fillStat(rfs *RagtimeFS, out *fuse.Stat_t) {
	out.Mode = fuse.S_IFREG | 0o444
	out.Nlink = 1
	if f.streaming {
		out.Size = 1<<62 - 1 // report large size so tail -f doesn't stop early
	} else {
		out.Size = int64(len(f.data(rfs)))
	}
	if !f.mtime.IsZero() {
		t := fuse.NewTimespec(f.mtime)
		out.Mtim, out.Ctim, out.Atim = t, t, t
	}
}

// writeOnlyEntry: the agents/<id>/input write-only file.
type writeOnlyEntry struct {
	name    string
	shellID string
}

func (w *writeOnlyEntry) ename() string { return w.name }
func (w *writeOnlyEntry) fillStat(_ *RagtimeFS, out *fuse.Stat_t) {
	out.Mode = fuse.S_IFREG | 0o222
	out.Nlink = 1
}

// noteFileEntry: a file inside a notes/ directory.
type noteFileEntry struct {
	shellID string
	name    string
	impl    *ragtimeImpl
}

func (n *noteFileEntry) ename() string { return n.name }
func (n *noteFileEntry) fillStat(_ *RagtimeFS, out *fuse.Stat_t) {
	content := n.impl.notes.get(n.shellID, n.name)
	out.Mode = fuse.S_IFREG | 0o644
	out.Nlink = 1
	out.Size = int64(len(content))
	t := fuse.NewTimespec(time.Now())
	out.Mtim, out.Ctim, out.Atim = t, t, t
}

// linkEntry: a symbolic link.
type linkEntry struct {
	name   string
	target string
}

func (l *linkEntry) ename() string { return l.name }
func (l *linkEntry) fillStat(_ *RagtimeFS, out *fuse.Stat_t) {
	out.Mode = fuse.S_IFLNK | 0o777
	out.Nlink = 1
	out.Size = int64(len(l.target))
}

// jsonFile: a fileEntry that marshals obj as indented JSON.
func jsonFile(name string, mtime time.Time, obj func(rfs *RagtimeFS) any) *fileEntry {
	return &fileEntry{
		name:  name,
		mtime: mtime,
		data: func(rfs *RagtimeFS) []byte {
			v := obj(rfs)
			b, _ := json.MarshalIndent(v, "", "  ")
			return append(b, '\n')
		},
	}
}

// ---------------------------------------------------------------------------
// Tree construction
// ---------------------------------------------------------------------------

func (impl *ragtimeImpl) root() *dirEntry {
	return &dirEntry{
		name: "",
		kids: func(rfs *RagtimeFS) []entry {
			return []entry{
				impl.activeDir(),
				impl.sessionsDir(),
				impl.collectionsDir(),
				impl.agentsDir(),
				&linkEntry{name: "shells", target: "agents"},
			}
		},
	}
}

// ── sessions/ ───────────────────────────────────────────────────────────────

func (impl *ragtimeImpl) sessionsDir() *dirEntry {
	return &dirEntry{
		name: "sessions",
		kids: func(rfs *RagtimeFS) []entry {
			sessions, _ := rfs.daemon.listSessions()
			entries := make([]entry, len(sessions))
			for i, s := range sessions {
				s := s
				entries[i] = impl.sessionDir(s)
			}
			return entries
		},
	}
}

func (impl *ragtimeImpl) sessionDir(s sessionSummary) *dirEntry {
	return &dirEntry{
		name:  fmt.Sprintf("%s-%s", s.Agent, s.SessionID),
		mtime: s.StartedAt,
		kids: func(rfs *RagtimeFS) []entry {
			return []entry{
				jsonFile("info.json", s.StartedAt, func(_ *RagtimeFS) any { return s }),
				impl.sessionEventsJSON(s),
				impl.sessionHistoryTxt(s),
			}
		},
	}
}

func (impl *ragtimeImpl) sessionEventsJSON(s sessionSummary) *fileEntry {
	return &fileEntry{
		name:  "events.json",
		mtime: s.StartedAt,
		data: func(rfs *RagtimeFS) []byte {
			events, _ := rfs.daemon.sessionHistory(s.Agent, s.SessionID, 1000)
			b, _ := json.MarshalIndent(events, "", "  ")
			return append(b, '\n')
		},
	}
}

func (impl *ragtimeImpl) sessionHistoryTxt(s sessionSummary) *fileEntry {
	return &fileEntry{
		name:  "history.txt",
		mtime: s.StartedAt,
		data: func(rfs *RagtimeFS) []byte {
			events, _ := rfs.daemon.sessionHistory(s.Agent, s.SessionID, 1000)
			return formatHistory(s, events)
		},
	}
}

func formatHistory(s sessionSummary, events []map[string]any) []byte {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Session: %s / %s\n", s.Agent, s.SessionID)
	fmt.Fprintf(&sb, "Started: %s\n", s.StartedAt.Format(time.RFC3339))
	fmt.Fprintf(&sb, "Events:  %d\n\n", s.EventCount)
	sb.WriteString(strings.Repeat("─", 60) + "\n")
	for _, ev := range events {
		ts, _ := ev["timestamp"].(string)
		evType, _ := ev["event_type"].(string)
		tool, _ := ev["tool_name"].(string)
		detail, _ := ev["detail"].(string)
		decision, _ := ev["decision"].(string)

		line := ts + "  " + evType
		if tool != "" {
			line += " [" + tool + "]"
		}
		if detail != "" {
			if len(detail) > 80 {
				detail = detail[:77] + "..."
			}
			line += "  " + detail
		}
		if decision != "" {
			line += "  → " + decision
		}
		sb.WriteString(line + "\n")
	}
	return []byte(sb.String())
}

// ── collections/ ─────────────────────────────────────────────────────────────

func (impl *ragtimeImpl) collectionsDir() *dirEntry {
	return &dirEntry{
		name: "collections",
		kids: func(rfs *RagtimeFS) []entry {
			if rfs.rag == nil {
				return nil
			}
			cols, _ := rfs.rag.ListCollections()
			entries := make([]entry, len(cols))
			for i, c := range cols {
				c := c
				entries[i] = impl.collectionDir(c)
			}
			return entries
		},
	}
}

func (impl *ragtimeImpl) collectionDir(c rag.CollectionMeta) *dirEntry {
	return &dirEntry{
		name: c.Name,
		kids: func(rfs *RagtimeFS) []entry {
			return []entry{
				jsonFile("meta.json", time.Time{}, func(_ *RagtimeFS) any { return c }),
				impl.chunksFile(c.Name),
			}
		},
	}
}

func (impl *ragtimeImpl) chunksFile(name string) *fileEntry {
	return &fileEntry{
		name: "chunks.txt",
		data: func(rfs *RagtimeFS) []byte {
			if rfs.rag == nil {
				return nil
			}
			results, err := rfs.rag.Search(name, " ", 10000)
			if err != nil {
				return []byte("(error: " + err.Error() + ")\n")
			}
			var sb strings.Builder
			for _, r := range results {
				fmt.Fprintf(&sb, "─── %s ───\n%s\n\n", r.Source, r.Content)
			}
			return []byte(sb.String())
		},
	}
}

// ── agents/ ──────────────────────────────────────────────────────────────────

func (impl *ragtimeImpl) agentsDir() *dirEntry {
	return &dirEntry{
		name: "agents",
		kids: func(rfs *RagtimeFS) []entry {
			shells, _ := rfs.daemon.listShells()
			entries := make([]entry, len(shells))
			for i, s := range shells {
				s := s
				entries[i] = impl.agentDir(s)
			}
			return entries
		},
	}
}

func (impl *ragtimeImpl) agentDir(s protocol.ShellInfo) *dirEntry {
	return &dirEntry{
		name:  s.ID,
		mtime: s.StartedAt,
		kids: func(rfs *RagtimeFS) []entry {
			return []entry{
				jsonFile("info.json", s.StartedAt, func(_ *RagtimeFS) any { return s }),
				impl.outputLog(s.ID, s.StartedAt),
				&writeOnlyEntry{name: "input", shellID: s.ID},
				impl.notesDir(s.ID),
			}
		},
	}
}

func (impl *ragtimeImpl) outputLog(shellID string, mtime time.Time) *fileEntry {
	return &fileEntry{
		name:      "output.log",
		mtime:     mtime,
		streaming: true,
		shellID:   shellID,
		// data is used only as fallback when streaming attach fails
		data: func(rfs *RagtimeFS) []byte {
			out, _ := rfs.daemon.captureShell(shellID)
			return []byte(out)
		},
	}
}

// ── active/ ──────────────────────────────────────────────────────────────────

func (impl *ragtimeImpl) activeDir() *dirEntry {
	return &dirEntry{
		name: "active",
		kids: func(rfs *RagtimeFS) []entry {
			sessions, _ := rfs.daemon.listActiveSessions()
			entries := make([]entry, len(sessions))
			for i, s := range sessions {
				s := s
				entries[i] = impl.activeSessionDir(s)
			}
			return entries
		},
	}
}

func (impl *ragtimeImpl) activeSessionDir(s map[string]any) *dirEntry {
	agent, _ := s["agent"].(string)
	sessionID, _ := s["session_id"].(string)
	shellID, _ := s["shell_id"].(string)

	kids := func(rfs *RagtimeFS) []entry {
		entries := []entry{
			impl.activeSummaryTxt(s),
			impl.activeStatusJSON(s),
		}
		if shellID != "" {
			// Symlinks into agents/ for live terminal access
			rel := "../../agents/" + shellID
			entries = append(entries,
				&linkEntry{name: "output.log", target: rel + "/output.log"},
				&linkEntry{name: "input", target: rel + "/input"},
			)
		}
		return entries
	}

	return &dirEntry{
		name: fmt.Sprintf("%s-%s", agent, sessionID),
		kids: kids,
	}
}

func (impl *ragtimeImpl) activeSummaryTxt(s map[string]any) *fileEntry {
	return &fileEntry{
		name: "summary.txt",
		data: func(_ *RagtimeFS) []byte { return formatActiveSummary(s) },
	}
}

func (impl *ragtimeImpl) activeStatusJSON(s map[string]any) *fileEntry {
	return &fileEntry{
		name: "status.json",
		data: func(_ *RagtimeFS) []byte {
			b, _ := json.MarshalIndent(s, "", "  ")
			return append(b, '\n')
		},
	}
}

func formatActiveSummary(s map[string]any) []byte {
	var sb strings.Builder
	agent, _ := s["agent"].(string)
	sessionID, _ := s["session_id"].(string)
	model, _ := s["model"].(string)
	cwd, _ := s["cwd"].(string)
	shellID, _ := s["shell_id"].(string)
	shellState, _ := s["shell_state"].(string)

	startedStr, _ := s["started_at"].(string)
	started, _ := time.Parse(time.RFC3339Nano, startedStr)
	lastEventStr, _ := s["last_event"].(string)
	lastEvent, _ := time.Parse(time.RFC3339Nano, lastEventStr)

	eventCount, _ := s["event_count"].(float64)
	numTurns, _ := s["num_turns"].(float64)
	costUSD, _ := s["cost_usd"].(float64)
	inputTok, _ := s["input_tokens"].(float64)
	outputTok, _ := s["output_tokens"].(float64)
	cacheCreate, _ := s["cache_create_tokens"].(float64)
	cacheRead, _ := s["cache_read_tokens"].(float64)

	now := time.Now()

	fmt.Fprintf(&sb, "Session:  %s / %s\n", agent, sessionID)
	if model != "" {
		fmt.Fprintf(&sb, "Model:    %s\n", model)
	}
	if !started.IsZero() {
		fmt.Fprintf(&sb, "Started:  %s  (%s ago)\n",
			started.Format("2006-01-02 15:04:05"),
			formatDuration(now.Sub(started)))
	}
	if !lastEvent.IsZero() {
		fmt.Fprintf(&sb, "Active:   %d events, last %s ago\n",
			int(eventCount), formatDuration(now.Sub(lastEvent)))
	}
	if cwd != "" {
		fmt.Fprintf(&sb, "CWD:      %s\n", cwd)
	}

	sb.WriteString("\n── Cost & Tokens ")
	sb.WriteString(strings.Repeat("─", 44) + "\n")
	if costUSD > 0 {
		fmt.Fprintf(&sb, "Cost:     $%.4f\n", costUSD)
	}
	if numTurns > 0 {
		fmt.Fprintf(&sb, "Turns:    %d\n", int(numTurns))
	}
	if inputTok > 0 || outputTok > 0 {
		fmt.Fprintf(&sb, "Input:    %s tokens\n", formatTokens(int(inputTok)))
		fmt.Fprintf(&sb, "Output:   %s tokens\n", formatTokens(int(outputTok)))
	}
	if cacheCreate > 0 || cacheRead > 0 {
		fmt.Fprintf(&sb, "Cache:    %s created / %s read\n",
			formatTokens(int(cacheCreate)), formatTokens(int(cacheRead)))
		// Context window estimate: input + cache_read is what the model actually sees
		contextUsed := int(inputTok) + int(cacheRead)
		fmt.Fprintf(&sb, "Context:  ~%s tokens in window\n", formatTokens(contextUsed))
	}

	if shellID != "" {
		sb.WriteString("\n── Shell ")
		sb.WriteString(strings.Repeat("─", 51) + "\n")
		fmt.Fprintf(&sb, "Shell ID: %s", shellID)
		if shellState != "" {
			fmt.Fprintf(&sb, "  (%s)", shellState)
		}
		sb.WriteString("\n")
		fmt.Fprintf(&sb, "          output.log / input available in this directory\n")
	}

	return []byte(sb.String())
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh%dm", h, m)
}

func formatTokens(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func (impl *ragtimeImpl) notesDir(shellID string) *writableDirEntry {
	return &writableDirEntry{dirEntry: dirEntry{
		name: "notes",
		kids: func(rfs *RagtimeFS) []entry {
			names := impl.notes.list(shellID)
			entries := make([]entry, len(names))
			for i, n := range names {
				n := n
				entries[i] = &noteFileEntry{shellID: shellID, name: n, impl: impl}
			}
			return entries
		},
	}}
}
