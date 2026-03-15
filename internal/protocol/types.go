package protocol

import (
	"encoding/json"
	"time"
)

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
	Prompt    string            `json:"prompt,omitempty"`
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

// SubscribeRequest is sent by TUI clients to start streaming events.
type SubscribeRequest struct {
	EventTypes []string `json:"event_types,omitempty"` // filter (empty = all)
}

// SubscribeResponse is the initial reply with a daemon state snapshot.
type SubscribeResponse struct {
	Success    bool          `json:"success"`
	DaemonInfo DaemonInfo    `json:"daemon_info"`
	Sessions   []SessionInfo `json:"sessions"`
	Error      string        `json:"error,omitempty"`
}

// DaemonInfo describes the running daemon.
type DaemonInfo struct {
	PID        int       `json:"pid"`
	StartedAt  time.Time `json:"started_at"`
	SocketPath string    `json:"socket_path"`
	RuleCount  int       `json:"rule_count"`
}

// SessionInfo is a summary of an active session.
type SessionInfo struct {
	Agent      string    `json:"agent"`
	SessionID  string    `json:"session_id"`
	StartedAt  time.Time `json:"started_at"`
	EventCount int       `json:"event_count"`
	LastEvent  time.Time `json:"last_event,omitempty"`
}

// StreamEvent wraps events pushed to TUI subscribers.
type StreamEvent struct {
	Kind      string       `json:"kind"` // "hook_event", "session_update"
	Timestamp time.Time    `json:"timestamp"`
	Event     *HookEvent   `json:"event,omitempty"`
	Session   *SessionInfo `json:"session,omitempty"`
}
