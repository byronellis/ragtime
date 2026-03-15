package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/byronellis/ragtime/internal/protocol"
)

func testModel() Model {
	info := &protocol.SubscribeResponse{
		Success: true,
		DaemonInfo: protocol.DaemonInfo{
			PID:        9999,
			StartedAt:  time.Now(),
			SocketPath: "/tmp/test.sock",
			RuleCount:  3,
		},
	}
	m := NewModel(nil, info)
	// Simulate window size
	m.width = 80
	m.height = 24
	m.statusBar.SetWidth(80)
	m.helpBar.SetWidth(80)
	m.eventFeed.SetSize(80, 22)
	return m
}

func TestModelView(t *testing.T) {
	m := testModel()
	view := m.View()

	if !strings.Contains(view, "ragtime") {
		t.Error("view should contain 'ragtime'")
	}
	if !strings.Contains(view, "quit") {
		t.Error("view should contain help text")
	}
}

func TestModelViewDisconnected(t *testing.T) {
	m := testModel()
	m.connected = false

	view := m.View()
	if !strings.Contains(view, "disconnected") {
		t.Error("disconnected view should show 'disconnected'")
	}
}

func TestModelUpdateKeyQuit(t *testing.T) {
	m := testModel()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	_ = updated

	if cmd == nil {
		t.Fatal("quit key should return a cmd")
	}
}

func TestModelUpdateKeyScroll(t *testing.T) {
	m := testModel()

	// Add some events
	for i := 0; i < 30; i++ {
		m.eventFeed.Push(makeEvent("Read", "/tmp/test.go"))
	}

	// Scroll up
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = updated.(Model)
	if m.eventFeed.offset != 1 {
		t.Errorf("offset after k = %d, want 1", m.eventFeed.offset)
	}

	// Scroll down
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(Model)
	if m.eventFeed.offset != 0 {
		t.Errorf("offset after j = %d, want 0", m.eventFeed.offset)
	}
}

func TestModelUpdateWindowSize(t *testing.T) {
	m := testModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)

	if m.width != 120 || m.height != 40 {
		t.Errorf("size = %dx%d, want 120x40", m.width, m.height)
	}
}

func TestModelUpdateEventMsg(t *testing.T) {
	m := testModel()

	event := EventMsg{Event: makeEvent("Read", "/tmp/foo.go")}
	updated, _ := m.Update(event)
	m = updated.(Model)

	if len(m.eventFeed.events) != 1 {
		t.Errorf("events = %d, want 1", len(m.eventFeed.events))
	}
}

func TestModelUpdateDisconnected(t *testing.T) {
	m := testModel()
	updated, _ := m.Update(DisconnectedMsg{})
	m = updated.(Model)

	if m.connected {
		t.Error("should be disconnected")
	}
}

func TestModelFeedHeight(t *testing.T) {
	m := testModel()
	m.height = 24

	h := m.feedHeight()
	if h != 22 {
		t.Errorf("feedHeight = %d, want 22", h)
	}
}

func TestModelFeedHeightMinimum(t *testing.T) {
	m := testModel()
	m.height = 1

	h := m.feedHeight()
	if h != 1 {
		t.Errorf("feedHeight = %d, want 1 (minimum)", h)
	}
}

func TestModelUpdateScrollKeys(t *testing.T) {
	m := testModel()
	for i := 0; i < 50; i++ {
		m.eventFeed.Push(makeEvent("Read", "/tmp/test.go"))
	}

	// G - scroll to bottom
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	m = updated.(Model)
	if m.eventFeed.offset != 0 {
		t.Errorf("offset after G = %d, want 0", m.eventFeed.offset)
	}

	// g - scroll to top
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	m = updated.(Model)
	if m.eventFeed.autoScroll {
		t.Error("should not auto-scroll after g")
	}
}

func TestModelUpdateCtrlC(t *testing.T) {
	m := testModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c should return quit cmd")
	}
}

func TestModelUpdatePageUpDown(t *testing.T) {
	m := testModel()
	for i := 0; i < 50; i++ {
		m.eventFeed.Push(makeEvent("Read", "/tmp/test.go"))
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = updated.(Model)
	if m.eventFeed.autoScroll {
		t.Error("should not auto-scroll after pgup")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	m = updated.(Model)
}

func TestModelUpdateArrowKeys(t *testing.T) {
	m := testModel()
	for i := 0; i < 50; i++ {
		m.eventFeed.Push(makeEvent("Read", "/tmp/test.go"))
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	_ = updated.(Model)
}

func TestModelInit(t *testing.T) {
	m := testModel()
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init should return a cmd")
	}
}

func TestModelTrackSession(t *testing.T) {
	m := testModel()

	event := protocol.StreamEvent{
		Kind:      "hook_event",
		Timestamp: time.Now(),
		Event: &protocol.HookEvent{
			Agent:     "claude",
			SessionID: "sess-123",
			EventType: "pre-tool-use",
			ToolName:  "Read",
			CWD:       "/Users/test/myproject",
		},
	}

	updated, _ := m.Update(EventMsg{Event: event})
	m = updated.(Model)

	if m.sessionsPanel.Count() != 1 {
		t.Errorf("sessions = %d, want 1", m.sessionsPanel.Count())
	}
	if m.statusBar.project != "myproject" {
		t.Errorf("project = %q, want %q", m.statusBar.project, "myproject")
	}

	// Same session again shouldn't increase count
	updated, _ = m.Update(EventMsg{Event: event})
	m = updated.(Model)
	if m.sessionsPanel.Count() != 1 {
		t.Errorf("sessions after dup = %d, want 1", m.sessionsPanel.Count())
	}

	// Different session
	event2 := event
	event2.Event = &protocol.HookEvent{
		Agent:     "claude",
		SessionID: "sess-456",
		EventType: "pre-tool-use",
	}
	updated, _ = m.Update(EventMsg{Event: event2})
	m = updated.(Model)
	if m.sessionsPanel.Count() != 2 {
		t.Errorf("sessions after new = %d, want 2", m.sessionsPanel.Count())
	}
}

func TestModelTrackSessionNilEvent(t *testing.T) {
	m := testModel()
	event := protocol.StreamEvent{
		Kind:      "session_update",
		Timestamp: time.Now(),
		Event:     nil,
	}

	// Should not panic
	updated, _ := m.Update(EventMsg{Event: event})
	m = updated.(Model)
	if m.sessionsPanel.Count() != 0 {
		t.Errorf("sessions = %d, want 0", m.sessionsPanel.Count())
	}
}

func TestModelSessionsPanelHeight(t *testing.T) {
	m := testModel()
	m.height = 40

	// No sessions = no panel
	if h := m.sessionsPanelHeight(); h != 0 {
		t.Errorf("sessionsPanelHeight with 0 sessions = %d, want 0", h)
	}

	// Add a session
	m.sessionsPanel.Update(protocol.StreamEvent{
		Kind:      "hook_event",
		Timestamp: time.Now(),
		Event: &protocol.HookEvent{
			Agent: "claude", SessionID: "s1", EventType: "pre-tool-use",
		},
	})

	// 1 session = header + 1 row = 2
	if h := m.sessionsPanelHeight(); h != 2 {
		t.Errorf("sessionsPanelHeight with 1 session = %d, want 2", h)
	}
}

func TestModelViewWithSessions(t *testing.T) {
	m := testModel()
	m.width = 80
	m.height = 30

	// Add a session via event
	event := protocol.StreamEvent{
		Kind:      "hook_event",
		Timestamp: time.Now(),
		Event: &protocol.HookEvent{
			Agent:     "claude",
			SessionID: "test-sess",
			EventType: "pre-tool-use",
			ToolName:  "Read",
			CWD:       "/home/user/myapp",
		},
	}
	updated, _ := m.Update(EventMsg{Event: event})
	m = updated.(Model)
	// Trigger layout recalc
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m = updated.(Model)

	view := m.View()
	if !strings.Contains(view, "claude") {
		t.Error("view should show agent name in session panel")
	}
	if !strings.Contains(view, "AGENT") {
		t.Error("view should show session panel header")
	}
}
