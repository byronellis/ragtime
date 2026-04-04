# AgentOS Remaining Phases

Phases 1, 2, and 4 are complete (SQLite, statusline, PTY muxer). This document covers the remaining work.

## Phase 3: FUSE Filesystem

### Dependencies

- `bazil.org/fuse` — works with Linux native FUSE and macOS FUSE-T (no kernel extension needed on macOS 12+)
- Requires CGO. Build tag `//go:build !nofuse` on all FUSE files; stub file `//go:build nofuse` prints error for `rt mount`.
- macOS: FUSE-T must be installed (`brew install --cask fuse-t`). Detect at mount time and print helpful error if absent.

### Commands

```
rt mount [--path PATH]     # mount at ~/.ragtime/fs by default (global)
rt umount [--path PATH]
```

Mount is foreground/blocking. The daemon tracks mount state. `rt status` shows whether filesystem is mounted.

### Filesystem Layout

```
~/.ragtime/fs/
  sessions/
    claude-<session-id>/
      info.json          # SessionInfo snapshot
      history.txt        # human-readable event log
      events.json        # raw events array
  collections/
    <name>/
      meta.json          # CollectionMeta
      search             # virtual: write a query, read results
      chunks.txt         # all indexed content concatenated
  agents/                # populated by PTY muxer shells
    <shell-id>/
      info.json          # ShellInfo
      output.log         # live-streamed PTY output (read-only)
      input              # write-only: writes inject into PTY stdin
      notes/             # R/W directory; writes get RAG-indexed
  shells -> agents/      # alias
```

### Virtual File Semantics

**`search` file**: Writing a query string to it triggers `rag.Search()` and caches results. Reading it returns the last search results as JSON. Agents can do:
```bash
echo "auth middleware design" > ~/.ragtime/fs/collections/project-docs/search
cat ~/.ragtime/fs/collections/project-docs/search
```

**`input` file** (agents/): write-only, each write is delivered to the PTY master as keyboard input. Enables cross-agent nudging:
```bash
echo "please summarize progress" > ~/.ragtime/fs/agents/<id>/input
```

**`notes/` directory**: regular R/W files. A background inotify/FSEvents watcher re-indexes changed files into the shell's RAG collection (keyed by shell ID).

### Implementation Structure

```
internal/fs/
  fs.go              # RagtimeFS struct, NewRagtimeFS(), Mount(), Unmount()
  nodes.go           # Dir, File, VirtualFile node types
  sessions.go        # sessions/ subtree — reads from session.Manager via daemon socket
  collections.go     # collections/ subtree — reads from rag.Engine
  agents.go          # agents/ subtree — reads from mux.ShellManager, writes to PTY
  fuse_stub.go       # //go:build nofuse — stub returning ErrNotSupported
internal/cli/mount.go  # rt mount / rt umount commands
```

The FS process connects to the daemon socket (or runs embedded in the daemon) to serve live data. Since mount is a foreground process, the simplest model is:
- `rt mount` connects to the daemon
- Uses a long-lived streaming connection (like TUI subscribe) to receive session/shell updates
- Serves the FUSE filesystem from that process

### Cross-Agent Visibility (Deferred Detail)

All shells are visible to all viewers in `agents/`. The operator can see everything; agents only see their own `$RAGTIME_SHELL_ID` by default unless explicitly given another shell's ID. We'll add a `RAGTIME_VISIBLE_SHELLS` env var or config option to control this.

---

## Phase 5: Web Terminal

Deferred for a larger web UI overhaul. When we do it:

- Add xterm.js (CDN initially, embed later)
- New "Agents" tab alongside Sessions/Events tabs
- WebSocket endpoint: `GET /api/shells/{id}/ws` — bidirectional PTY I/O
  - Binary frames: raw PTY bytes server→client (output), client→server (input)
  - JSON control frame: `{"type":"resize","cols":220,"rows":50}`
- Multi-viewer: multiple WebSocket connections receive the same output stream
- Agent list panel: ID, name, state, command, uptime, cost (joined from statusline_events)
- Cost/token dashboard: chart of `cost_usd` over time from statusline_events, by model and by session
- Shell detail view: terminal + metadata side panel

The statusline data already flowing into SQLite makes the cost dashboard straightforward — just query `statusline_events` grouped by time bucket.

---

## Phase 6: Hook Integration Improvements

With shells running inside the ragtime PTY muxer (`RAGTIME_SHELL_ID` set), these improvements follow naturally:

### `inject_input` for ragtime type

Already implemented in Phase 4 — the Starlark `inject_input` builtin detects `RAGTIME_SHELL_ID` and routes through the daemon's `ShellManager.WriteToShell()` directly, no `tmux send-keys` needed.

### Hook event correlation

All hook events from agents running inside `rt sh` shells carry `MuxInfo{Type: "ragtime", Pane: shellID}`. This lets Starlark rules and the web UI correlate hook events ↔ PTY output ↔ statusline telemetry by shell ID.

Add `ShellID` to `SessionInfo` so the sessions panel can link directly to the terminal view.

### Auto-start hooks for agents launched via `rt sh`

When launching `claude` or `gemini` via `rt sh new`, automatically inject the ragtime hook env vars if not already configured:
```go
// In mux/manager.go New():
if isAgentCommand(spec.Command) {
    spec.Env = append(spec.Env, hookEnvVars()...)
}
```

`hookEnvVars()` returns whatever is needed for the agent to pick up the ragtime hooks automatically (e.g., writing a temp settings file or setting `CLAUDE_CONFIG_DIR`).

---

## Phase 7: FUSE Agent Nodes (depends on Phase 3 + 4)

Extend the `agents/` subtree in the FUSE filesystem:

- `output.log` streams live PTY output via a synthetic read that blocks and delivers new data (like `tail -f`)
- `input` write triggers `shell.Write()` directly
- `notes/<filename>` R/W — on write/close, reindex into a `shell-<id>-notes` RAG collection
- `status` read returns `ShellInfo` as JSON

This is Phase 3 extended with shell awareness. The Phase 3 `agents.go` file has stubs; this fills them in after Phase 4 is stable.

---

## Phase 8: Starlark Nudging Patterns + Example Rules

Document and ship example rules in `examples/rules/`:

```yaml
# examples/rules/cost-alert.yaml
name: cost-alert
match:
  event: statusline
actions:
  - type: starlark
    script: |
      if float(event.raw.get("cost_usd", 0)) > 0.50:
          response.prompt(
              "Session cost is $%.2f — continue?" % float(event.raw.get("cost_usd", 0)),
              type="ok_cancel",
              default="ok",
              timeout=30,
          )
```

```yaml
# examples/rules/stall-nudge.yaml
name: stall-nudge
# Fires on stop events — if the agent hasn't made progress, nudge it
match:
  event: stop
actions:
  - type: starlark
    script: |
      # Check if this session has had many stop events with no tool calls
      # (placeholder — implement stall detection logic)
      shell_id = event.raw.get("ragtime_shell_id", "")
      if shell_id and tui.connected():
          resp = response.prompt(
              "Agent appears stalled. Nudge it?",
              type="ok_cancel",
              default="cancel",
              timeout=15,
          )
          if resp == "ok":
              shell.send(shell_id, "Please continue with the task.", enter=True)
```

```yaml
# examples/rules/cross-agent-context.yaml
name: cross-agent-context
# When agent A uses the Bash tool, inject context from agent B's recent session
match:
  event: pre-tool-use
  tool: Bash
actions:
  - type: starlark
    script: |
      results = rag.search("sessions", event.tool_input.get("command", ""), top_k=3)
      if results:
          ctx = "\n".join(r["content"] for r in results)
          response.inject_context("## Related session context\n" + ctx)
```

---

## Implementation Order Reminder

```
Phase 3: FUSE filesystem (rt mount/umount, read-only first)
Phase 5: Web terminal + dashboard (larger UI overhaul)
Phase 6: Hook correlation + auto-start hooks
Phase 7: FUSE agent nodes (R/W, live output.log)
Phase 8: Example rules + docs
```

Phases 3 and 5 are parallel-workable. Phase 7 requires both 3 and 4. Phase 6 is incremental polish on Phase 4.
