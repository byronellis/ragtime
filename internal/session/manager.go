package session

import (
	"log/slog"
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

	// Record the event
	session.RecordEvent(event.EventType, event.ToolName, resp.Context, resp.PermissionDecision)

	return resp
}
