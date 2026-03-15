package session

import (
	"strings"
	"testing"

	"github.com/byronellis/ragtime/internal/protocol"
)

func TestDedup(t *testing.T) {
	s := NewSession("claude", "test-123")

	content := "Some injected context"

	if s.IsDuplicate(content) {
		t.Error("first time should not be duplicate")
	}
	if !s.IsDuplicate(content) {
		t.Error("second time should be duplicate")
	}

	// Different content should not be duplicate
	if s.IsDuplicate("Different content") {
		t.Error("different content should not be duplicate")
	}

	// Empty content is never duplicate
	if s.IsDuplicate("") {
		t.Error("empty content should not be duplicate")
	}
}

func TestManagerProcessEvent_Dedup(t *testing.T) {
	mgr := NewManager(nil)

	event := &protocol.HookEvent{
		Agent:     "claude",
		SessionID: "sess-1",
		EventType: "pre-tool-use",
		ToolName:  "Read",
		ToolInput: map[string]any{"file_path": "/foo/bar.go"},
	}

	resp := &protocol.HookResponse{
		Context: "Here is some context",
	}

	// First call: context should pass through
	result := mgr.ProcessEvent(event, resp)
	if result.Context != "Here is some context" {
		t.Errorf("first call: context = %q, want non-empty", result.Context)
	}

	// Second call with same context: should be deduped
	result = mgr.ProcessEvent(event, resp)
	if result.Context != "" {
		t.Errorf("second call: context = %q, want empty (deduped)", result.Context)
	}

	// Session should have 2 events
	sess := mgr.Get("claude", "sess-1")
	if sess.EventCount() != 2 {
		t.Errorf("event count = %d, want 2", sess.EventCount())
	}

	// Events should have detail from ToolInput
	events := sess.RecentEvents(2)
	if events[0].Detail == "" {
		t.Error("event should have detail extracted from ToolInput")
	}
}

func TestManagerProcessEvent_NoSession(t *testing.T) {
	mgr := NewManager(nil)

	event := &protocol.HookEvent{
		Agent:     "claude",
		SessionID: "", // no session ID
		EventType: "notification",
	}

	resp := &protocol.HookResponse{Context: "test"}
	result := mgr.ProcessEvent(event, resp)

	// Should pass through unchanged
	if result.Context != "test" {
		t.Errorf("context = %q, want test", result.Context)
	}
}

func TestExtractDetail(t *testing.T) {
	tests := []struct {
		name     string
		event    *protocol.HookEvent
		contains string
	}{
		{
			name: "Read extracts file_path",
			event: &protocol.HookEvent{
				ToolName:  "Read",
				ToolInput: map[string]any{"file_path": "/Users/me/project/internal/foo/bar.go"},
			},
			contains: "foo/bar.go",
		},
		{
			name: "Bash extracts command",
			event: &protocol.HookEvent{
				ToolName:  "Bash",
				ToolInput: map[string]any{"command": "go build ./..."},
			},
			contains: "go build",
		},
		{
			name: "Grep extracts pattern and path",
			event: &protocol.HookEvent{
				ToolName:  "Grep",
				ToolInput: map[string]any{"pattern": "func main", "path": "/project/cmd"},
			},
			contains: "func main",
		},
		{
			name: "Agent extracts description",
			event: &protocol.HookEvent{
				ToolName:  "Agent",
				ToolInput: map[string]any{"description": "explore codebase"},
			},
			contains: "explore codebase",
		},
		{
			name: "prompt is captured as detail",
			event: &protocol.HookEvent{
				EventType: "user-prompt-submit",
				Prompt:    "implement the auth middleware",
			},
			contains: "implement the auth middleware",
		},
		{
			name: "empty input returns empty",
			event: &protocol.HookEvent{
				ToolName:  "Read",
				ToolInput: nil,
			},
			contains: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detail := extractDetail(tt.event)
			if tt.contains == "" {
				if detail != "" {
					t.Errorf("got %q, want empty", detail)
				}
				return
			}
			if !strings.Contains(detail, tt.contains) {
				t.Errorf("detail %q does not contain %q", detail, tt.contains)
			}
		})
	}
}

