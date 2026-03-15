package adapters

import (
	"encoding/json"

	"github.com/byronellis/ragtime/internal/protocol"
)

// ClaudeRawEvent represents the JSON structure Claude Code sends on stdin.
type ClaudeRawEvent struct {
	SessionID      string         `json:"session_id"`
	TranscriptPath string         `json:"transcript_path"`
	CWD            string         `json:"cwd"`
	ToolName       string         `json:"tool_name,omitempty"`
	ToolInput      map[string]any `json:"tool_input,omitempty"`
	ToolResponse   any            `json:"tool_response,omitempty"`
	Prompt         string         `json:"prompt,omitempty"`
	Source         string         `json:"source,omitempty"`
	PermissionMode string         `json:"permission_mode,omitempty"`
	HookEventName  string         `json:"hook_event_name,omitempty"`
	AgentID        string         `json:"agent_id,omitempty"`
	AgentType      string         `json:"agent_type,omitempty"`
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

	return &protocol.HookEvent{
		Agent:     "claude",
		EventType: eventType,
		SessionID: raw.SessionID,
		ToolName:  raw.ToolName,
		ToolInput: raw.ToolInput,
		Prompt:    raw.Prompt,
		CWD:       raw.CWD,
		Raw:       rawMap,
	}, nil
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
