package protocol

import (
	"bytes"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	event := HookEvent{
		Agent:     "claude",
		EventType: "pre-tool-use",
		SessionID: "test-session",
		ToolName:  "Read",
		ToolInput: map[string]any{"file_path": "/tmp/test.go"},
	}

	env, err := NewEnvelope(MsgHookEvent, &event)
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}

	var buf bytes.Buffer
	if err := WriteMessage(&buf, env); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	got, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	if got.Type != MsgHookEvent {
		t.Errorf("type = %q, want %q", got.Type, MsgHookEvent)
	}

	var decoded HookEvent
	if err := got.DecodePayload(&decoded); err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}

	if decoded.Agent != "claude" {
		t.Errorf("agent = %q, want %q", decoded.Agent, "claude")
	}
	if decoded.ToolName != "Read" {
		t.Errorf("tool_name = %q, want %q", decoded.ToolName, "Read")
	}
	if decoded.SessionID != "test-session" {
		t.Errorf("session_id = %q, want %q", decoded.SessionID, "test-session")
	}
}

func TestMessageTooLarge(t *testing.T) {
	var buf bytes.Buffer
	// Write a length that exceeds the 10MB limit
	env := &Envelope{Type: MsgHookEvent, Payload: []byte(`{}`)}
	if err := WriteMessage(&buf, env); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	// Overwrite the length prefix with a huge value
	b := buf.Bytes()
	b[0], b[1], b[2], b[3] = 0x01, 0x00, 0x00, 0x00 // 16MB
	_, err := ReadMessage(bytes.NewReader(b))
	if err == nil {
		t.Fatal("expected error for oversized message")
	}
}

func TestResponseRoundTrip(t *testing.T) {
	resp := HookResponse{
		Context:            "Some injected context",
		PermissionDecision: PermAllow,
	}

	env, err := NewEnvelope(MsgHookResponse, &resp)
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}

	var buf bytes.Buffer
	if err := WriteMessage(&buf, env); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	got, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	var decoded HookResponse
	if err := got.DecodePayload(&decoded); err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}

	if decoded.Context != "Some injected context" {
		t.Errorf("context = %q, want %q", decoded.Context, "Some injected context")
	}
	if decoded.PermissionDecision != PermAllow {
		t.Errorf("permission = %q, want %q", decoded.PermissionDecision, PermAllow)
	}
}
