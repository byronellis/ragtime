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

#### Permission Control

| Method | Description |
|--------|-------------|
| `response.approve()` | Auto-approve the tool call (no human confirmation) |
| `response.deny(reason="")` | Block the tool call with an optional reason |
| `response.ask()` | Defer to the human (TUI prompt or agent default) |

#### Context Injection

| Method | Description |
|--------|-------------|
| `response.inject_context(text)` | Add context that will be shown to the agent |

#### Agent Info

| Attribute | Type | Description |
|-----------|------|-------------|
| `response.agent` | `string` | The agent platform handling this event (`"claude"`, `"gemini"`, etc.). Use this to branch on agent-specific behavior. |

```python
if response.agent == "claude":
    response.inject_context("Claude-specific guidance here.")
```

#### Interactive Prompts

| Method | Description |
|--------|-------------|
| `response.prompt(text, type="ok_cancel", default="cancel", timeout=30)` | Show an interactive prompt in the TUI and block until the user responds or timeout expires. |

**Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `text` | `string` | required | Prompt text (markdown supported) |
| `type` | `string` | `"ok_cancel"` | One of `"ok_cancel"`, `"approve_deny_cancel"`, or `"freeform"` |
| `default` | `string` | `"cancel"` | Value returned on timeout |
| `timeout` | `int` | `30` | Seconds before auto-responding with default |

**Returns:** A string with the user's choice (`"ok"`, `"cancel"`, `"approve"`, `"deny"`, or freeform text).

The timer starts when the prompt is displayed in the TUI, not when it's enqueued. If no TUI is connected, the prompt is queued until one connects (or times out with the default).

```python
answer = response.prompt(
    text="**Dangerous command detected:**\n\n`" + event.tool_input["command"] + "`\n\nAllow execution?",
    type="approve_deny_cancel",
    default="deny",
    timeout=15,
)
if answer == "approve":
    response.approve()
elif answer == "deny":
    response.deny("User denied the command")
```

#### Raw Output Control

| Method | Description |
|--------|-------------|
| `response.set_output(key, value)` | Set an arbitrary key-value pair in the agent output JSON. |

Values can be strings, numbers, bools, lists, or dicts. These are merged into the top-level agent output alongside the standard fields. Use this for:

- Agent-specific output fields the standard API doesn't cover
- Debugging hooks by adding metadata
- Overriding the standard formatted output when full control is needed

```python
# Add custom metadata alongside standard output
response.inject_context("Be careful with this command.")
response.set_output("debugInfo", {
    "rule": "my-rule",
    "agent": response.agent,
})

# Override agent-specific output entirely (advanced)
if response.agent == "claude":
    response.set_output("hookSpecificOutput", {
        "hookEventName": "PreToolUse",
        "additionalContext": "Custom context",
        "permissionDecision": "allow",
    })
```

### `inject_input`

Send key sequences to the terminal multiplexer (tmux or screen). Useful for automating responses to interactive prompts from tools.

```python
inject_input([
    {"keys": "y", "delay_ms": 100},
    {"keys": "Enter"},
])
```

Each item in the list is a dict with:

| Key | Type | Description |
|-----|------|-------------|
| `keys` | `string` | Key sequence to send |
| `delay_ms` | `int` | Optional delay in milliseconds after sending |

Requires a detected terminal multiplexer (tmux via `$TMUX`/`$TMUX_PANE`, or screen via `$STY`). Returns an error if no multiplexer is found.

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

## Testing Rules

The `rt hook --test` command runs the hook engine locally without a daemon, making it easy to develop and debug rules.

### Basic Usage

```bash
# Synthetic event from flags (defaults to agent=claude, event=pre-tool-use)
rt hook --test --tool Bash --input '{"command":"rm -rf /"}'

# Pipe agent-format JSON (same format Claude Code sends on stdin)
echo '{"tool_name":"Read","tool_input":{"file_path":"/etc/passwd"}}' | rt hook --test

# Override agent and event type
rt hook --test --agent gemini --event post-tool-use --tool Bash
```

### Testing Specific Rules

Use `--rule` to test specific rule files instead of loading from config directories. The flag is repeatable — use it multiple times to test how rules interact:

```bash
# Test a single rule
rt hook --test --tool Bash --input '{"command":"ls"}' --rule my-rule.yaml

# Test interaction between multiple rules
rt hook --test --tool Bash --input '{"command":"rm -rf /"}' \
  --rule deny-dangerous.yaml --rule inject-warning.yaml
```

Without `--rule`, rules are loaded from the standard locations (`~/.ragtime/rules/`, `.ragtime/rules/`, and config file).

### Verbose Output

`--verbose` shows per-rule match/skip details:

```bash
rt hook --test --tool Bash --input '{"command":"ls"}' --rule my-rule.yaml --verbose
```

```
=== Hook Test ===
Event:  pre-tool-use / Bash (ls)
Agent:  claude
Rules:  2 loaded
Time:   340µs

Matched rules: inject-warning
Permission:    (none)

--- Injected Context ---
Remember: always review bash commands carefully.

--- Agent Output (claude) ---
{
  "hookSpecificOutput": {
    "additionalContext": "Remember: always review bash commands carefully.",
    "hookEventName": "PreToolUse"
  }
}

--- Rule Details ---
  [SKIP] deny-dangerous  event=pre-tool-use  tool=Bash
  [MATCH] inject-warning  tool=Bash
```

### Interactive TUI Testing

Use `--tui` to launch actual TUI modals when a rule calls `response.prompt()`. This shows the exact modal that would appear in the ragtime dashboard:

```bash
rt hook --test --tui --tool Bash --input '{"command":"rm -rf /"}' --rule confirm-bash.yaml
```

The modal supports:
- Tab/arrow keys to switch between buttons
- Enter to confirm selection
- Escape to cancel
- Countdown timer with auto-response on timeout
- Freeform text input (for `type="freeform"` prompts)

Without `--tui`, prompts fall back to plain text on stderr (or return the default value if stdin is not a terminal).

### Test Mode Flags

| Flag | Description |
|------|-------------|
| `--test` | Enable test mode (run locally, no daemon) |
| `--tool NAME` | Tool name for the synthetic event |
| `--input JSON` | JSON object for tool_input |
| `--agent NAME` | Agent platform (default: `claude`) |
| `--event TYPE` | Event type (default: `pre-tool-use`) |
| `--rule FILE` | Rule YAML file to test (repeatable, replaces config dir loading) |
| `--tui` | Show interactive TUI modals for `response.prompt()` calls |
| `--verbose` | Show per-rule match details and agent output |

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
