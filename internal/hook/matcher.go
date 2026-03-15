package hook

import (
	"path/filepath"
	"strings"

	"github.com/byronellis/ragtime/internal/config"
	"github.com/byronellis/ragtime/internal/protocol"
)

// Match tests whether a HookEvent matches a MatchConfig.
func Match(event *protocol.HookEvent, m config.MatchConfig) bool {
	if m.Agent != "" && !matchGlob(m.Agent, event.Agent) {
		return false
	}
	if m.Event != "" && !matchGlob(m.Event, event.EventType) {
		return false
	}
	if m.Tool != "" && !matchGlob(m.Tool, event.ToolName) {
		return false
	}
	if m.PathGlob != "" {
		path := extractPath(event)
		if path == "" || !matchGlob(m.PathGlob, path) {
			return false
		}
	}
	return true
}

// matchGlob performs a case-sensitive glob match. "*" matches everything.
func matchGlob(pattern, value string) bool {
	if pattern == "*" {
		return true
	}
	// Support pipe-separated alternatives: "Read|Write"
	for _, alt := range strings.Split(pattern, "|") {
		matched, err := filepath.Match(alt, value)
		if err == nil && matched {
			return true
		}
	}
	return false
}

// extractPath pulls a file path from the event's tool input.
func extractPath(event *protocol.HookEvent) string {
	if event.ToolInput == nil {
		return ""
	}
	// Try common path fields
	for _, key := range []string{"file_path", "path", "command"} {
		if v, ok := event.ToolInput[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}
