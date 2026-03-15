package session

import (
	"sync"
	"time"

	"github.com/byronellis/ragtime/internal/protocol"
)

// Event records a hook event in session history.
type Event struct {
	Timestamp time.Time                    `json:"timestamp"`
	EventType string                       `json:"event_type"`
	ToolName  string                       `json:"tool_name,omitempty"`
	Detail    string                       `json:"detail,omitempty"`
	Injected  string                       `json:"injected,omitempty"`
	Decision  protocol.PermissionDecision  `json:"decision,omitempty"`
}

// Session tracks state for a single agent conversation.
type Session struct {
	mu        sync.Mutex
	Agent     string            `json:"agent"`
	SessionID string            `json:"session_id"`
	StartedAt time.Time         `json:"started_at"`
	Events    []Event           `json:"events"`
	State     map[string]string `json:"state"`
	injected  map[uint64]bool   // content hashes for dedup
}

// NewSession creates a new session.
func NewSession(agent, sessionID string) *Session {
	return &Session{
		Agent:     agent,
		SessionID: sessionID,
		StartedAt: time.Now(),
		State:     make(map[string]string),
		injected:  make(map[uint64]bool),
	}
}

// RecordEvent adds an event to the session history.
func (s *Session) RecordEvent(eventType, toolName, detail, injected string, decision protocol.PermissionDecision) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Events = append(s.Events, Event{
		Timestamp: time.Now(),
		EventType: eventType,
		ToolName:  toolName,
		Detail:    detail,
		Injected:  injected,
		Decision:  decision,
	})
}

// IsDuplicate checks if content has already been injected in this session.
func (s *Session) IsDuplicate(content string) bool {
	if content == "" {
		return false
	}
	h := fnvHash(content)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.injected[h] {
		return true
	}
	s.injected[h] = true
	return false
}

// Get reads a session-scoped value.
func (s *Session) Get(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.State[key]
}

// Set writes a session-scoped value.
func (s *Session) Set(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.State[key] = value
}

// EventCount returns the number of recorded events.
func (s *Session) EventCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.Events)
}

// EventsRange returns a copy of events in the range [from, to), clamped to bounds.
func (s *Session) EventsRange(from, to int) []Event {
	s.mu.Lock()
	defer s.mu.Unlock()

	if from < 0 {
		from = 0
	}
	if to > len(s.Events) {
		to = len(s.Events)
	}
	if from >= to {
		return nil
	}

	result := make([]Event, to-from)
	copy(result, s.Events[from:to])
	return result
}

// RecentEvents returns the last n events.
func (s *Session) RecentEvents(n int) []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n <= 0 || n > len(s.Events) {
		n = len(s.Events)
	}
	start := len(s.Events) - n
	result := make([]Event, n)
	copy(result, s.Events[start:])
	return result
}

// fnvHash is a simple FNV-1a hash for deduplication.
func fnvHash(s string) uint64 {
	var h uint64 = 14695981039346656037
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}
