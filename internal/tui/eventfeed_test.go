package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/byronellis/ragtime/internal/protocol"
)

func makeEvent(tool, detail string) protocol.StreamEvent {
	input := map[string]any{}
	switch tool {
	case "Read", "Write", "Edit":
		input["file_path"] = detail
	case "Bash":
		input["command"] = detail
	case "Grep":
		input["pattern"] = detail
	case "Glob":
		input["pattern"] = detail
	}
	return protocol.StreamEvent{
		Kind:      "hook_event",
		Timestamp: time.Now(),
		Event: &protocol.HookEvent{
			Agent:     "claude",
			EventType: "pre-tool-use",
			ToolName:  tool,
			ToolInput: input,
		},
	}
}

func TestEventFeedPush(t *testing.T) {
	f := NewEventFeed()
	f.SetSize(80, 10)

	f.Push(makeEvent("Read", "/tmp/test.go"))

	view := f.View()
	if !strings.Contains(view, "Read") {
		t.Errorf("view should contain 'Read', got: %s", view)
	}
}

func TestEventFeedMaxEvents(t *testing.T) {
	f := NewEventFeed()
	f.SetSize(80, 10)

	for i := 0; i < maxEvents+50; i++ {
		f.Push(makeEvent("Read", "/tmp/test.go"))
	}

	if len(f.events) != maxEvents {
		t.Errorf("events = %d, want %d", len(f.events), maxEvents)
	}
}

func TestEventFeedEmptyView(t *testing.T) {
	f := NewEventFeed()
	f.SetSize(80, 10)

	view := f.View()
	if !strings.Contains(view, "Waiting for events") {
		t.Errorf("empty view should show waiting message, got: %s", view)
	}
}

func TestEventFeedScrollUp(t *testing.T) {
	f := NewEventFeed()
	f.SetSize(80, 5)

	for i := 0; i < 20; i++ {
		f.Push(makeEvent("Read", "/tmp/test.go"))
	}

	if !f.autoScroll {
		t.Error("should auto-scroll by default")
	}

	f.ScrollUp(3)

	if f.autoScroll {
		t.Error("should not auto-scroll after scrolling up")
	}
	if f.offset != 3 {
		t.Errorf("offset = %d, want 3", f.offset)
	}
}

func TestEventFeedScrollDown(t *testing.T) {
	f := NewEventFeed()
	f.SetSize(80, 5)

	for i := 0; i < 20; i++ {
		f.Push(makeEvent("Read", "/tmp/test.go"))
	}

	f.ScrollUp(5)
	f.ScrollDown(3)

	if f.offset != 2 {
		t.Errorf("offset = %d, want 2", f.offset)
	}

	f.ScrollDown(10) // should clamp and re-enable auto-scroll
	if f.offset != 0 {
		t.Errorf("offset = %d, want 0 after clamping", f.offset)
	}
	if !f.autoScroll {
		t.Error("should re-enable auto-scroll when at bottom")
	}
}

func TestEventFeedScrollToTop(t *testing.T) {
	f := NewEventFeed()
	f.SetSize(80, 5)

	for i := 0; i < 20; i++ {
		f.Push(makeEvent("Read", "/tmp/test.go"))
	}

	f.ScrollToTop()

	if f.autoScroll {
		t.Error("should not auto-scroll after scroll to top")
	}
	expectedOffset := len(f.events) - 5
	if f.offset != expectedOffset {
		t.Errorf("offset = %d, want %d", f.offset, expectedOffset)
	}
}

func TestEventFeedScrollToBottom(t *testing.T) {
	f := NewEventFeed()
	f.SetSize(80, 5)

	for i := 0; i < 20; i++ {
		f.Push(makeEvent("Read", "/tmp/test.go"))
	}

	f.ScrollUp(10)
	f.ScrollToBottom()

	if f.offset != 0 {
		t.Errorf("offset = %d, want 0", f.offset)
	}
	if !f.autoScroll {
		t.Error("should auto-scroll after scroll to bottom")
	}
}

func TestEventFeedScrollUpClampsToMax(t *testing.T) {
	f := NewEventFeed()
	f.SetSize(80, 5)

	for i := 0; i < 10; i++ {
		f.Push(makeEvent("Read", "/tmp/test.go"))
	}

	f.ScrollUp(100) // more than available
	maxOffset := len(f.events) - 5
	if f.offset != maxOffset {
		t.Errorf("offset = %d, want %d", f.offset, maxOffset)
	}
}

func TestShortDetail(t *testing.T) {
	tests := []struct {
		name     string
		event    *protocol.HookEvent
		contains string
	}{
		{
			name: "Read tool",
			event: &protocol.HookEvent{
				ToolName:  "Read",
				ToolInput: map[string]any{"file_path": "/a/b/c/d/e.go"},
			},
			contains: "c/d/e.go",
		},
		{
			name: "Bash tool",
			event: &protocol.HookEvent{
				ToolName:  "Bash",
				ToolInput: map[string]any{"command": "go test ./..."},
			},
			contains: "go test",
		},
		{
			name: "Grep tool",
			event: &protocol.HookEvent{
				ToolName:  "Grep",
				ToolInput: map[string]any{"pattern": "func New"},
			},
			contains: "func New",
		},
		{
			name: "Glob tool",
			event: &protocol.HookEvent{
				ToolName:  "Glob",
				ToolInput: map[string]any{"pattern": "**/*.go"},
			},
			contains: "**/*.go",
		},
		{
			name: "Agent tool",
			event: &protocol.HookEvent{
				ToolName:  "Agent",
				ToolInput: map[string]any{"description": "explore codebase"},
			},
			contains: "explore codebase",
		},
		{
			name: "prompt event",
			event: &protocol.HookEvent{
				Prompt: "hello world",
			},
			contains: "hello world",
		},
		{
			name:  "empty event",
			event: &protocol.HookEvent{},
		},
		{
			name: "unknown tool with path",
			event: &protocol.HookEvent{
				ToolName:  "Custom",
				ToolInput: map[string]any{"file_path": "/foo/bar.txt"},
			},
			contains: "bar.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shortDetail(tt.event)
			if tt.contains != "" && !strings.Contains(got, tt.contains) {
				t.Errorf("shortDetail() = %q, want it to contain %q", got, tt.contains)
			}
		})
	}
}

func TestShortDetailLongPrompt(t *testing.T) {
	long := strings.Repeat("a", 200)
	event := &protocol.HookEvent{Prompt: long}
	got := shortDetail(event)
	if len(got) > 100 {
		t.Errorf("long prompt should be truncated, got len=%d", len(got))
	}
}

func TestShortPath(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"", ""},
		{"file.go", "file.go"},
		{"a/b/c", "a/b/c"},
		{"/a/b/c/d/e.go", "c/d/e.go"},
	}
	for _, tt := range tests {
		got := shortPath(tt.input)
		if got != tt.want {
			t.Errorf("shortPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestEventFeedRenderNilEvent(t *testing.T) {
	f := NewEventFeed()
	f.SetSize(80, 5)

	// Push an event with nil Event field
	f.Push(protocol.StreamEvent{
		Kind:      "session_update",
		Timestamp: time.Now(),
		Event:     nil,
	})

	// Should not panic and should render empty
	view := f.View()
	_ = view
}

func TestEventFeedScrollUpWhenFewEvents(t *testing.T) {
	f := NewEventFeed()
	f.SetSize(80, 10)

	// Only 3 events, fewer than height
	for i := 0; i < 3; i++ {
		f.Push(makeEvent("Read", "/tmp/test.go"))
	}

	f.ScrollUp(5) // should clamp to max (0 since events < height)
	if f.offset != 0 {
		t.Errorf("offset = %d, want 0 (events < height)", f.offset)
	}
}

func TestEventFeedScrollToTopFewEvents(t *testing.T) {
	f := NewEventFeed()
	f.SetSize(80, 10)

	for i := 0; i < 3; i++ {
		f.Push(makeEvent("Read", "/tmp/test.go"))
	}

	f.ScrollToTop()
	if f.offset != 0 {
		t.Errorf("offset = %d, want 0 (events < height)", f.offset)
	}
}

func TestShortDetailLongBash(t *testing.T) {
	cmd := strings.Repeat("x", 200)
	event := &protocol.HookEvent{
		ToolName:  "Bash",
		ToolInput: map[string]any{"command": cmd},
	}
	got := shortDetail(event)
	if len(got) > 100 {
		t.Errorf("long bash should be truncated, got len=%d", len(got))
	}
}

func TestShortDetailWebSearch(t *testing.T) {
	event := &protocol.HookEvent{
		ToolName:  "WebSearch",
		ToolInput: map[string]any{"query": "golang testing"},
	}
	got := shortDetail(event)
	// WebSearch falls through to the default case
	if !strings.Contains(got, "golang testing") {
		t.Errorf("shortDetail(WebSearch) = %q, want to contain 'golang testing'", got)
	}
}

func TestStrField(t *testing.T) {
	m := map[string]any{
		"str": "hello",
		"num": 42,
	}

	if got := strField(m, "str"); got != "hello" {
		t.Errorf("strField(str) = %q, want %q", got, "hello")
	}
	if got := strField(m, "num"); got != "42" {
		t.Errorf("strField(num) = %q, want %q", got, "42")
	}
	if got := strField(m, "missing"); got != "" {
		t.Errorf("strField(missing) = %q, want empty", got)
	}
}
