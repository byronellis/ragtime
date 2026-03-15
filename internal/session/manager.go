package session

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"

	"github.com/byronellis/ragtime/internal/protocol"
)

// Manager tracks active sessions across agent conversations.
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session // key: "agent:session_id"
	logger   *slog.Logger
}

// NewManager creates a new session manager.
func NewManager(logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		sessions: make(map[string]*Session),
		logger:   logger,
	}
}

// GetOrCreate returns an existing session or creates a new one.
func (m *Manager) GetOrCreate(agent, sessionID string) *Session {
	key := agent + ":" + sessionID
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.sessions[key]; ok {
		return s
	}

	s := NewSession(agent, sessionID)
	m.sessions[key] = s
	m.logger.Info("new session", "agent", agent, "session_id", sessionID)
	return s
}

// Get returns a session if it exists.
func (m *Manager) Get(agent, sessionID string) *Session {
	key := agent + ":" + sessionID
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[key]
}

// List returns all active sessions.
func (m *Manager) List() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		result = append(result, s)
	}
	return result
}

// ProcessEvent handles a hook event: tracks it in the session and applies dedup.
// Returns the response with duplicate context removed.
func (m *Manager) ProcessEvent(event *protocol.HookEvent, resp *protocol.HookResponse) *protocol.HookResponse {
	if event.SessionID == "" {
		return resp
	}

	session := m.GetOrCreate(event.Agent, event.SessionID)

	// Check for duplicate context injection
	if resp.Context != "" && session.IsDuplicate(resp.Context) {
		m.logger.Debug("dedup: skipping duplicate context",
			"session", event.SessionID,
			"tool", event.ToolName,
		)
		resp = &protocol.HookResponse{
			PermissionDecision: resp.PermissionDecision,
			DenyReason:         resp.DenyReason,
			// Context cleared — it was already injected earlier
		}
	}

	// Extract a meaningful detail string from tool input
	detail := extractDetail(event)

	// Record the event
	session.RecordEvent(event.EventType, event.ToolName, detail, resp.Context, resp.PermissionDecision)

	return resp
}

// extractDetail pulls the most relevant info from a hook event's tool input
// to produce a short, human-readable description of what the tool did.
func extractDetail(event *protocol.HookEvent) string {
	if len(event.ToolInput) == 0 {
		return ""
	}

	switch event.ToolName {
	case "Read":
		return shortPath(strField(event.ToolInput, "file_path"))

	case "Write":
		return shortPath(strField(event.ToolInput, "file_path"))

	case "Edit":
		return shortPath(strField(event.ToolInput, "file_path"))

	case "Bash":
		cmd := strField(event.ToolInput, "command")
		if len(cmd) > 200 {
			cmd = cmd[:200] + "..."
		}
		return cmd

	case "Grep":
		pattern := strField(event.ToolInput, "pattern")
		path := strField(event.ToolInput, "path")
		if path != "" {
			return fmt.Sprintf("%q in %s", pattern, shortPath(path))
		}
		return fmt.Sprintf("%q", pattern)

	case "Glob":
		return strField(event.ToolInput, "pattern")

	case "Agent":
		desc := strField(event.ToolInput, "description")
		if desc != "" {
			return desc
		}
		return strField(event.ToolInput, "prompt")

	case "WebSearch":
		return strField(event.ToolInput, "query")

	case "WebFetch":
		return strField(event.ToolInput, "url")

	default:
		// For unknown tools, try common field names
		for _, key := range []string{"file_path", "path", "command", "query", "description"} {
			if v := strField(event.ToolInput, key); v != "" {
				return shortPath(v)
			}
		}
		return ""
	}
}

// strField extracts a string field from a map[string]any.
func strField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return s
}

// shortPath reduces an absolute path to the last 3 components for readability.
func shortPath(path string) string {
	if path == "" {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) <= 3 {
		return path
	}
	return strings.Join(parts[len(parts)-3:], "/")
}
