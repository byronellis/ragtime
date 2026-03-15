package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ClaudeHooksConfig represents the hooks section of Claude Code settings.
type ClaudeHooksConfig struct {
	Hooks map[string][]ClaudeHookEntry `json:"hooks"`
}

// ClaudeHookEntry is a single hook registration.
type ClaudeHookEntry struct {
	Matcher string           `json:"matcher"`
	Hooks   []ClaudeHookDef  `json:"hooks"`
}

// ClaudeHookDef defines the hook command.
type ClaudeHookDef struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// GenerateClaudeHooks creates the Claude Code hooks.json configuration.
func GenerateClaudeHooks() *ClaudeHooksConfig {
	events := []struct {
		name      string
		eventFlag string
	}{
		{"PreToolUse", "pre-tool-use"},
		{"PostToolUse", "post-tool-use"},
		{"Notification", "notification"},
		{"Stop", "stop"},
		{"UserPromptSubmit", "user-prompt-submit"},
		{"SessionStart", "session-start"},
		{"SubagentStop", "subagent-stop"},
	}

	hooks := make(map[string][]ClaudeHookEntry)
	for _, e := range events {
		hooks[e.name] = []ClaudeHookEntry{
			{
				Matcher: ".*",
				Hooks: []ClaudeHookDef{
					{
						Type:    "command",
						Command: fmt.Sprintf("rt hook --agent claude --event %s", e.eventFlag),
					},
				},
			},
		}
	}

	return &ClaudeHooksConfig{Hooks: hooks}
}

// GenerateClaudeSkillsMarkdown generates the CLAUDE.md skill documentation block.
func GenerateClaudeSkillsMarkdown() string {
	var sb strings.Builder
	sb.WriteString("# Ragtime Tools\n\n")
	sb.WriteString("You have access to a local RAG system via the `rt` command. Use these tools to search for context, add knowledge, and track session state.\n\n")

	for _, skill := range AllSkills() {
		sb.WriteString(fmt.Sprintf("- `%s` — %s\n", skill.Command, skill.Description))
	}

	sb.WriteString("\nExample: to find relevant documentation before working on a component, run:\n")
	sb.WriteString("```\nrt search project-docs \"component name or concept\"\n```\n")

	return sb.String()
}

// SetupClaude generates and writes hooks.json and CLAUDE.md for Claude Code.
func SetupClaude(projectDir string) error {
	// Generate hooks config
	hooksConfig := GenerateClaudeHooks()
	hooksJSON, err := json.MarshalIndent(hooksConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal hooks: %w", err)
	}

	// Write .claude/settings.local.json (local, gitignored)
	claudeDir := filepath.Join(projectDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return fmt.Errorf("create .claude dir: %w", err)
	}

	settingsPath := filepath.Join(claudeDir, "settings.local.json")

	// Read existing settings if present
	existing := make(map[string]any)
	if data, err := os.ReadFile(settingsPath); err == nil {
		json.Unmarshal(data, &existing)
	}

	// Merge hooks into existing settings
	var hooksMap map[string]any
	json.Unmarshal(hooksJSON, &hooksMap)
	for k, v := range hooksMap {
		existing[k] = v
	}

	mergedJSON, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}

	if err := os.WriteFile(settingsPath, mergedJSON, 0o644); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}
	fmt.Printf("Wrote hooks to %s\n", settingsPath)

	// Generate skills markdown
	skillsMD := GenerateClaudeSkillsMarkdown()

	// Write to .claude/rules/ragtime.md (auto-loaded by Claude Code)
	rulesDir := filepath.Join(claudeDir, "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		return fmt.Errorf("create rules dir: %w", err)
	}

	rulesPath := filepath.Join(rulesDir, "ragtime.md")
	if err := os.WriteFile(rulesPath, []byte(skillsMD), 0o644); err != nil {
		return fmt.Errorf("write skills: %w", err)
	}
	fmt.Printf("Wrote skill definitions to %s\n", rulesPath)

	return nil
}
