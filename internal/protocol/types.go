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
	MsgSubscribe            MessageType = "subscribe"
	MsgEvent                MessageType = "event"
	MsgInteractionRequest   MessageType = "interaction_request"
	MsgInteractionResponse  MessageType = "interaction_response"
	MsgStatuslineEvent      MessageType = "statusline_event"

	// Shell protocol messages
	MsgShellNew    MessageType = "shell_new"
	MsgShellAttach MessageType = "shell_attach"
	MsgShellInput  MessageType = "shell_input"
	MsgShellOutput MessageType = "shell_output"
	MsgShellResize MessageType = "shell_resize"
	MsgShellKill   MessageType = "shell_kill"
	MsgShellSend   MessageType = "shell_send"
)

// Envelope wraps all messages sent over the wire protocol.
type Envelope struct {
	Type    MessageType     `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// HookEvent is the universal hook event model, normalized across agent platforms.
type HookEvent struct {
	Agent        string            `json:"agent"`
	EventType    string            `json:"event_type"`
	SessionID    string            `json:"session_id"`
	ToolName     string            `json:"tool_name,omitempty"`
	ToolInput    map[string]any    `json:"tool_input,omitempty"`
	ToolResponse string            `json:"tool_response,omitempty"`
	Prompt       string            `json:"prompt,omitempty"`
	Response     string            `json:"response,omitempty"`
	CWD          string            `json:"cwd,omitempty"`
	Raw          map[string]any    `json:"raw,omitempty"`
	Mux          *MuxInfo          `json:"mux,omitempty"`
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
	MatchedRules       []string           `json:"matched_rules,omitempty"`
	OutputOverrides    map[string]any     `json:"output_overrides,omitempty"` // raw key-values merged into agent output
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
	Kind        string              `json:"kind"` // "hook_event", "session_update", "interaction_request", "statusline", "shell_update"
	Timestamp   time.Time           `json:"timestamp"`
	Event       *HookEvent          `json:"event,omitempty"`
	Session     *SessionInfo        `json:"session,omitempty"`
	Interaction *InteractionRequest `json:"interaction,omitempty"`
	Statusline  *StatuslineEvent    `json:"statusline,omitempty"`
	Shell       *ShellInfo          `json:"shell,omitempty"`
}

// StatuslineEvent carries Claude Code statusline telemetry (matches the real JSON format).
type StatuslineEvent struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path,omitempty"`
	CWD            string `json:"cwd,omitempty"`
	Agent          string `json:"agent,omitempty"` // set by rt, not Claude Code
	ShellID        string `json:"shell_id,omitempty"` // set when running inside rt sh

	Model struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"model"`

	Cost struct {
		TotalCostUSD       float64 `json:"total_cost_usd"`
		TotalDurationMs    int64   `json:"total_duration_ms"`
		TotalApiDurationMs int64   `json:"total_api_duration_ms"`
		TotalLinesAdded    int     `json:"total_lines_added"`
		TotalLinesRemoved  int     `json:"total_lines_removed"`
	} `json:"cost"`

	ContextWindow struct {
		TotalInputTokens  int `json:"total_input_tokens"`
		TotalOutputTokens int `json:"total_output_tokens"`
		ContextWindowSize int `json:"context_window_size"`
		CurrentUsage      struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		} `json:"current_usage"`
		UsedPercentage      int `json:"used_percentage"`
		RemainingPercentage int `json:"remaining_percentage"`
	} `json:"context_window"`

	Version string `json:"version,omitempty"`
}

// StatuslineSummary aggregates cost/token data.
type StatuslineSummary struct {
	TotalCostUSD  float64            `json:"total_cost_usd"`
	TotalInputTok int                `json:"total_input_tokens"`
	TotalOutputTok int               `json:"total_output_tokens"`
	ByModel       map[string]float64 `json:"by_model"`
}

// ShellInfo describes a shell process.
type ShellInfo struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Command   []string  `json:"command"`
	CWD       string    `json:"cwd"`
	State     string    `json:"state"`
	StartedAt time.Time `json:"started_at"`
	PID       int       `json:"pid,omitempty"`
}

// ShellNewRequest starts a new shell.
type ShellNewRequest struct {
	Name    string   `json:"name,omitempty"`
	Command []string `json:"command"`
	CWD     string   `json:"cwd,omitempty"`
	Env     []string `json:"env,omitempty"`
}

// ShellAttachRequest attaches to a shell's PTY stream.
type ShellAttachRequest struct {
	ID   string `json:"id"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

// ShellInputMessage sends data to a shell's stdin.
type ShellInputMessage struct {
	ID   string `json:"id"`
	Data []byte `json:"data"`
}

// ShellOutputMessage carries PTY output.
type ShellOutputMessage struct {
	ID   string `json:"id"`
	Data []byte `json:"data"`
}

// ShellResizeMessage updates PTY dimensions.
type ShellResizeMessage struct {
	ID   string `json:"id"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// ShellKillRequest sends a signal to a shell.
type ShellKillRequest struct {
	ID     string `json:"id"`
	Signal string `json:"signal,omitempty"`
}

// ShellSendRequest sends text (optionally + Enter) to a shell.
type ShellSendRequest struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Enter bool   `json:"enter"`
}

// InteractionType determines the input mode for a TUI interaction.
type InteractionType string

const (
	InteractionOKCancel          InteractionType = "ok_cancel"
	InteractionApproveDenyCancel InteractionType = "approve_deny_cancel"
	InteractionFreeform          InteractionType = "freeform"
)

// InteractionRequest is sent from the daemon to the TUI to prompt the user.
type InteractionRequest struct {
	ID         string          `json:"id"`
	Text       string          `json:"text"`        // markdown-formatted prompt
	Type       InteractionType `json:"type"`
	Default    string          `json:"default"`      // auto-response on timeout
	TimeoutSec int            `json:"timeout_sec"`
}

// InteractionResponse is sent from the TUI back to the daemon.
type InteractionResponse struct {
	ID       string `json:"id"`
	Value    string `json:"value"`     // "ok", "cancel", "approve", "deny", or freeform text
	TimedOut bool   `json:"timed_out"`
}
