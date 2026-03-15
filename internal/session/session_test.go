package session

import (
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
