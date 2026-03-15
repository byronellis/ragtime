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
