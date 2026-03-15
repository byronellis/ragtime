package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/byronellis/ragtime/internal/protocol"
)

func TestSessionsPanelEmpty(t *testing.T) {
	p := NewSessionsPanel()
	p.SetSize(80, 5)

	if p.Count() != 0 {
		t.Errorf("Count = %d, want 0", p.Count())
	}

	view := p.View()
	if !strings.Contains(view, "No active sessions") {
		t.Errorf("empty panel should show 'No active sessions', got: %s", view)
	}
}

func TestSessionsPanelInitSessions(t *testing.T) {
	p := NewSessionsPanel()
	p.SetSize(100, 10)

	infos := []protocol.SessionInfo{
		{Agent: "claude", SessionID: "s1", StartedAt: time.Now(), EventCount: 5},
		{Agent: "gemini", SessionID: "s2", StartedAt: time.Now(), EventCount: 3},
	}
	p.InitSessions(infos)

	if p.Count() != 2 {
		t.Errorf("Count = %d, want 2", p.Count())
	}

	view := p.View()
	if !strings.Contains(view, "claude") {
		t.Error("should contain 'claude'")
	}
	if !strings.Contains(view, "gemini") {
		t.Error("should contain 'gemini'")
	}
}

func TestSessionsPanelUpdateFromHookEvent(t *testing.T) {
	p := NewSessionsPanel()
	p.SetSize(100, 10)

	event := protocol.StreamEvent{
		Kind:      "hook_event",
		Timestamp: time.Now(),
		Event: &protocol.HookEvent{
			Agent:     "claude",
			SessionID: "new-sess",
			EventType: "pre-tool-use",
			ToolName:  "Read",
			CWD:       "/home/user/myproject",
		},
	}

	p.Update(event)

	if p.Count() != 1 {
		t.Fatalf("Count = %d, want 1", p.Count())
	}

	view := p.View()
	if !strings.Contains(view, "claude") {
		t.Error("should contain 'claude'")
	}
	if !strings.Contains(view, "myproject") {
		t.Error("should contain project name")
	}
}

func TestSessionsPanelUpdateFromSessionInfo(t *testing.T) {
	p := NewSessionsPanel()
	p.SetSize(100, 10)

	event := protocol.StreamEvent{
		Kind:      "session_update",
		Timestamp: time.Now(),
		Session: &protocol.SessionInfo{
			Agent:      "claude",
			SessionID:  "s1",
			StartedAt:  time.Now(),
			EventCount: 42,
			LastEvent:  time.Now(),
		},
	}

	p.Update(event)

	if p.Count() != 1 {
		t.Fatalf("Count = %d, want 1", p.Count())
	}

	view := p.View()
	if !strings.Contains(view, "42") {
		t.Error("should show event count")
	}
}

func TestSessionsPanelUpdateIncrements(t *testing.T) {
	p := NewSessionsPanel()
	p.SetSize(100, 10)

	// Two events for the same session should increment, not create duplicate
	for i := 0; i < 5; i++ {
		p.Update(protocol.StreamEvent{
			Kind:      "hook_event",
			Timestamp: time.Now(),
			Event: &protocol.HookEvent{
				Agent:     "claude",
				SessionID: "s1",
				EventType: "pre-tool-use",
			},
		})
	}

	if p.Count() != 1 {
		t.Errorf("Count = %d, want 1 (same session)", p.Count())
	}

	// Check event count is 5
	entry := p.sessions["claude:s1"]
	if entry.info.EventCount != 5 {
		t.Errorf("EventCount = %d, want 5", entry.info.EventCount)
	}
}

func TestSessionsPanelUpdateNilEvent(t *testing.T) {
	p := NewSessionsPanel()
	p.SetSize(100, 10)

	// Should not panic
	p.Update(protocol.StreamEvent{
		Kind:      "hook_event",
		Timestamp: time.Now(),
		Event:     nil,
	})

	if p.Count() != 0 {
		t.Errorf("Count = %d, want 0", p.Count())
	}
}

func TestSessionsPanelUpdateNoSessionID(t *testing.T) {
	p := NewSessionsPanel()
	p.SetSize(100, 10)

	p.Update(protocol.StreamEvent{
		Kind:      "hook_event",
		Timestamp: time.Now(),
		Event: &protocol.HookEvent{
			Agent:     "claude",
			EventType: "notification",
		},
	})

	if p.Count() != 0 {
		t.Errorf("Count = %d, want 0 (no session ID)", p.Count())
	}
}

func TestSessionsPanelViewHeader(t *testing.T) {
	p := NewSessionsPanel()
	p.SetSize(100, 10)
	p.InitSessions([]protocol.SessionInfo{
		{Agent: "claude", SessionID: "s1", EventCount: 1, LastEvent: time.Now()},
	})

	view := p.View()
	if !strings.Contains(view, "AGENT") {
		t.Error("should contain header 'AGENT'")
	}
	if !strings.Contains(view, "SESSION") {
		t.Error("should contain header 'SESSION'")
	}
	if !strings.Contains(view, "EVENTS") {
		t.Error("should contain header 'EVENTS'")
	}
}

func TestSessionsPanelSortsByLastActivity(t *testing.T) {
	p := NewSessionsPanel()
	p.SetSize(100, 10)

	old := time.Now().Add(-1 * time.Hour)
	recent := time.Now()

	p.InitSessions([]protocol.SessionInfo{
		{Agent: "claude", SessionID: "old-session", LastEvent: old, EventCount: 1},
		{Agent: "claude", SessionID: "new-session", LastEvent: recent, EventCount: 1},
	})

	view := p.View()
	newIdx := strings.Index(view, "new-session")
	oldIdx := strings.Index(view, "old-session")

	if newIdx > oldIdx {
		t.Error("most recent session should appear first")
	}
}

func TestFormatAgo(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "now"},
		{400 * time.Millisecond, "now"},
		{30 * time.Second, "30s ago"},
		{5 * time.Minute, "5m ago"},
		{2 * time.Hour, "2h ago"},
	}

	for _, tt := range tests {
		got := formatAgo(tt.d)
		if got != tt.want {
			t.Errorf("formatAgo(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestProjectName(t *testing.T) {
	tests := []struct {
		cwd, want string
	}{
		{"", ""},
		{"/home/user/myproject", "myproject"},
		{"/foo/bar/baz/", "baz"},
		{"single", "single"},
	}

	for _, tt := range tests {
		got := projectName(tt.cwd)
		if got != tt.want {
			t.Errorf("projectName(%q) = %q, want %q", tt.cwd, got, tt.want)
		}
	}
}

func TestSessionsPanelLongSessionID(t *testing.T) {
	p := NewSessionsPanel()
	p.SetSize(100, 10)

	p.InitSessions([]protocol.SessionInfo{
		{Agent: "claude", SessionID: "abcdef1234567890xyz", EventCount: 1, LastEvent: time.Now()},
	})

	view := p.View()
	// Session ID should be truncated
	if strings.Contains(view, "abcdef1234567890xyz") {
		t.Error("long session ID should be truncated")
	}
	if !strings.Contains(view, "abcdef123456") {
		t.Error("should show first 12 chars of session ID")
	}
}
