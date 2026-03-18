# Ragtime

Ragtime is a programmable context injection and tool approval system for AI coding agents. It sits between your agent (Claude Code, Gemini CLI, etc.) and you, intercepting hook events and applying rules written in Starlark to control what context the agent sees, which tool calls get approved, and how you interact with the process.

## Why

I use multiple agent harnesses and kept rebuilding the same thing: a dynamic context injection layer that replaces static AGENTS.md files with something context-sensitive. Ragtime breaks that out into a standalone tool that works across agents.

Beyond context injection, ragtime provides:

- **Session indexing** — every agent session is chunked and indexed for semantic search, so agents (and you) can find relevant past work across any harness
- **Starlark rules** — dynamic logic for context injection, tool approval, RAG search, and interactive prompts
- **Live TUI dashboard** — real-time event feed, session tracking, and interactive modals for tool approval
- **Hook test mode** — develop and debug rules locally without running an agent

## Architecture

```
Agent (Claude Code / Gemini CLI)
  │ hook event (stdin JSON)
  ▼
rt hook ──► ragtime daemon ──► hook engine ──► rules (YAML + Starlark)
  │              │                                    │
  │              ├── session manager ──► RAG indexer   ├── rag.search()
  │              ├── event bus ──► TUI subscribers     ├── response.prompt()
  │              └── interaction manager               └── inject_input()
  │
  ◄── hook response (stdout JSON)
```

The daemon runs as a background process, communicating over a Unix socket. Hook events flow in, get matched against rules, and responses flow back — all within the agent's hook timeout window.

## Quick Start

```bash
# Build
go build -o rt ./cmd/ragtime

# Start the daemon
rt start

# Open the live dashboard
rt tui
```

### Setting Up Hooks

#### Claude Code

Add to your Claude Code settings (`.claude/settings.local.json`):

```json
{
  "hooks": {
    "PreToolUse": [{ "command": "rt hook --agent claude --event pre-tool-use" }],
    "PostToolUse": [{ "command": "rt hook --agent claude --event post-tool-use" }],
    "PermissionRequest": [{ "command": "rt hook --agent claude --event permission-request" }],
    "Stop": [{ "command": "rt hook --agent claude --event stop" }],
    "SubagentStop": [{ "command": "rt hook --agent claude --event subagent-stop" }],
    "Notification": [{ "command": "rt hook --agent claude --event notification" }],
    "SessionStart": [{ "command": "rt hook --agent claude --event session-start" }],
    "UserPromptSubmit": [{ "command": "rt hook --agent claude --event user-prompt-submit" }]
  }
}
```

### Writing Rules

Rules live in `~/.ragtime/rules/` (global) or `.ragtime/rules/` (per-project) as YAML files:

```yaml
# .ragtime/rules/rag-context.yaml
name: inject-project-docs
match:
  event: pre-tool-use
  tool: "Read|Write|Edit"
actions:
  - type: rag-search
    collections: [project-docs]
    query_from: tool_input.file_path
    top_k: 3
```

For dynamic logic, use Starlark:

```yaml
# .ragtime/rules/review-bash.yaml
name: review-bash-commands
match:
  event: pre-tool-use
  tool: Bash
actions:
  - type: starlark
    script: |
      cmd = event.tool_input.get("command", "")
      if "rm " in cmd or "drop " in cmd.lower():
          answer = response.prompt(
              text="## Destructive Command\n\n```bash\n" + cmd + "\n```\n\nAllow this?",
              type="approve_deny_cancel",
              default="deny",
              timeout=15,
          )
          if answer == "approve":
              response.approve()
          else:
              response.deny("blocked by review rule")
```

For permission-level control using Claude's built-in permission system:

```yaml
# ~/.ragtime/rules/review-permission.yaml
name: review-permission
match:
  event: permission-request
actions:
  - type: starlark
    script: |
      tool = event.tool_name
      cmd = event.tool_input.get("command", "")
      path = event.tool_input.get("file_path", "")

      detail = cmd if cmd else path
      if detail:
          text = "## Permission Request\n\n**Tool:** `" + tool + "`\n\n```\n" + detail + "\n```\n\nAllow this?"
      else:
          text = "## Permission Request\n\n**Tool:** `" + tool + "`\n\nAllow this?"

      if tui.connected():
          answer = response.prompt(
              text=text,
              type="approve_deny_cancel",
              default="approve",
              timeout=5,
          )
          if answer == "approve":
              response.approve()
          elif answer == "deny":
              response.deny("denied via TUI review")
      # If no TUI, fall through to agent's default behavior
```

This rule intercepts Claude Code's permission prompts and routes them to a TUI modal with a 5-second auto-approve countdown. If the TUI isn't open, the hook is a no-op and the agent's normal permission dialog appears.

### Searching Sessions

```bash
# Search past agent sessions
rt search sessions "how did we implement the auth middleware"

# List available collections
rt search --collections
```

### Testing Rules

Test rules locally without running an agent or daemon:

```bash
# Synthetic event with specific rule files
rt hook --test --tool Bash --input '{"command":"rm -rf /tmp"}' \
  --rule rules/review-bash.yaml --verbose

# Test multiple rules together
rt hook --test --tool Read --input '{"file_path":"src/main.go"}' \
  --rule rules/rag-context.yaml --rule rules/log-all.yaml

# Interactive TUI modal testing
rt hook --test --tui --tool Bash --input '{"command":"docker stop app"}' \
  --rule rules/review-bash.yaml
```

## Starlark API

Rules have access to the full event, response helpers, RAG search, TUI state, and interactive prompts. See [docs/starlark-api.md](docs/starlark-api.md) for the complete reference.

Key capabilities:

| API | Description |
|-----|-------------|
| `event.*` | Read event fields (agent, tool_name, tool_input, etc.) |
| `response.approve/deny/ask()` | Control tool permission |
| `response.inject_context(text)` | Add context visible to the agent |
| `response.prompt(text, type, ...)` | Interactive TUI modal with timeout |
| `response.set_output(key, value)` | Set raw agent output fields |
| `response.agent` | Current agent platform name |
| `rag.search(collection, query)` | Search indexed documents |
| `tui.connected()` | Check if TUI dashboard is open |
| `inject_input([...])` | Send keystrokes to terminal multiplexer |
| `log(...)` | Write to daemon log |

## Components

| Component | Description |
|-----------|-------------|
| `rt start/stop/restart` | Daemon lifecycle management |
| `rt hook` | Agent hook handler (stdin/stdout JSON relay) |
| `rt hook --test` | Local rule testing without daemon |
| `rt tui` | Live terminal dashboard |
| `rt search` | RAG collection search |
| `rt index` | Index management |
| `rt add` | Add content to collections |
| `rt status` | Daemon status |
| `rt rules` | List loaded rules |
| `rt session` | Session management |

## Documentation

- [Starlark API Reference](docs/starlark-api.md) — complete rule scripting API
- [Design Document](docs/design.md) — architecture and design decisions
- [Example Rules](docs/examples/rules/) — starter rule templates

## Building

```bash
go build -o rt ./cmd/ragtime
```

Requires Go 1.21+. Single binary, no external dependencies at runtime (embeddings require a local Ollama instance for RAG features).

## Status

Ragtime is in active early development — functional but not yet stable.

### What works

| Area | Status | Notes |
|------|--------|-------|
| Daemon lifecycle | Working | `rt start/stop/restart`, PID tracking, Unix socket IPC |
| Hook relay | Working | stdin/stdout JSON relay for Claude Code hooks; all event types supported |
| Rule engine | Working | YAML rule matching by event type, tool name, and glob patterns |
| Starlark scripting | Working | Full scripting in rule actions — conditionals, event inspection, response control |
| RAG indexing | Working | Ollama-backed embeddings, per-collection chunking and search |
| Session indexing | Working | Automatic session capture, chunked and indexed for cross-session search |
| TUI dashboard | Working | Live event feed, session panel, interaction modals (approve/deny/cancel) |
| Hook test mode | Working | `rt hook --test` for local rule development without a daemon |
| Permission requests | Working | `PermissionRequest` event support with TUI-based approval flow |
| Markdown rendering | Working | Glamour-based rendering in TUI modals |
| Multi-agent support | Partial | Hook relay works with any agent; Starlark `response.agent` exposes platform name. Session capture tested with Claude Code only |

### What's next

- Curated session summaries (compress session chunks to save space)
- Time-range and project-scoped search
- Session summary on connect
- Agent note-taking (`rt add` from Starlark)
- More example rules and cookbook patterns
- Stability, error handling, and documentation polish

## License

Apache License 2.0 — see [LICENSE](LICENSE) for details.
