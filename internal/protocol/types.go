package protocol

import "encoding/json"

// MessageType identifies the kind of message sent over the wire.
type MessageType string

const (
	MsgHookEvent    MessageType = "hook_event"
	MsgHookResponse MessageType = "hook_response"
	MsgCommand      MessageType = "command"
	MsgSubscribe    MessageType = "subscribe"
	MsgEvent        MessageType = "event"
)

// Envelope wraps all messages sent over the wire protocol.
type Envelope struct {
	Type    MessageType     `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// HookEvent is the universal hook event model, normalized across agent platforms.
type HookEvent struct {
	Agent     string            `json:"agent"`
	EventType string            `json:"event_type"`
	SessionID string            `json:"session_id"`
	ToolName  string            `json:"tool_name,omitempty"`
	ToolInput map[string]any    `json:"tool_input,omitempty"`
	CWD       string            `json:"cwd,omitempty"`
	Raw       map[string]any    `json:"raw,omitempty"`
	Mux       *MuxInfo          `json:"mux,omitempty"`
}

// MuxInfo describes the detected terminal multiplexer environment.
type MuxInfo struct {
	Type        string `json:"type"`
	SessionName string `json:"session_name,omitempty"`
	Pane        string `json:"pane,omitempty"`
}

// PermissionDecision controls tool approval behavior.
type PermissionDecision string

const (
	PermAllow PermissionDecision = "allow"
	PermDeny  PermissionDecision = "deny"
	PermAsk   PermissionDecision = "ask"
)

// HookResponse is the universal response from the hook engine.
type HookResponse struct {
	Context            string             `json:"context,omitempty"`
	PermissionDecision PermissionDecision `json:"permission_decision,omitempty"`
	DenyReason         string             `json:"deny_reason,omitempty"`
}

// CommandRequest represents a CLI command sent to the daemon (e.g., index, search).
type CommandRequest struct {
	Command string         `json:"command"`
	Args    map[string]any `json:"args,omitempty"`
}

// CommandResponse is returned by the daemon for CLI commands.
type CommandResponse struct {
	Success bool   `json:"success"`
	Data    any    `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
}
