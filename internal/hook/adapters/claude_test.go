package adapters

import (
	"encoding/json"
	"testing"

	"github.com/byronellis/ragtime/internal/protocol"
)

func TestParseClaudeEvent(t *testing.T) {
	input := `{
		"session_id": "abc123",
		"cwd": "/home/user/project",
		"tool_name": "Read",
		"tool_input": {"file_path": "/tmp/test.go"},
		"hook_event_name": "PreToolUse"
	}`

	event, err := ParseClaudeEvent([]byte(input), "pre-tool-use")
	if err != nil {
		t.Fatalf("ParseClaudeEvent: %v", err)
	}

	if event.Agent != "claude" {
		t.Errorf("agent = %q, want %q", event.Agent, "claude")
	}
	if event.EventType != "pre-tool-use" {
		t.Errorf("event_type = %q, want %q", event.EventType, "pre-tool-use")
	}
	if event.SessionID != "abc123" {
		t.Errorf("session_id = %q, want %q", event.SessionID, "abc123")
	}
	if event.ToolName != "Read" {
		t.Errorf("tool_name = %q, want %q", event.ToolName, "Read")
	}
	if event.ToolInput["file_path"] != "/tmp/test.go" {
		t.Errorf("tool_input.file_path = %v, want /tmp/test.go", event.ToolInput["file_path"])
	}
	if event.Raw == nil {
		t.Error("raw should be populated")
	}
}

func TestClaudePreToolUseResponse_ContextInjection(t *testing.T) {
	resp := &protocol.HookResponse{
		Context: "Here is some helpful context",
	}

	output := ClaudePreToolUseResponse(resp)
	data, _ := json.Marshal(output)

	var parsed map[string]any
	json.Unmarshal(data, &parsed)

	hookOutput, ok := parsed["hookSpecificOutput"].(map[string]any)
	if !ok {
		t.Fatalf("missing hookSpecificOutput, got: %s", data)
	}
	if hookOutput["additionalContext"] != "Here is some helpful context" {
		t.Errorf("additionalContext = %v", hookOutput["additionalContext"])
	}
}

func TestClaudePreToolUseResponse_Approve(t *testing.T) {
	resp := &protocol.HookResponse{
		PermissionDecision: protocol.PermAllow,
	}

	output := ClaudePreToolUseResponse(resp)
	data, _ := json.Marshal(output)

	var parsed map[string]any
	json.Unmarshal(data, &parsed)

	hookOutput := parsed["hookSpecificOutput"].(map[string]any)
	if hookOutput["permissionDecision"] != "allow" {
		t.Errorf("permissionDecision = %v, want allow", hookOutput["permissionDecision"])
	}
}

func TestClaudePreToolUseResponse_Deny(t *testing.T) {
	resp := &protocol.HookResponse{
		PermissionDecision: protocol.PermDeny,
		DenyReason:         "This tool is not allowed",
	}

	output := ClaudePreToolUseResponse(resp)
	data, _ := json.Marshal(output)

	var parsed map[string]any
	json.Unmarshal(data, &parsed)

	hookOutput := parsed["hookSpecificOutput"].(map[string]any)
	if hookOutput["permissionDecision"] != "deny" {
		t.Errorf("permissionDecision = %v, want deny", hookOutput["permissionDecision"])
	}
	if hookOutput["permissionDecisionReason"] != "This tool is not allowed" {
		t.Errorf("reason = %v", hookOutput["permissionDecisionReason"])
	}
}

func TestClaudePreToolUseResponse_NoOp(t *testing.T) {
	resp := &protocol.HookResponse{}
	output := ClaudePreToolUseResponse(resp)

	if _, ok := output["hookSpecificOutput"]; ok {
		t.Error("no-op response should not include hookSpecificOutput")
	}
}
