# Starlark Rule API

Ragtime supports Starlark scripts as rule actions for dynamic hook logic. Scripts run in a sandboxed environment with access to event data, RAG search, TUI state, and response helpers.

## Quick Start

Add a rule with `type: starlark` to your ragtime config or rules directory:

```yaml
name: my-rule
match:
  event: pre-tool-use
  tool: Bash
actions:
  - type: starlark
    script: |
      cmd = event.tool_input.get("command", "")
      if "rm -rf" in cmd:
          response.deny("Dangerous command blocked")
```

Scripts can also be loaded from `.star` files:

```yaml
actions:
  - type: starlark
    script: file:///path/to/my-rule.star
```

## API Reference

### `event` (read-only)

The hook event that triggered this rule.

| Field | Type | Description |
|-------|------|-------------|
| `event.agent` | `string` | Agent name (`"claude"`, `"gemini"`) |
| `event.event_type` | `string` | Event type (`"pre-tool-use"`, `"post-tool-use"`, `"stop"`, etc.) |
| `event.session_id` | `string` | Session identifier |
| `event.tool_name` | `string` | Tool being used (`"Read"`, `"Write"`, `"Bash"`, etc.) |
| `event.tool_input` | `dict` | Tool parameters (e.g., `{"file_path": "/foo/bar.go"}`) |
| `event.tool_response` | `string` | Tool output (post-tool-use only) |
| `event.prompt` | `string` | User prompt (user-prompt-submit only) |
| `event.response` | `string` | Agent response (stop events only) |
| `event.cwd` | `string` | Working directory |

### `response`

Mutable helper for building the hook response. Multiple calls accumulate (context is joined with `---` separators). Permission decisions use last-write-wins within a single script.

| Method | Description |
|--------|-------------|
| `response.inject_context(text)` | Add context that will be shown to the agent |
| `response.approve()` | Auto-approve the tool call (no human confirmation) |
| `response.deny(reason="")` | Block the tool call with an optional reason |
| `response.ask()` | Defer to the human (TUI prompt or agent default) |

### `rag`

Search indexed RAG collections.

```python
results = rag.search("collection-name", "search query", top_k=5)
```

Returns a list of dicts:
```python
[{"content": "...", "source": "docs/auth.md", "score": 0.85}, ...]
```

Returns an empty list if no RAG engine is configured or the collection doesn't exist.

### `tui`

Check TUI connectivity.

```python
if tui.connected():
    response.ask()    # someone is watching, let them decide
else:
    response.approve()  # unattended, auto-approve
```

Returns `False` if no TUI clients are connected or if the TUI subsystem is not available.

### `log`

Print to the daemon log (slog INFO level).

```python
log("processing", event.tool_name, "for", event.agent)
```

## Sandbox Limits

- **CPU**: 1,000,000 execution steps per script invocation
- **No imports**: The `load()` function is disabled
- **No filesystem**: Scripts cannot read/write files directly
- **No network**: The only external call is `rag.search()` through the controlled interface
- **Isolated state**: Each invocation gets a fresh thread — no global mutable state persists between executions

## YAML Action Types

Starlark complements the existing declarative action types:

| Type | Description |
|------|-------------|
| `inject-context` | Static context injection (`content: "..."`) |
| `approve` | Auto-approve the tool call |
| `deny` | Block with `reason: "..."` |
| `rag-search` | Search RAG collections (`collections: [...]`, `query_from: "..."`) |
| `starlark` | Dynamic logic via `script: \|` or `script: file://path.star` |
| `log` | Log the event to daemon output |

## Match Config

Rules fire when their `match` config matches the incoming event:

```yaml
match:
  agent: claude          # optional, supports glob and pipe alternatives
  event: pre-tool-use    # optional, event type filter
  tool: "Read|Write"     # optional, pipe-separated alternatives
  path_glob: "*.go"      # optional, matches against file_path in tool_input
```

All fields are optional. Omitted fields match everything.
