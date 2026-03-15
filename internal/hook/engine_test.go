package hook

import (
	"log/slog"
	"testing"

	"github.com/byronellis/ragtime/internal/config"
	"github.com/byronellis/ragtime/internal/protocol"
)

func TestEvaluate_InjectContext(t *testing.T) {
	rules := []config.RuleConfig{
		{
			Name:  "inject-test",
			Match: config.MatchConfig{Event: "pre-tool-use", Tool: "Read"},
			Actions: []config.Action{
				{Type: "inject-context", Content: "Remember to check the API docs"},
			},
		},
	}

	engine := NewEngine(rules, slog.Default())
	event := &protocol.HookEvent{
		Agent:     "claude",
		EventType: "pre-tool-use",
		ToolName:  "Read",
	}

	resp := engine.Evaluate(event)
	if resp.Context != "Remember to check the API docs" {
		t.Errorf("context = %q", resp.Context)
	}
}

func TestEvaluate_Approve(t *testing.T) {
	rules := []config.RuleConfig{
		{
			Name:  "auto-approve-read",
			Match: config.MatchConfig{Event: "pre-tool-use", Tool: "Read"},
			Actions: []config.Action{
				{Type: "approve"},
			},
		},
	}

	engine := NewEngine(rules, slog.Default())
	event := &protocol.HookEvent{
		Agent:     "claude",
		EventType: "pre-tool-use",
		ToolName:  "Read",
	}

	resp := engine.Evaluate(event)
	if resp.PermissionDecision != protocol.PermAllow {
		t.Errorf("permission = %q, want allow", resp.PermissionDecision)
	}
}

func TestEvaluate_Deny(t *testing.T) {
	rules := []config.RuleConfig{
		{
			Name:  "deny-dangerous",
			Match: config.MatchConfig{Event: "pre-tool-use", Tool: "Bash"},
			Actions: []config.Action{
				{Type: "deny", Reason: "Bash is restricted"},
			},
		},
	}

	engine := NewEngine(rules, slog.Default())
	event := &protocol.HookEvent{
		Agent:     "claude",
		EventType: "pre-tool-use",
		ToolName:  "Bash",
	}

	resp := engine.Evaluate(event)
	if resp.PermissionDecision != protocol.PermDeny {
		t.Errorf("permission = %q, want deny", resp.PermissionDecision)
	}
	if resp.DenyReason != "Bash is restricted" {
		t.Errorf("reason = %q", resp.DenyReason)
	}
}

func TestEvaluate_NoMatch(t *testing.T) {
	rules := []config.RuleConfig{
		{
			Name:  "read-only",
			Match: config.MatchConfig{Tool: "Read"},
			Actions: []config.Action{
				{Type: "inject-context", Content: "should not appear"},
			},
		},
	}

	engine := NewEngine(rules, slog.Default())
	event := &protocol.HookEvent{
		Agent:     "claude",
		EventType: "pre-tool-use",
		ToolName:  "Write",
	}

	resp := engine.Evaluate(event)
	if resp.Context != "" {
		t.Errorf("context = %q, want empty", resp.Context)
	}
}

func TestEvaluate_MultipleContextInjections(t *testing.T) {
	rules := []config.RuleConfig{
		{
			Name:  "rule-a",
			Match: config.MatchConfig{Event: "*"},
			Actions: []config.Action{
				{Type: "inject-context", Content: "Context A"},
			},
		},
		{
			Name:  "rule-b",
			Match: config.MatchConfig{Event: "*"},
			Actions: []config.Action{
				{Type: "inject-context", Content: "Context B"},
			},
		},
	}

	engine := NewEngine(rules, slog.Default())
	event := &protocol.HookEvent{EventType: "pre-tool-use", ToolName: "Read"}

	resp := engine.Evaluate(event)
	if resp.Context != "Context A\n\n---\n\nContext B" {
		t.Errorf("context = %q", resp.Context)
	}
}

func TestEvaluate_WildcardMatch(t *testing.T) {
	rules := []config.RuleConfig{
		{
			Name:  "catch-all",
			Match: config.MatchConfig{Agent: "*", Event: "*"},
			Actions: []config.Action{
				{Type: "inject-context", Content: "universal"},
			},
		},
	}

	engine := NewEngine(rules, slog.Default())
	event := &protocol.HookEvent{Agent: "gemini", EventType: "stop"}

	resp := engine.Evaluate(event)
	if resp.Context != "universal" {
		t.Errorf("context = %q, want universal", resp.Context)
	}
}

func TestEvaluate_PathGlob(t *testing.T) {
	rules := []config.RuleConfig{
		{
			Name:  "api-docs",
			Match: config.MatchConfig{Tool: "Read", PathGlob: "api/*"},
			Actions: []config.Action{
				{Type: "inject-context", Content: "API context"},
			},
		},
	}

	engine := NewEngine(rules, slog.Default())

	// Matches
	event := &protocol.HookEvent{
		EventType: "pre-tool-use",
		ToolName:  "Read",
		ToolInput: map[string]any{"file_path": "api/handler.go"},
	}
	resp := engine.Evaluate(event)
	if resp.Context != "API context" {
		t.Errorf("should match api/*, context = %q", resp.Context)
	}

	// Doesn't match
	event.ToolInput = map[string]any{"file_path": "internal/foo.go"}
	resp = engine.Evaluate(event)
	if resp.Context != "" {
		t.Errorf("should not match, context = %q", resp.Context)
	}
}

func TestEvaluate_PipeAlternatives(t *testing.T) {
	rules := []config.RuleConfig{
		{
			Name:  "read-or-write",
			Match: config.MatchConfig{Tool: "Read|Write"},
			Actions: []config.Action{
				{Type: "inject-context", Content: "file op"},
			},
		},
	}

	engine := NewEngine(rules, slog.Default())

	for _, tool := range []string{"Read", "Write"} {
		event := &protocol.HookEvent{EventType: "pre-tool-use", ToolName: tool}
		resp := engine.Evaluate(event)
		if resp.Context != "file op" {
			t.Errorf("tool=%s: context = %q, want 'file op'", tool, resp.Context)
		}
	}

	event := &protocol.HookEvent{EventType: "pre-tool-use", ToolName: "Bash"}
	resp := engine.Evaluate(event)
	if resp.Context != "" {
		t.Errorf("Bash should not match Read|Write")
	}
}
