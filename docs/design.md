# Ragtime Design Document

## Overview

Ragtime is a programmable context management tool for AI coding agents. It integrates with the hook/plugin systems of agent platforms (initially Claude Code and Gemini CLI) to give users dynamic control over what context is provided to agents and when.

The core idea: rather than relying on static context files, Ragtime lets you define rules and scripts that respond to agent lifecycle events (tool calls, prompts, conversations) and inject relevant context on the fly — including RAG-powered retrieval from local document stores.

## Architecture

Ragtime is a single Go binary named `rt` (short to type, minimal agent context consumption). It supports multiple operating modes:

```
rt daemon    — long-running background process
rt hook      — CLI invoked by agent hook systems
rt tui       — terminal UI for monitoring/interaction
rt index     — manage RAG indexes
rt search    — search RAG collections
rt add       — add content to RAG collections
rt session   — inspect/annotate agent sessions
rt rules     — list/explain/manage rules
rt interview — interactive knowledge capture session
rt setup     — generate agent hook configs and skill definitions
rt <other>   — additional CLI subcommands (config, etc.)
```

### Component Overview

```
┌──────────────────────────────────────────────────────────┐
│                     Agent Platforms                       │
│              (Claude Code, Gemini CLI)                    │
└──────────┬───────────────────────────────┬───────────────┘
           │ hook invocation               │ hook invocation
           ▼                               ▼
┌──────────────────────┐     ┌──────────────────────┐
│   rt hook       │     │   rt hook        │
│   (CLI process)      │     │   (CLI process)       │
└──────────┬───────────┘     └──────────┬────────────┘
           │ unix socket                 │ unix socket
           ▼                             ▼
┌──────────────────────────────────────────────────────────┐
│                    rt daemon                        │
│                                                          │
│  ┌─────────────┐ ┌──────────────┐ ┌───────────────────┐ │
│  │ Hook Engine  │ │ Script Engine│ │ RAG Engine        │ │
│  │ (all events)│ │ (Starlark)   │ │ (embed + search)  │ │
│  └─────────────┘ └──────────────┘ └───────────────────┘ │
│  ┌─────────────┐ ┌──────────────┐ ┌───────────────────┐ │
│  │ Session Mgr  │ │ Web Server   │ │ Event Bus         │ │
│  │ (per-agent)  │ │ (HTTP)       │ │ (internal pub/sub)│ │
│  └─────────────┘ └──────────────┘ └───────────────────┘ │
│  ┌─────────────┐ ┌──────────────┐                       │
│  │ Mux Detector │ │ Approval Mgr │                       │
│  │ (tmux/etc)  │ │ (countdown)  │                       │
│  └─────────────┘ └──────────────┘                       │
└──────────────────────────────────────────────────────────┘
           ▲                             ▲
           │ unix socket                 │ HTTP
           │                             │
┌──────────────────────┐     ┌──────────────────────┐
│   rt tui        │     │   Web Browser         │
│   (Bubble Tea)       │     │                       │
└──────────────────────┘     └──────────────────────┘
```

## Daemon

The daemon is the central process. It runs in the background and manages all state.

- **Socket**: listens on `~/.ragtime/daemon.sock` (Unix domain socket)
- **Lifecycle**: started on demand (first `rt hook` or `rt tui` invocation auto-starts it), or explicitly via `rt daemon`
- **Web server**: also binds an HTTP port (default `localhost:7483`) for the web UI and API
- **State directory**: `~/.ragtime/` holds config, indexes, logs, and the socket

### Auto-start

When `rt hook` or `rt tui` connects and the daemon isn't running, it spawns one in the background automatically. This keeps the UX frictionless — users don't need to remember to start a service.

## CLI: `rt hook`

This is the fast-path command invoked by agent hook systems. It must:

1. Connect to the daemon over the Unix socket
2. Forward the hook event (stdin + env vars from the agent)
3. Stream the daemon's response to stdout
4. Exit quickly — agents expect hooks to be fast

The hook CLI itself does no processing; it's a thin relay to the daemon.

### Claude Code Integration

Claude Code hooks are configured in `.claude/hooks.json` or the settings UI. Ragtime registers for **all** lifecycle events to provide full coverage:

- `PreToolUse` — intercept tool calls before execution (context injection, approval control)
- `PostToolUse` — react after tool execution (logging, follow-up context)
- `Notification` — respond to agent notifications
- `Stop` — act when the agent completes a turn
- `SubagentStop` — act when a subagent completes
- Any future hook types as Claude Code evolves

Hook commands receive JSON on stdin and emit JSON on stdout. Example config:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": ".*",
        "command": "rt hook --agent claude --event pre-tool-use"
      }
    ],
    "PostToolUse": [
      {
        "matcher": ".*",
        "command": "rt hook --agent claude --event post-tool-use"
      }
    ],
    "Notification": [
      {
        "matcher": ".*",
        "command": "rt hook --agent claude --event notification"
      }
    ],
    "Stop": [
      {
        "matcher": ".*",
        "command": "rt hook --agent claude --event stop"
      }
    ]
  }
}
```

Ragtime can generate this configuration automatically via `rt setup claude`.

### Gemini CLI Integration

Gemini CLI supports a similar hook/extension model. The exact integration surface will be determined during implementation, but the pattern is the same: Ragtime registers as a hook handler for all available events, and the CLI shim relays events to the daemon. Setup via `rt setup gemini`.

### Universal Hooks

Where possible, Ragtime defines a **universal hook abstraction** that normalizes events across agent platforms. This allows users to write rules and scripts that work regardless of which agent is running.

The universal event model:

```
HookEvent:
  agent: string          # "claude", "gemini", etc.
  event_type: string     # normalized: "pre-tool-use", "post-tool-use", "stop", "notification"
  session_id: string
  tool_name: string?     # for tool-related events
  tool_input: dict?      # normalized tool input
  raw: dict              # original agent-specific payload (escape hatch)
  mux: MuxInfo?          # multiplexer context (see Multiplexer Awareness)
```

Agent-specific adapters translate native hook payloads into this universal format. Scripts can work against the universal model for portability, or access `event.raw` for agent-specific behavior.

## Hook Engine

The hook engine is the core of Ragtime's programmability. When a hook event arrives, the engine:

1. Matches the event against configured rules
2. Executes any matching scripts
3. Aggregates results (context to inject, actions to take)
4. Returns the response to the calling hook CLI

### Hook Actions

Beyond context injection, hooks can take several types of actions:

- **inject-context** — add text to the agent's context
- **approve** — auto-approve a tool use (with optional countdown/delay)
- **deny** — block a tool use with a reason
- **rag-search** — search collections and inject results
- **notify** — send a notification to TUI/web
- **script** — run a Starlark script for custom logic
- **mux-command** — inject a command into a multiplexer pane (see Multiplexer Awareness)

### Tool Approval System

Ragtime can manage tool approval with configurable policies:

```yaml
rules:
  - name: "auto-approve-reads"
    match:
      event: pre-tool-use
      tool: "Read"
    actions:
      - type: approve

  - name: "approve-writes-with-countdown"
    match:
      event: pre-tool-use
      tool: "Write"
    actions:
      - type: approve
        countdown: 5  # seconds — user can cancel via TUI/web/mux
```

The countdown approval flow:
1. Hook fires for a tool use
2. Ragtime starts a countdown (visible in TUI/web)
3. If the user doesn't cancel within the window, the tool is approved
4. User can cancel from TUI, web UI, or by sending a command via their multiplexer

### Rules

Rules are the primary configuration unit. A rule matches on event properties and specifies what to do:

```yaml
rules:
  - name: "inject-api-docs"
    match:
      agent: "*"           # universal — works across agents
      event: pre-tool-use
      tool: "Read"
      path_glob: "api/**"
    actions:
      - type: inject-context
        script: "api_docs.star"

  - name: "auto-rag-on-question"
    match:
      event: pre-tool-use
      tool: "AskUser"
    actions:
      - type: rag-search
        collections: ["project-docs"]
        query_from: "input.question"
        top_k: 5
```

## Script Engine

Ragtime embeds [Starlark](https://github.com/google/starlark-go) as its scripting language.

Rationale:
- Pure Go, no CGo required
- Python-like syntax familiar to most developers (and to Bazel users)
- Deterministic execution (no `while` loops, no mutation of globals) makes scripts predictable and safe
- First-class support for structured data (dicts, lists) maps well to JSON hook payloads
- Battle-tested at scale (Bazel, Buck, Tilt)
- Google-maintained with a stable API

Starlark's intentional limitations (no unbounded loops, no imports of arbitrary modules) are actually features for a hook scripting system — scripts should be fast and side-effect-free by default, with Ragtime providing the APIs for I/O.

### Ragtime Starlark API

Scripts have access to a `ragtime` module with these built-in functions:

```python
# Context management
ragtime.inject_context(content, position="before")  # inject text into agent context
ragtime.no_op()                                       # do nothing
ragtime.approve(countdown=0)                          # approve tool use
ragtime.deny(reason="...")                            # deny tool use

# RAG access
ragtime.rag_search(collection, query, top_k=5)       # search a collection
ragtime.rag_add(collection, content, metadata={})     # add content to a collection

# Session access
ragtime.session.history                               # list of prior events in session
ragtime.session.get(key)                              # read session-scoped state
ragtime.session.set(key, value)                       # write session-scoped state

# Multiplexer
ragtime.mux.send_keys(pane, keys)                    # send keystrokes to a mux pane
ragtime.mux.list_panes()                             # list visible panes

# Utilities
ragtime.log(level, message)                          # structured logging
ragtime.config(key)                                  # read ragtime config values
```

Example script:

```python
# rules/api_docs.star

def on_hook(event):
    """Called when a matching hook fires."""
    path = event.tool_input.get("file_path", "")

    if path.startswith("api/"):
        # Pull relevant API documentation via RAG
        docs = ragtime.rag_search("api-docs", query=path, top_k=3)
        return ragtime.inject_context(
            content=format_docs(docs),
            position="before",
        )

    return ragtime.no_op()

def format_docs(results):
    lines = ["Relevant API documentation:"]
    for r in results:
        lines.append("---")
        lines.append(r.content)
    return "\n".join(lines)
```

## RAG Engine

Ragtime provides local RAG (Retrieval-Augmented Generation) capabilities so scripts and rules can search over user-defined document collections.

### Content Sources

Users can populate collections from multiple sources:

- **File indexing**: `rt index create my-docs --path ./docs/`
- **Direct addition**: `rt add my-collection "some content" --metadata key=value`
- **Session history**: automatically indexed per-session (see Session Manager)
- **Interview capture**: interactive knowledge capture (see Interview System)
- **Stdin/pipe**: `cat notes.txt | rt add my-collection --source notes.txt`

### CLI

```
rt index create <name> --path <dir>    # create index from directory
rt index update <name>                  # re-index
rt index list                           # list all indexes
rt index delete <name>                  # remove an index
rt add <collection> <content>           # add content directly
rt search <collection> <query>          # search from CLI
```

These CLI commands are also suitable for use as **agent skills** — an agent can invoke `rt search project-docs "how does auth work"` as a tool to query the RAG system directly.

### Storage

Indexes live in `~/.ragtime/indexes/` (global) or `.ragtime/indexes/` (per-project). Each index stores:
- Document chunks with metadata
- Embedding vectors

### Multi-Project Support

Indexes and rules support both global and per-project scoping:

- **Global**: `~/.ragtime/indexes/`, `~/.ragtime/rules/` — available everywhere
- **Per-project**: `.ragtime/indexes/`, `.ragtime/rules/` at the git root — project-specific overrides
- **Resolution**: per-project rules/indexes take precedence; global ones serve as defaults
- **Project detection**: Ragtime walks up from CWD to find the nearest `.git` directory

### Embedding Providers

The embedding backend is pluggable via a provider interface. Ragtime communicates with all providers via their HTTP APIs (no shelling out):

| Provider | Type | Default Model | Notes |
|----------|------|---------------|-------|
| **Built-in Go** | Local | nomic-embed-text-v1.5 (137M) | Pure-Go GGUF inference. Slower but zero dependencies — good for getting started and small collections. |
| **Ollama** | Local | nomic-embed-text | HTTP API (`/api/embeddings`). High performance, wide model selection. |
| **vLLM** | Local | (configurable) | OpenAI-compatible `/v1/embeddings` endpoint. GPU-accelerated. |
| **MLX Server** | Local | (configurable) | `/v1/embeddings` endpoint. Optimized for Apple Silicon. |
| **OpenAI** | Remote | text-embedding-3-small | `/v1/embeddings` API. |
| **Voyage** | Remote | voyage-code-3 | Embedding-focused API, strong on code. |

Configuration:

```yaml
embeddings:
  provider: ollama           # or builtin, vllm, mlx, openai, voyage
  endpoint: http://localhost:11434  # for local providers
  model: nomic-embed-text
  # api_key: ...             # for remote providers
```

#### Built-in Embedding Model

The built-in provider uses **nomic-embed-text-v1.5** — a 137M parameter model based on a modified BERT architecture (rotary position embeddings, SwiGLU). Key properties:

- Apache 2.0 licensed, fully open weights
- Available in GGUF format (~50-100 MB at Q4 quantization)
- Supports Matryoshka embeddings (variable dimensions 64-768) — useful for trading quality vs. storage
- 8192 token context window
- General-purpose text embeddings — adequate for code, though not code-specialized

For code-heavy workloads, users should consider a dedicated provider (Ollama with `nomic-embed-code`, or Voyage's `voyage-code-3`) which can leverage GPU acceleration for larger code-specific models.

The built-in model is loaded on first use and downloaded automatically if not present (~50-100 MB). It is **not** bundled in the binary to keep the binary size reasonable — instead it's cached in `~/.ragtime/models/`.

### Search

Search is exposed to:
- Scripts via `ragtime.rag_search(collection, query, top_k)`
- Rules via the `rag-search` action type
- CLI via `rt search <collection> <query>`
- Agents as a tool/skill

Results include the matched chunk text, source document path, and similarity score.

## Interview System

The interview system is an interactive knowledge capture flow. It lets users build up RAG collections through guided conversation:

```
rt interview <collection>
```

This launches an interactive session (in TUI or terminal) that:

1. Asks the user structured questions about a topic
2. Captures responses as documents in the specified collection
3. Can follow up with clarifying questions based on prior answers
4. Supports attaching files, code snippets, or URLs as source material

Use cases:
- Onboarding: capture institutional knowledge from a team member
- Project context: document decisions, architecture, and rationale
- Personal notes: build a searchable knowledge base from conversational input

The interview can also be driven from the web UI or TUI.

## Session Manager

The daemon tracks per-agent sessions. A "session" corresponds to a single agent conversation (e.g., one Claude Code session). This allows Ragtime to:

- Accumulate context about what the agent has done so far
- Avoid re-injecting the same context repeatedly
- Track which rules have fired and their results
- Provide session-scoped state to scripts
- **Feed session history into the RAG system** — prior tool calls, injected context, and agent actions become searchable, allowing cross-session learning

Sessions are identified by a combination of agent type + session ID (derived from environment variables the agents provide).

### Session-RAG Integration

Session events are automatically indexed into a `sessions` collection. This enables:
- Scripts that reference what happened earlier in the session or in prior sessions
- Cross-session context: "last time we worked on the auth module, we..."
- Searchable audit trail of agent activity

## Multiplexer Awareness

Ragtime detects terminal multiplexers from environment variables:

| Multiplexer | Detection | Interaction |
|-------------|-----------|-------------|
| **tmux** | `$TMUX`, `$TMUX_PANE` | `tmux send-keys`, `tmux display-message` |
| **Zellij** | `$ZELLIJ`, `$ZELLIJ_SESSION_NAME` | Zellij CLI actions |
| **GNU Screen** | `$STY`, `$WINDOW` | `screen -X stuff` |

This enables powerful workflows:

- **Remote tool approval**: when a countdown approval is running, Ragtime can display a notification in a multiplexer status bar or adjacent pane, and accept cancel commands sent to a designated pane
- **Cross-pane injection**: send commands or text to other panes (e.g., trigger a build in a side pane)
- **Agent coordination**: when multiple agents run in different panes, Ragtime can coordinate context between them

### Mux API in Scripts

```python
def on_hook(event):
    if event.mux and event.mux.type == "tmux":
        # Show approval countdown in tmux status
        ragtime.mux.send_keys(event.mux.pane, "")
        ragtime.mux.display_message(
            "Ragtime: approving Write in 5s — press 'c' to cancel"
        )
    return ragtime.approve(countdown=5)
```

## Web Interface

The daemon serves a web UI on its HTTP port. Capabilities:

- **Dashboard**: overview of active sessions, recent hook events, rule matches
- **Session inspector**: drill into a specific agent session to see event history, injected context, script executions
- **Rule editor**: view and edit rules (changes are written back to the config file)
- **Index management**: create/update/delete RAG indexes, browse indexed documents, add content
- **Interview UI**: web-based interview for knowledge capture
- **Approval queue**: pending tool approvals with cancel buttons
- **Log viewer**: real-time log stream

The web UI is served as embedded static assets (Go `embed` package) so it ships inside the single binary.

### Web Authentication

The web interface uses a Zellij-style session code for first-time access:

1. When the daemon starts, it generates a short-lived session code (displayed in daemon logs, TUI, and terminal output)
2. On first browser visit, the user is prompted to enter this code
3. Once entered, the browser receives a session token (stored as a cookie) that grants access until the daemon restarts or the token expires
4. The session code can be regenerated via `rt web-code` if needed

This keeps the web UI secure without requiring full user/password infrastructure, which matters when the HTTP port is exposed over SSH tunnels.

## TUI Interface

The TUI provides similar functionality to the web UI but in the terminal. Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) and styled with [Lip Gloss](https://github.com/charmbracelet/lipgloss).

Key views:
- Live event stream (hook events as they arrive)
- Session list and detail
- Rule list with match counts
- RAG index status and search
- Approval queue with countdown timers
- Interview mode
- Log viewer

The TUI connects to the daemon over the same Unix socket used by the hook CLI.

## Event Bus

Internal pub/sub system that connects daemon components. When a hook event arrives:

1. Hook engine publishes the event
2. Interested components subscribe (session manager, RAG indexer, TUI/web push, logging)
3. Results flow back through the bus

This decouples components and makes it easy to add new event consumers (e.g., metrics, webhooks).

## Configuration

Configuration lives in `~/.ragtime/config.yaml` (global) with optional per-project overrides in `.ragtime/config.yaml` at the git root. Per-project config is merged on top of global config.

```yaml
daemon:
  socket: ~/.ragtime/daemon.sock
  http_port: 7483
  log_level: info

agents:
  claude:
    enabled: true
  gemini:
    enabled: true

embeddings:
  provider: ollama
  endpoint: http://localhost:11434
  model: nomic-embed-text
  # api_key: ... (for remote providers)

rules:
  # inline rules or path to rules directory
  - name: "example"
    match: { event: "*" }
    actions:
      - type: log
```

Rules can also be defined as individual files in `~/.ragtime/rules/` (global) or `.ragtime/rules/` (per-project).

### Hot Reload

Ragtime watches configuration files, rules, and scripts for changes using filesystem notifications (fsnotify). Changes are picked up automatically without requiring a daemon restart or reload command. The TUI and web UI show a brief notification when config is reloaded.

## Directory Layout

Global (`~/.ragtime/`):

```
~/.ragtime/
├── config.yaml
├── daemon.sock
├── daemon.pid
├── models/
│   └── nomic-embed-text-v1.5-Q4_K_M.gguf
├── rules/
│   ├── api_docs.star
│   └── auto_context.star
├── indexes/
│   ├── sessions/
│   └── my-notes/
└── logs/
    └── ragtime.log
```

Per-project (`.ragtime/` at git root):

```
.ragtime/
├── config.yaml          # project-specific overrides
├── rules/
│   └── project_hooks.star
└── indexes/
    └── project-docs/
```

## Project Structure (source)

```
ragtime/
├── cmd/
│   └── ragtime/
│       └── main.go          # CLI entrypoint
├── internal/
│   ├── daemon/              # daemon lifecycle, socket server
│   ├── hook/                # hook engine, rule matching, universal events
│   ├── hook/adapters/       # per-agent event normalization (claude, gemini)
│   ├── approval/            # tool approval with countdown
│   ├── script/              # starlark script engine + ragtime module
│   ├── rag/                 # RAG engine (indexing, search, chunking)
│   ├── rag/providers/       # embedding providers (builtin, ollama, vllm, mlx, openai)
│   ├── session/             # session management + RAG integration
│   ├── bus/                 # internal event bus
│   ├── mux/                 # multiplexer detection + interaction
│   ├── interview/           # interactive knowledge capture
│   ├── web/                 # web server + embedded UI + session code auth
│   ├── tui/                 # terminal UI (Bubble Tea + Lip Gloss)
│   ├── skills/              # agent skill definitions + setup generators
│   ├── config/              # configuration loading + hot reload (fsnotify)
│   ├── project/             # project detection (git root, scope resolution)
│   └── protocol/            # wire protocol (daemon <-> CLI)
├── web/                     # web UI source (if using a JS framework)
├── docs/
│   └── design.md
├── go.mod
├── go.sum
└── README.md
```

## Wire Protocol

Communication between the hook CLI and daemon (over the Unix socket) uses a simple length-prefixed JSON protocol:

```
[4 bytes: payload length (big-endian uint32)][JSON payload]
```

Message types:
- `hook_event` — CLI → daemon, forwarding a hook invocation
- `hook_response` — daemon → CLI, the response to emit
- `subscribe` — TUI → daemon, subscribe to event stream
- `event` — daemon → TUI, pushed events
- `command` — CLI → daemon, administrative commands (index, config, etc.)

## Agent Skills

Ragtime can expose its capabilities as **agent skills** — structured tool descriptions that agents understand and can invoke autonomously. This turns Ragtime from a passive hook responder into an active collaborator that agents can query on demand.

### Skill Generation

`rt setup` generates agent-specific skill/tool definitions alongside hook configurations:

**Claude Code**: Skills are registered as Bash tool patterns in CLAUDE.md or as custom slash commands. The agent learns it can invoke `rt` commands directly:

```markdown
# Tools available via Ragtime

You have access to a local RAG system via the `rt` command:
- `rt search <collection> <query>` — search indexed documents for relevant context
- `rt search --collections` — list available collections
- `rt add <collection> <content> [--metadata key=value]` — add knowledge to a collection
- `rt index list` — list all indexed collections with stats
- `rt session history [--last N]` — review recent session activity
- `rt session note <text>` — save a note to the current session for future reference
```

**Gemini CLI**: Equivalent skill definitions in Gemini's tool/extension format.

### Skill Categories

| Skill | Command | Description |
|-------|---------|-------------|
| **Search** | `rt search <collection> <query>` | Semantic search across indexed documents |
| **List Collections** | `rt search --collections` | Show available RAG collections with doc counts |
| **Add Knowledge** | `rt add <collection> <content>` | Add new content to a collection |
| **Session History** | `rt session history` | Review what happened in prior sessions |
| **Session Note** | `rt session note <text>` | Save a note for cross-session context |
| **Index Status** | `rt index list` | Show all indexes and their health |
| **Explain Rule** | `rt rules explain <name>` | Show what a rule does and when it fires |

### Self-Hosting: Ragtime for Ragtime Development

Ragtime is designed to support its own development. As the project matures, we use `rt` to enhance the agent experience when working on the ragtime codebase itself:

- **Project knowledge base**: index the design doc, ADRs, and code comments so agents working on ragtime can search for architectural decisions and rationale
- **Session continuity**: cross-session RAG means an agent picking up work on ragtime can see what was done in prior sessions
- **Custom rules**: project-specific `.ragtime/rules/` scripts that inject relevant context when the agent touches specific subsystems (e.g., automatically surface the embedding provider interface docs when editing `rag/providers/`)
- **Skill access**: agents working on ragtime can `rt search ragtime-docs "how does the hook engine work"` to get context without reading every file

This dogfooding loop ensures the skill and hook systems stay practical and that rough edges are caught early.

## Future Directions

### Agent-Assisted Library Management

Ragtime could potentially use agent harnesses (Claude Code, Gemini CLI) to help manage its own knowledge base — for example, having an agent review and organize RAG collections, suggest new indexes, or identify gaps in documentation. The exact mechanism is TBD, but the session/hook infrastructure provides the foundation for bidirectional agent interaction.

## Design Principles

- **Keep the codebase clean and modular** — agent hook protocols evolve frequently. The universal hook abstraction insulates scripts from breaking changes, and the per-agent adapter layer should be thin and easy to update. Favor clear interfaces and small packages over clever abstractions.
- **Single binary, zero required dependencies** — the built-in embedding model downloads on first use; everything else is self-contained.
- **Local-first** — all core functionality works without network access (given a local embedding provider or the built-in model).

## Resolved Decisions

| Question | Decision |
|----------|----------|
| Multi-project support | Both global (`~/.ragtime/`) and per-project (`.ragtime/` at git root). Per-project overrides global. |
| Web authentication | Zellij-style session code on first access. |
| Hot reload | Yes, via fsnotify. Automatic, no restart needed. |
| Built-in embedding model | nomic-embed-text-v1.5 (137M params, GGUF Q4, ~50-100 MB). Downloaded on first use, cached in `~/.ragtime/models/`. |
| Scripting language | Starlark (go.starlark.net). |
| TUI framework | Bubble Tea + Lip Gloss. |
