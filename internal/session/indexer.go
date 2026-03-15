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

	// Open or create the global sessions collection
	globalDir := project.GlobalDir()
	if globalDir == "" {
		idx.logger.Error("session indexer: no global dir")
		return
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
			idx.logger.Error("session indexer: create collection failed", "error", err)
			return
		}
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

func (idx *SessionIndexer) formatEventsAsText(events []Event, hookEvent *protocol.HookEvent) string {
	if len(events) == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Session: %s | Agent: %s | Project: %s\n",
		hookEvent.SessionID, hookEvent.Agent, hookEvent.CWD)
	b.WriteString("---\n")

	for _, e := range events {
		ts := e.Timestamp.Format("15:04:05")
		b.WriteString(ts)
		b.WriteString(" ")
		b.WriteString(e.EventType)

		if e.ToolName != "" {
			fmt.Fprintf(&b, " [%s]", e.ToolName)
		}
		if e.Decision != "" {
			fmt.Fprintf(&b, " decision=%s", e.Decision)
		}
		b.WriteString("\n")

		if e.Injected != "" {
			fmt.Fprintf(&b, "  context: %s\n", truncate(e.Injected, 500))
		}
	}

	return b.String()
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

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
