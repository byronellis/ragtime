package fs

// ragtimeImpl implements the cgofuse FileSystem interface.
// The virtual tree is rebuilt on every path resolution so the filesystem
// always reflects live daemon/RAG state.

import (
	"encoding/json"
	"fmt"
	"strings"
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
	rfs *RagtimeFS
}

func (impl *ragtimeImpl) Init() {}

func (impl *ragtimeImpl) Getattr(path string, stat *fuse.Stat_t, fh uint64) int {
	e := impl.resolve(path)
	if e == nil {
		return -fuse.ENOENT
	}
	e.fillStat(impl.rfs, stat)
	return 0
}

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

func (impl *ragtimeImpl) Open(path string, flags int) (int, uint64) {
	e := impl.resolve(path)
	if e == nil {
		return -fuse.ENOENT, ^uint64(0)
	}
	if _, ok := e.(*fileEntry); !ok {
		return -fuse.EISDIR, ^uint64(0)
	}
	return 0, 0
}

func (impl *ragtimeImpl) Read(path string, buf []byte, ofst int64, fh uint64) int {
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

func (impl *ragtimeImpl) Getxattr(path string, name string) (int, []byte) {
	return -fuse.ENOTSUP, nil
}
func (impl *ragtimeImpl) Listxattr(path string, fill func(name string) bool) int { return 0 }
func (impl *ragtimeImpl) Setxattr(path string, name string, value []byte, flags int) int {
	return -fuse.ENOTSUP
}

// resolve walks the virtual tree to the entry at path.
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

type fileEntry struct {
	name  string
	mtime time.Time
	data  func(rfs *RagtimeFS) []byte
}

func (f *fileEntry) ename() string { return f.name }
func (f *fileEntry) fillStat(rfs *RagtimeFS, out *fuse.Stat_t) {
	out.Mode = fuse.S_IFREG | 0o444
	out.Nlink = 1
	out.Size = int64(len(f.data(rfs)))
	if !f.mtime.IsZero() {
		t := fuse.NewTimespec(f.mtime)
		out.Mtim, out.Ctim, out.Atim = t, t, t
	}
}

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

// jsonFile returns a fileEntry that marshals obj as indented JSON.
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

// ── collections/ ────────────────────────────────────────────────────────────

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

// ── agents/ ─────────────────────────────────────────────────────────────────

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
			}
		},
	}
}

func (impl *ragtimeImpl) outputLog(shellID string, mtime time.Time) *fileEntry {
	return &fileEntry{
		name:  "output.log",
		mtime: mtime,
		data: func(rfs *RagtimeFS) []byte {
			out, _ := rfs.daemon.captureShell(shellID)
			return []byte(out)
		},
	}
}
