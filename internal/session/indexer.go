package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/byronellis/ragtime/internal/project"
	"github.com/byronellis/ragtime/internal/protocol"
	"github.com/byronellis/ragtime/internal/rag"
)

type indexRequest struct {
	events  []Event
	event   *protocol.HookEvent
	session *Session
}

// sessionRef is an entry in the per-project session-refs.json file.
type sessionRef struct {
	Agent       string    `json:"agent"`
	SessionID   string    `json:"session_id"`
	LastUpdated time.Time `json:"last_updated"`
}

type sessionRefsFile struct {
	Sessions []sessionRef `json:"sessions"`
}

// SessionIndexer indexes session events into a global RAG collection in the background.
type SessionIndexer struct {
	provider    rag.EmbeddingProvider
	logger      *slog.Logger
	mu          sync.Mutex
	lastIndexed map[string]int // "agent:sessionID" -> last indexed event index
	queue       chan indexRequest
	done        chan struct{}
	col         *rag.Collection // cached collection handle, opened lazily
}

// NewSessionIndexer creates a new indexer.
func NewSessionIndexer(provider rag.EmbeddingProvider, logger *slog.Logger) *SessionIndexer {
	if logger == nil {
		logger = slog.Default()
	}
	return &SessionIndexer{
		provider:    provider,
		logger:      logger,
		lastIndexed: make(map[string]int),
		queue:       make(chan indexRequest, 64),
		done:        make(chan struct{}),
	}
}

// Start launches the background goroutine that processes index requests.
func (idx *SessionIndexer) Start() {
	go func() {
		defer close(idx.done)
		for req := range idx.queue {
			idx.processRequest(req)
		}
	}()
	idx.logger.Info("session indexer started")
}

// Stop closes the queue and waits for the background goroutine to drain.
func (idx *SessionIndexer) Stop() {
	close(idx.queue)
	<-idx.done
	idx.logger.Info("session indexer stopped")
}

// OnEvent is called from the bus subscriber. On user-prompt-submit or stop events,
// it computes the new event range and enqueues an index request.
func (idx *SessionIndexer) OnEvent(event *protocol.HookEvent, session *Session) {
	if event.EventType != "user-prompt-submit" && event.EventType != "stop" {
		return
	}

	key := event.Agent + ":" + event.SessionID
	idx.mu.Lock()
	from := idx.lastIndexed[key]
	idx.mu.Unlock()

	to := session.EventCount()
	if to <= from {
		return
	}

	events := session.EventsRange(from, to)
	if len(events) == 0 {
		return
	}

	req := indexRequest{
		events:  events,
		event:   event,
		session: session,
	}

	// Non-blocking send
	select {
	case idx.queue <- req:
		idx.mu.Lock()
		idx.lastIndexed[key] = to
		idx.mu.Unlock()
	default:
		idx.logger.Warn("session indexer queue full, dropping events",
			"agent", event.Agent,
			"session_id", event.SessionID,
			"events", len(events),
		)
	}
}

func (idx *SessionIndexer) processRequest(req indexRequest) {
	text := idx.formatEventsAsText(req.events, req.event)
	if text == "" {
		return
	}

	source := fmt.Sprintf("session/%s/%s", req.event.Agent, req.event.SessionID)
	metadata := map[string]string{
		"agent":      req.event.Agent,
		"session_id": req.event.SessionID,
		"project":    req.event.CWD,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
		"type":       "session",
	}

	// Chunk if content is large
	var chunks []rag.Chunk
	if len(text) > rag.DefaultChunkSize {
		chunks = rag.ChunkText(text, source, rag.DefaultChunkSize, rag.DefaultChunkOverlap)
	} else {
		chunks = []rag.Chunk{{
			ID:       fmt.Sprintf("session_%s_%s_%d", req.event.Agent, req.event.SessionID, time.Now().UnixMilli()),
			Content:  text,
			Source:   source,
			Metadata: metadata,
		}}
	}

	// Add metadata to chunked results
	for i := range chunks {
		if chunks[i].Metadata == nil {
			chunks[i].Metadata = metadata
		}
	}

	// Embed all chunks
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}

	vecs, err := idx.provider.Embed(context.Background(), texts)
	if err != nil {
		idx.logger.Error("session indexer: embed failed", "error", err)
		return
	}

	col, err := idx.getCollection()
	if err != nil {
		idx.logger.Error("session indexer: open collection", "error", err)
		return
	}

	if err := col.AppendChunksAndVectors(chunks, vecs); err != nil {
		idx.logger.Error("session indexer: append failed", "error", err)
		return
	}

	idx.logger.Debug("session indexed",
		"agent", req.event.Agent,
		"session_id", req.event.SessionID,
		"chunks", len(chunks),
	)

	// Write per-project reference
	if req.event.CWD != "" {
		idx.writeProjectRef(req.event.CWD, req.event.Agent, req.event.SessionID)
	}
}

// getCollection returns the cached sessions collection, opening or creating it on first use.
func (idx *SessionIndexer) getCollection() (*rag.Collection, error) {
	if idx.col != nil {
		return idx.col, nil
	}

	globalDir := project.GlobalDir()
	if globalDir == "" {
		return nil, fmt.Errorf("no global dir")
	}

	colDir := filepath.Join(globalDir, "indexes", "sessions")
	col, err := rag.OpenCollection(colDir)
	if err != nil {
		meta := rag.CollectionMeta{
			Name:       "sessions",
			Provider:   "ollama",
			Dimensions: idx.provider.Dimensions(),
		}
		col, err = rag.CreateCollection(colDir, meta)
		if err != nil {
			return nil, err
		}
	}

	idx.col = col
	return col, nil
}

func (idx *SessionIndexer) formatEventsAsText(events []Event, hookEvent *protocol.HookEvent) string {
	if len(events) == 0 {
		return ""
	}

	var b strings.Builder

	// Header with project context
	projectName := hookEvent.CWD
	if parts := strings.Split(projectName, "/"); len(parts) > 0 {
		projectName = parts[len(parts)-1]
	}

	date := events[0].Timestamp.Format("2006-01-02")
	timeRange := events[0].Timestamp.Format("15:04")
	if len(events) > 1 {
		timeRange += "-" + events[len(events)-1].Timestamp.Format("15:04")
	}

	fmt.Fprintf(&b, "Project: %s (%s)\nAgent: %s | Session: %s | Time: %s\n\n",
		projectName, hookEvent.CWD, hookEvent.Agent, hookEvent.SessionID, date+" "+timeRange)

	// Collect user prompts, file reads, writes, edits, commands into meaningful groups
	var prompts []string
	var reads []string
	var writes []string
	var edits []string
	var commands []string
	var searches []string
	var agents []string
	var denied []string
	hasContext := false

	seen := make(map[string]bool) // dedup file paths within this batch

	for _, e := range events {
		// Capture user prompts
		if e.EventType == "user-prompt-submit" && e.Detail != "" {
			prompts = append(prompts, e.Detail)
			continue
		}

		// Skip post-tool-use — pre-tool-use has the detail we need
		if e.EventType == "post-tool-use" {
			continue
		}

		if e.HasContext {
			hasContext = true
		}

		if e.Decision == "deny" {
			detail := e.ToolName
			if e.Detail != "" {
				detail += ": " + e.Detail
			}
			denied = append(denied, detail)
			continue
		}

		if e.EventType != "pre-tool-use" {
			continue
		}

		detail := e.Detail
		if detail == "" {
			continue
		}

		switch e.ToolName {
		case "Read":
			if !seen["r:"+detail] {
				reads = append(reads, detail)
				seen["r:"+detail] = true
			}
		case "Write":
			if !seen["w:"+detail] {
				writes = append(writes, detail)
				seen["w:"+detail] = true
			}
		case "Edit":
			if !seen["e:"+detail] {
				edits = append(edits, detail)
				seen["e:"+detail] = true
			}
		case "Bash":
			commands = append(commands, detail)
		case "Grep", "Glob":
			if !seen["s:"+detail] {
				searches = append(searches, detail)
				seen["s:"+detail] = true
			}
		case "Agent":
			agents = append(agents, detail)
		}
	}

	// Build narrative — user request first, then actions
	for _, p := range prompts {
		fmt.Fprintf(&b, "User: %s\n", p)
	}
	if len(prompts) > 0 {
		b.WriteString("\n")
	}
	if len(reads) > 0 {
		b.WriteString("Read: ")
		b.WriteString(strings.Join(reads, ", "))
		b.WriteString("\n")
	}
	if len(searches) > 0 {
		b.WriteString("Searched: ")
		b.WriteString(strings.Join(searches, "; "))
		b.WriteString("\n")
	}
	if len(edits) > 0 {
		b.WriteString("Edited: ")
		b.WriteString(strings.Join(edits, ", "))
		b.WriteString("\n")
	}
	if len(writes) > 0 {
		b.WriteString("Wrote: ")
		b.WriteString(strings.Join(writes, ", "))
		b.WriteString("\n")
	}
	if len(commands) > 0 {
		b.WriteString("Ran:\n")
		for _, cmd := range commands {
			fmt.Fprintf(&b, "  $ %s\n", cmd)
		}
	}
	if len(agents) > 0 {
		b.WriteString("Sub-agents: ")
		b.WriteString(strings.Join(agents, "; "))
		b.WriteString("\n")
	}
	if len(denied) > 0 {
		b.WriteString("Denied: ")
		b.WriteString(strings.Join(denied, ", "))
		b.WriteString("\n")
	}
	if hasContext {
		b.WriteString("Context was injected from RAG/rules\n")
	}

	result := b.String()
	// Skip empty turns (only header, no actions)
	if !strings.Contains(result, "\n\n") {
		return ""
	}
	lines := strings.Split(result, "\n")
	contentLines := 0
	for _, l := range lines {
		if l != "" && !strings.HasPrefix(l, "Project:") && !strings.HasPrefix(l, "Agent:") {
			contentLines++
		}
	}
	if contentLines == 0 {
		return ""
	}

	return result
}

func (idx *SessionIndexer) writeProjectRef(cwd, agent, sessionID string) {
	projDir := project.RagtimeDir(cwd)
	if projDir == "" {
		return
	}

	refsPath := filepath.Join(projDir, "indexes", "session-refs.json")

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(refsPath), 0o755); err != nil {
		idx.logger.Error("session indexer: create refs dir", "error", err)
		return
	}

	// Load existing refs
	var refs sessionRefsFile
	if data, err := os.ReadFile(refsPath); err == nil {
		_ = json.Unmarshal(data, &refs)
	}

	// Update or add entry
	now := time.Now().UTC()
	found := false
	for i, r := range refs.Sessions {
		if r.Agent == agent && r.SessionID == sessionID {
			refs.Sessions[i].LastUpdated = now
			found = true
			break
		}
	}
	if !found {
		refs.Sessions = append(refs.Sessions, sessionRef{
			Agent:       agent,
			SessionID:   sessionID,
			LastUpdated: now,
		})
	}

	data, err := json.MarshalIndent(&refs, "", "  ")
	if err != nil {
		idx.logger.Error("session indexer: marshal refs", "error", err)
		return
	}

	if err := os.WriteFile(refsPath, data, 0o644); err != nil {
		idx.logger.Error("session indexer: write refs", "error", err)
	}
}

