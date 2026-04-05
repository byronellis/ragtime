# Ragtime

Ragtime is a programmable context injection and tool approval system for AI coding agents. It sits between your agent (Claude Code, Gemini CLI, etc.) and you, intercepting hook events and applying rules written in Starlark to control what context the agent sees, which tool calls get approved, and how you interact with the process.

## Why

I use multiple agent harnesses and kept rebuilding the same thing: a dynamic context injection layer that replaces static AGENTS.md files with something context-sensitive. Ragtime breaks that out into a standalone tool that works across agents.

Beyond context injection, ragtime provides:

- **Session indexing** — every agent session is chunked and indexed for semantic search, so agents (and you) can find relevant past work across any harness
- **Starlark rules** — dynamic logic for context injection, tool approval, RAG search, and interactive prompts
- **Live TUI dashboard** — real-time event feed, session tracking, and interactive modals for tool approval
- **FUSE filesystem** — mount a live view of all active agent sessions, shells, and telemetry as browsable files
- **Shell sessions** — `rt sh` launches shells with full ragtime integration, correlating PTY output with agent hook events
- **Statusline telemetry** — record Claude Code cost and context window usage per session
- **Hook test mode** — develop and debug rules locally without running an agent

## Architecture

```
Agent (Claude Code / Gemini CLI)
  │ hook event (stdin JSON)       statusLine (stdin JSON)
  ▼                               ▼
rt hook ──► ragtime daemon ◄── rt statusline
  │              │
  │              ├── session manager ──► RAG indexer
  │              ├── event bus ──► TUI subscribers
  │              ├── shell manager (PTY sessions)
  │              ├── interaction manager
  │              └── SQLite (statusline telemetry)
  │
  ◄── hook response (stdout JSON)

rt mount ──► FUSE filesystem (~/.ragtime/fs/)
               ├── active/      live session dashboards
               ├── agents/      per-agent PTY output + input
               ├── sessions/    session history
               ├── shells/      running shell processes
               └── collections/ RAG index listings
```

The daemon runs as a background process, communicating over a Unix socket. Hook events flow in, get matched against rules, and responses flow back — all within the agent's hook timeout window.

## Quick Start

```bash
# Build
go build -o rt ./cmd/ragtime
codesign --sign "Apple Development: Your Name" rt
cp rt /usr/local/bin/rt

# Start the daemon
rt start

# Open the live dashboard
rt tui

# Mount the FUSE filesystem
rt mount
ls ~/.ragtime/fs/active/   # live session summaries
```

### Setting Up Hooks

#### Claude Code

Add to your Claude Code settings (`.claude/settings.local.json`):

```json
{
  "hooks": {
    "PreToolUse": [{ "matcher": ".*", "hooks": [{ "type": "command", "command": "rt hook --agent claude --event pre-tool-use" }] }],
    "PostToolUse": [{ "matcher": ".*", "hooks": [{ "type": "command", "command": "rt hook --agent claude --event post-tool-use" }] }],
    "PermissionRequest": [{ "matcher": ".*", "hooks": [{ "type": "command", "command": "rt hook --agent claude --event permission-request" }] }],
    "Stop": [{ "matcher": ".*", "hooks": [{ "type": "command", "command": "rt hook --agent claude --event stop" }] }],
    "SubagentStop": [{ "matcher": ".*", "hooks": [{ "type": "command", "command": "rt hook --agent claude --event subagent-stop" }] }],
    "Notification": [{ "matcher": ".*", "hooks": [{ "type": "command", "command": "rt hook --agent claude --event notification" }] }],
    "SessionStart": [{ "matcher": ".*", "hooks": [{ "type": "command", "command": "rt hook --agent claude --event session-start" }] }],
    "UserPromptSubmit": [{ "matcher": ".*", "hooks": [{ "type": "command", "command": "rt hook --agent claude --event user-prompt-submit" }] }]
  },
  "statusLine": {
    "type": "command",
    "command": "rt statusline --agent claude"
  }
}
```

The `statusLine` entry records per-turn cost, token usage, and context window percentage to SQLite. Note the camelCase key — `statusLine`, not `statusline`.

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

### Searching Sessions

```bash
# Search past agent sessions
rt search sessions "how did we implement the auth middleware"

# List available collections
rt search --collections
```

### Testing Rules

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

## FUSE Filesystem

`rt mount` exposes a live read-only (with writable exceptions) filesystem at `~/.ragtime/fs/`:

```
~/.ragtime/fs/
├── active/
│   └── <session-id>/
│       ├── summary.txt      # cost, tokens, context window %, model
│       └── status.json      # full session state as JSON
├── agents/
│   └── <agent-id>/
│       ├── output.log       # live PTY output (for rt sh sessions)
│       ├── input            # writable — send text to the agent
│       └── notes/           # writable per-session notes
├── sessions/                # session history
├── shells/                  # running rt sh processes
└── collections/             # RAG index listings
```

`active/` is populated from recent statusline events so it survives daemon restarts. Each session directory shows the current model, cumulative cost, token counts, and context window percentage.

```bash
rt mount            # mount at ~/.ragtime/fs/
rt umount           # unmount
cat ~/.ragtime/fs/active/*/summary.txt   # check all active sessions
```

## Shell Sessions

`rt sh` launches a shell (or wraps a command) with full ragtime integration:

```bash
rt sh                        # interactive shell
rt sh new -- claude          # launch agent with correlation
rt sh new --name my-session  # named session
```

Shells launched via `rt sh` set `RAGTIME_SOCKET` and `RAGTIME_SHELL_ID` so hook events and statusline telemetry are automatically correlated with the PTY session. The shell's output is captured and visible in `active/<session>/output.log` via FUSE.

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
| `rt statusline` | Record Claude Code statusLine telemetry to SQLite |
| `rt tui` | Live terminal dashboard |
| `rt mount / rt umount` | Mount/unmount the FUSE filesystem |
| `rt sh` | Launch a shell or command with ragtime integration |
| `rt search` | RAG collection search |
| `rt index` | Index management |
| `rt add` | Add content to collections |
| `rt status` | Daemon status |
| `rt rules` | List loaded rules |
| `rt session` | Session management |

## Documentation

- [Starlark API Reference](docs/starlark-api.md) — complete rule scripting API
- [Design Document](docs/design.md) — architecture and design decisions

## Building

```bash
go build -o rt ./cmd/ragtime
codesign --sign "Apple Development: Your Name (TEAMID)" rt
```

Requires Go 1.21+. Single binary, no external dependencies at runtime (embeddings require a local Ollama instance for RAG features). The FUSE filesystem requires [fuse-t](https://www.fuse-t.org/) on macOS.

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
| TUI dashboard | Working | Live event feed, session panel, interaction modals (approve/deny/cancel), uptime ticker |
| TUI search | Working | Press `/` to run semantic search against the sessions collection from within the TUI |
| Hook test mode | Working | `rt hook --test` for local rule development without a daemon |
| Permission requests | Working | `PermissionRequest` event support with TUI-based approval flow and auto-approve countdown |
| Markdown rendering | Working | Glamour-based rendering in TUI modals |
| Hot reload | Working | Rule changes take effect immediately without daemon restart |
| Session summary on connect | Working | `session-summary` rule injects recent session context on `SessionStart` via RAG search |
| Statusline telemetry | Working | `rt statusline` records cost, tokens, model, and context window % per turn to SQLite |
| FUSE filesystem | Working | `rt mount` exposes live session data, agent PTY output, and notes as browsable files |
| Shell sessions | Working | `rt sh` launches correlated shell/agent sessions; PTY output captured in FUSE |
| Active session dashboard | Working | `active/` FUSE dir shows per-session summary with cost/context window from DB |
| Hook/shell correlation | Working | Hook events from `rt sh` sessions carry shell ID for cross-correlation |
| Multi-agent support | Partial | Hook relay works with any agent; Starlark `response.agent` exposes platform name. Session capture tested with Claude Code only |

### What's next

- Project-scoped search — filter `rt search sessions` by project/repo to reduce noise
- Agent note-taking — `rt note` or `rt add` from Starlark to leave breadcrumbs for future sessions
- Curated session summaries — compress session chunks to save index space
- Rule hit analytics — see which rules fire most often to help tune configurations
- More example rules and cookbook patterns
- Stability, error handling, and documentation polish

## License

Apache License 2.0 — see [LICENSE](LICENSE) for details.
