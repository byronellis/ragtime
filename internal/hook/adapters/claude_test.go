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

func TestParseClaudeStopEvent(t *testing.T) {
	input := `{
		"session_id": "abc123",
		"cwd": "/home/user/project",
		"hook_event_name": "Stop",
		"stop_hook_active": false,
		"last_assistant_message": "I've completed the refactoring. Here's a summary of changes."
	}`

	event, err := ParseClaudeEvent([]byte(input), "stop")
	if err != nil {
		t.Fatalf("ParseClaudeEvent: %v", err)
	}

	if event.Response != "I've completed the refactoring. Here's a summary of changes." {
		t.Errorf("response = %q, want assistant message", event.Response)
	}
}

func TestParseClaudeSubagentStopEvent(t *testing.T) {
	input := `{
		"session_id": "abc123",
		"cwd": "/home/user/project",
		"hook_event_name": "SubagentStop",
		"agent_id": "agent-456",
		"agent_type": "Explore",
		"last_assistant_message": "Found 3 potential issues in the codebase."
	}`

	event, err := ParseClaudeEvent([]byte(input), "subagent-stop")
	if err != nil {
		t.Fatalf("ParseClaudeEvent: %v", err)
	}

	if event.Response != "Found 3 potential issues in the codebase." {
		t.Errorf("response = %q, want subagent message", event.Response)
	}
}

func TestParseClaudeNotificationEvent(t *testing.T) {
	input := `{
		"session_id": "abc123",
		"cwd": "/home/user/project",
		"hook_event_name": "Notification",
		"message": "Claude needs your permission to use Bash",
		"title": "Permission needed"
	}`

	event, err := ParseClaudeEvent([]byte(input), "notification")
	if err != nil {
		t.Fatalf("ParseClaudeEvent: %v", err)
	}

	if event.Response != "Claude needs your permission to use Bash" {
		t.Errorf("response = %q, want notification message", event.Response)
	}
}

func TestParseClaudePostToolUseEvent(t *testing.T) {
	input := `{
		"session_id": "abc123",
		"cwd": "/home/user/project",
		"hook_event_name": "PostToolUse",
		"tool_name": "Write",
		"tool_input": {"file_path": "/tmp/test.go", "content": "package main"},
		"tool_response": {"filePath": "/tmp/test.go", "success": true}
	}`

	event, err := ParseClaudeEvent([]byte(input), "post-tool-use")
	if err != nil {
		t.Fatalf("ParseClaudeEvent: %v", err)
	}

	if event.ToolResponse == "" {
		t.Error("tool_response should be populated")
	}
	if event.ToolName != "Write" {
		t.Errorf("tool_name = %q, want Write", event.ToolName)
	}
}

func TestStringifyToolResponse(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  string
	}{
		{"nil", nil, ""},
		{"string", "file written", "file written"},
		{"map", map[string]any{"success": true}, `{"success":true}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stringifyToolResponse(tt.input)
			if got != tt.want {
				t.Errorf("stringifyToolResponse = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRawString(t *testing.T) {
	m := map[string]any{
		"str": "hello",
		"num": 42,
	}
	if got := rawString(m, "str"); got != "hello" {
		t.Errorf("rawString(str) = %q", got)
	}
	if got := rawString(m, "num"); got != "" {
		t.Errorf("rawString(num) = %q, want empty (wrong type)", got)
	}
	if got := rawString(m, "missing"); got != "" {
		t.Errorf("rawString(missing) = %q, want empty", got)
	}
}
