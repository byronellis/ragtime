package adapters

import (
	"encoding/json"
	"os"

	"github.com/byronellis/ragtime/internal/protocol"
)

// ClaudeRawEvent represents the JSON structure Claude Code sends on stdin.
// ClaudeRawEvent represents the JSON structure Claude Code sends on stdin.
// Field names match Claude Code's hook event schemas.
type ClaudeRawEvent struct {
	SessionID            string         `json:"session_id"`
	TranscriptPath       string         `json:"transcript_path"`
	CWD                  string         `json:"cwd"`
	ToolName             string         `json:"tool_name,omitempty"`
	ToolInput            map[string]any `json:"tool_input,omitempty"`
	ToolResponse         any            `json:"tool_response,omitempty"`
	Prompt               string         `json:"prompt,omitempty"`
	LastAssistantMessage string         `json:"last_assistant_message,omitempty"`
	Message              string         `json:"message,omitempty"`
	StopHookActive       bool           `json:"stop_hook_active,omitempty"`
	Source               string         `json:"source,omitempty"`
	PermissionMode       string         `json:"permission_mode,omitempty"`
	HookEventName        string         `json:"hook_event_name,omitempty"`
	AgentID              string         `json:"agent_id,omitempty"`
	AgentType            string         `json:"agent_type,omitempty"`
}

// ParseClaudeEvent converts raw stdin bytes from Claude Code into a universal HookEvent.
func ParseClaudeEvent(data []byte, eventType string) (*protocol.HookEvent, error) {
	var raw ClaudeRawEvent
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	// Store the full raw payload for escape-hatch access
	var rawMap map[string]any
	json.Unmarshal(data, &rawMap)

	event := &protocol.HookEvent{
		Agent:     "claude",
		EventType: eventType,
		SessionID: raw.SessionID,
		ToolName:  raw.ToolName,
		ToolInput: raw.ToolInput,
		Prompt:    raw.Prompt,
		CWD:       raw.CWD,
		Raw:       rawMap,
	}

	// Extract agent response text from stop/notification/subagent-stop events
	switch eventType {
	case "stop", "subagent-stop":
		event.Response = raw.LastAssistantMessage
		// Fall back to raw map in case field name changes
		if event.Response == "" {
			event.Response = rawString(rawMap, "last_assistant_message")
		}
	case "notification":
		event.Response = raw.Message
		if event.Response == "" {
			event.Response = rawString(rawMap, "message")
		}
	case "post-tool-use":
		event.ToolResponse = stringifyToolResponse(raw.ToolResponse)
	}

	// Detect terminal multiplexer from the hook process environment
	event.Mux = DetectMux()

	return event, nil
}

// ClaudePreToolUseResponse formats a HookResponse as Claude Code PreToolUse JSON output.
func ClaudePreToolUseResponse(resp *protocol.HookResponse) map[string]any {
	output := map[string]any{}
	hookOutput := map[string]any{
		"hookEventName": "PreToolUse",
	}

	if resp.Context != "" {
		hookOutput["additionalContext"] = resp.Context
	}

	if resp.PermissionDecision != "" {
		hookOutput["permissionDecision"] = string(resp.PermissionDecision)
		if resp.DenyReason != "" {
			hookOutput["permissionDecisionReason"] = resp.DenyReason
		}
	}

	// Only include hookSpecificOutput if there's something to say
	if len(hookOutput) > 1 { // more than just hookEventName
		output["hookSpecificOutput"] = hookOutput
	}

	return output
}

// ClaudePostToolUseResponse formats a HookResponse for PostToolUse events.
func ClaudePostToolUseResponse(resp *protocol.HookResponse) map[string]any {
	output := map[string]any{}
	if resp.Context != "" {
		hookOutput := map[string]any{
			"hookEventName":     "PostToolUse",
			"additionalContext": resp.Context,
		}
		output["hookSpecificOutput"] = hookOutput
	}
	return output
}

// ClaudeStopResponse formats a HookResponse for Stop events.
func ClaudeStopResponse(resp *protocol.HookResponse) map[string]any {
	output := map[string]any{}
	if resp.Context != "" {
		hookOutput := map[string]any{
			"hookEventName":     "Stop",
			"additionalContext": resp.Context,
		}
		output["hookSpecificOutput"] = hookOutput
	}
	return output
}

// FormatClaudeResponse routes to the appropriate response formatter based on event type.
func FormatClaudeResponse(resp *protocol.HookResponse, eventType string) map[string]any {
	switch eventType {
	case "pre-tool-use":
		return ClaudePreToolUseResponse(resp)
	case "post-tool-use":
		return ClaudePostToolUseResponse(resp)
	case "stop":
		return ClaudeStopResponse(resp)
	default:
		// Generic: just include context if present
		if resp.Context != "" {
			return map[string]any{
				"hookSpecificOutput": map[string]any{
					"additionalContext": resp.Context,
				},
			}
		}
		return map[string]any{}
	}
}

// rawString extracts a string value from a raw map, returning "" if missing or wrong type.
func rawString(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// stringifyToolResponse converts a tool_response value to a string for storage.
// Claude Code sends this as a string or structured object.
func stringifyToolResponse(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	s := string(data)
	// Don't store huge tool responses (e.g. full file contents)
	if len(s) > 2000 {
		return s[:2000] + "..."
	}
	return s
}

// DetectMux detects the terminal multiplexer from environment variables.
// Called in the hook process which inherits the terminal environment.
func DetectMux() *protocol.MuxInfo {
	if tmux := os.Getenv("TMUX"); tmux != "" {
		pane := os.Getenv("TMUX_PANE")
		return &protocol.MuxInfo{Type: "tmux", Pane: pane}
	}
	if sty := os.Getenv("STY"); sty != "" {
		return &protocol.MuxInfo{Type: "screen", SessionName: sty}
	}
	return nil
}
