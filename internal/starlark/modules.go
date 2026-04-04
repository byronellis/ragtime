package starlark

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/byronellis/ragtime/internal/protocol"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// buildPredeclared creates the namespace of globals available to Starlark scripts.
func (r *Runner) buildPredeclared(event *protocol.HookEvent, resp *responseHelper) starlark.StringDict {
	return starlark.StringDict{
		"event":        eventToStarlark(event),
		"response":     resp,
		"rag":          r.ragModule(),
		"tui":          r.tuiModule(),
		"shell":        r.shellModule(),
		"log":          starlark.NewBuiltin("log", r.logBuiltin),
		"inject_input": starlark.NewBuiltin("inject_input", resp.injectInputBuiltin),
	}
}

// --- event struct ---

func eventToStarlark(e *protocol.HookEvent) *starlarkstruct.Struct {
	toolInput := starlark.NewDict(len(e.ToolInput))
	for k, v := range e.ToolInput {
		toolInput.SetKey(starlark.String(k), goToStarlark(v))
	}

	raw := starlark.NewDict(len(e.Raw))
	for k, v := range e.Raw {
		raw.SetKey(starlark.String(k), goToStarlark(v))
	}

	return starlarkstruct.FromStringDict(starlark.String("event"), starlark.StringDict{
		"agent":         starlark.String(e.Agent),
		"event_type":    starlark.String(e.EventType),
		"session_id":    starlark.String(e.SessionID),
		"tool_name":     starlark.String(e.ToolName),
		"tool_input":    toolInput,
		"tool_response": starlark.String(e.ToolResponse),
		"prompt":        starlark.String(e.Prompt),
		"response":      starlark.String(e.Response),
		"cwd":           starlark.String(e.CWD),
		"raw":           raw,
	})
}

func goToStarlark(v any) starlark.Value {
	switch val := v.(type) {
	case string:
		return starlark.String(val)
	case float64:
		return starlark.Float(val)
	case bool:
		return starlark.Bool(val)
	case nil:
		return starlark.None
	case map[string]any:
		d := starlark.NewDict(len(val))
		for k, v := range val {
			d.SetKey(starlark.String(k), goToStarlark(v))
		}
		return d
	case []any:
		items := make([]starlark.Value, len(val))
		for i, v := range val {
			items[i] = goToStarlark(v)
		}
		return starlark.NewList(items)
	default:
		return starlark.String(fmt.Sprintf("%v", val))
	}
}

func starlarkToGo(v starlark.Value) any {
	switch val := v.(type) {
	case starlark.String:
		return string(val)
	case starlark.Int:
		i, _ := val.Int64()
		return i
	case starlark.Float:
		return float64(val)
	case starlark.Bool:
		return bool(val)
	case *starlark.List:
		items := make([]any, val.Len())
		for i := 0; i < val.Len(); i++ {
			items[i] = starlarkToGo(val.Index(i))
		}
		return items
	case *starlark.Dict:
		m := make(map[string]any)
		for _, item := range val.Items() {
			if key, ok := item[0].(starlark.String); ok {
				m[string(key)] = starlarkToGo(item[1])
			}
		}
		return m
	case starlark.NoneType:
		return nil
	default:
		return val.String()
	}
}

// --- response helper ---

type responseHelper struct {
	context         []string
	decision        protocol.PermissionDecision
	denyReason      string
	outputOverrides map[string]any
	interactor      Interactor
	shellWriter     ShellWriter
	event           *protocol.HookEvent
}

func newResponseHelper(interactor Interactor, shellWriter ShellWriter, event *protocol.HookEvent) *responseHelper {
	return &responseHelper{
		interactor:  interactor,
		shellWriter: shellWriter,
		event:       event,
	}
}

func (rh *responseHelper) toResponse() *protocol.HookResponse {
	resp := &protocol.HookResponse{
		PermissionDecision: rh.decision,
		DenyReason:         rh.denyReason,
		OutputOverrides:    rh.outputOverrides,
	}
	if len(rh.context) > 0 {
		resp.Context = strings.Join(rh.context, "\n\n---\n\n")
	}
	return resp
}

// Starlark interface implementation

func (rh *responseHelper) String() string        { return "response" }
func (rh *responseHelper) Type() string           { return "response" }
func (rh *responseHelper) Freeze()                {}
func (rh *responseHelper) Truth() starlark.Bool   { return true }
func (rh *responseHelper) Hash() (uint32, error)  { return 0, fmt.Errorf("unhashable") }

func (rh *responseHelper) AttrNames() []string {
	return []string{"inject_context", "approve", "deny", "ask", "prompt", "set_output", "agent"}
}

func (rh *responseHelper) Attr(name string) (starlark.Value, error) {
	switch name {
	case "inject_context":
		return starlark.NewBuiltin("response.inject_context", rh.injectContext), nil
	case "approve":
		return starlark.NewBuiltin("response.approve", rh.approve), nil
	case "deny":
		return starlark.NewBuiltin("response.deny", rh.deny), nil
	case "ask":
		return starlark.NewBuiltin("response.ask", rh.ask), nil
	case "prompt":
		return starlark.NewBuiltin("response.prompt", rh.promptBuiltin), nil
	case "set_output":
		return starlark.NewBuiltin("response.set_output", rh.setOutput), nil
	case "agent":
		if rh.event != nil {
			return starlark.String(rh.event.Agent), nil
		}
		return starlark.String(""), nil
	default:
		return nil, nil
	}
}

func (rh *responseHelper) injectContext(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var text string
	if err := starlark.UnpackPositionalArgs("inject_context", args, kwargs, 1, &text); err != nil {
		return nil, err
	}
	rh.context = append(rh.context, text)
	return starlark.None, nil
}

func (rh *responseHelper) approve(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	rh.decision = protocol.PermAllow
	return starlark.None, nil
}

func (rh *responseHelper) deny(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var reason string
	if err := starlark.UnpackPositionalArgs("deny", args, kwargs, 0, &reason); err != nil {
		return nil, err
	}
	rh.decision = protocol.PermDeny
	rh.denyReason = reason
	return starlark.None, nil
}

func (rh *responseHelper) setOutput(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var key string
	var value starlark.Value
	if err := starlark.UnpackPositionalArgs("set_output", args, kwargs, 2, &key, &value); err != nil {
		return nil, err
	}
	if rh.outputOverrides == nil {
		rh.outputOverrides = make(map[string]any)
	}
	rh.outputOverrides[key] = starlarkToGo(value)
	return starlark.None, nil
}

func (rh *responseHelper) ask(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	rh.decision = protocol.PermAsk
	return starlark.None, nil
}

// promptBuiltin sends an interaction request to the TUI and blocks until response.
func (rh *responseHelper) promptBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var text string
	interType := "ok_cancel"
	defaultVal := "cancel"
	timeout := 30

	if err := starlark.UnpackArgs("response.prompt", args, kwargs,
		"text", &text,
		"type?", &interType,
		"default?", &defaultVal,
		"timeout?", &timeout,
	); err != nil {
		return nil, err
	}

	if rh.interactor == nil {
		return starlark.String(defaultVal), nil
	}

	resp := rh.interactor.Prompt(text, protocol.InteractionType(interType), defaultVal, timeout)
	return starlark.String(resp.Value), nil
}

// --- inject_input ---

// injectInputBuiltin sends key sequences to a terminal multiplexer pane.
// Usage: inject_input([{"keys": "y", "delay_ms": 100}, {"keys": "Enter"}])
func (rh *responseHelper) injectInputBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("inject_input: expected 1 argument (list of sequences), got %d", len(args))
	}

	seqList, ok := args[0].(*starlark.List)
	if !ok {
		return nil, fmt.Errorf("inject_input: expected list, got %s", args[0].Type())
	}

	mux := detectMux(rh.event)
	if mux == nil {
		return nil, fmt.Errorf("inject_input: no terminal multiplexer detected")
	}

	iter := seqList.Iterate()
	defer iter.Done()
	var val starlark.Value
	for iter.Next(&val) {
		dict, ok := val.(*starlark.Dict)
		if !ok {
			return nil, fmt.Errorf("inject_input: sequence items must be dicts, got %s", val.Type())
		}

		keysVal, found, err := dict.Get(starlark.String("keys"))
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		keys, ok := starlark.AsString(keysVal)
		if !ok {
			return nil, fmt.Errorf("inject_input: 'keys' must be a string")
		}

		if err := sendKeys(mux, keys, rh.shellWriter); err != nil {
			return nil, fmt.Errorf("inject_input: %w", err)
		}

		// Check for delay
		delayVal, found, err := dict.Get(starlark.String("delay_ms"))
		if err != nil {
			return nil, err
		}
		if found {
			if delayInt, err := starlark.AsInt32(delayVal); err == nil && delayInt > 0 {
				time.Sleep(time.Duration(delayInt) * time.Millisecond)
			}
		}
	}

	return starlark.None, nil
}

// sendKeysRagtime sends keys to a ragtime shell via the shell manager.
func (r *Runner) sendKeysRagtime(shellID string, keys string) error {
	if r.shellMgr == nil {
		return fmt.Errorf("shell manager not available")
	}
	return r.shellMgr.WriteToShell(shellID, []byte(keys))
}

// detectMux detects the terminal multiplexer from the event's MuxInfo or environment.
func detectMux(event *protocol.HookEvent) *protocol.MuxInfo {
	if event != nil && event.Mux != nil && event.Mux.Type != "" {
		return event.Mux
	}

	// Fall back to environment variables
	if tmux := os.Getenv("TMUX"); tmux != "" {
		pane := os.Getenv("TMUX_PANE")
		return &protocol.MuxInfo{Type: "tmux", Pane: pane}
	}
	if sty := os.Getenv("STY"); sty != "" {
		return &protocol.MuxInfo{Type: "screen", SessionName: sty}
	}
	if shellID := os.Getenv("RAGTIME_SHELL_ID"); shellID != "" {
		return &protocol.MuxInfo{Type: "ragtime", Pane: shellID}
	}
	return nil
}

// sendKeys sends a key sequence to the multiplexer pane.
func sendKeys(mux *protocol.MuxInfo, keys string, shellWriter ShellWriter) error {
	switch mux.Type {
	case "tmux":
		args := []string{"send-keys"}
		if mux.Pane != "" {
			args = append(args, "-t", mux.Pane)
		}
		args = append(args, keys)
		return exec.Command("tmux", args...).Run()

	case "screen":
		args := []string{"-X", "stuff", keys}
		if mux.SessionName != "" {
			args = append([]string{"-S", mux.SessionName}, args...)
		}
		return exec.Command("screen", args...).Run()

	case "ragtime":
		if shellWriter == nil {
			return fmt.Errorf("ragtime shell writer not available")
		}
		return shellWriter.WriteToShell(mux.Pane, []byte(keys))

	default:
		return fmt.Errorf("unsupported multiplexer type: %s", mux.Type)
	}
}

// --- rag module ---

func (r *Runner) ragModule() *starlarkstruct.Module {
	return &starlarkstruct.Module{
		Name: "rag",
		Members: starlark.StringDict{
			"search": starlark.NewBuiltin("rag.search", r.ragSearch),
		},
	}
}

func (r *Runner) ragSearch(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var collection, query string
	topK := 5
	if err := starlark.UnpackArgs("rag.search", args, kwargs,
		"collection", &collection,
		"query", &query,
		"top_k?", &topK,
	); err != nil {
		return nil, err
	}

	if r.rag == nil {
		return starlark.NewList(nil), nil
	}

	results, err := r.rag.Search(collection, query, topK)
	if err != nil {
		return starlark.NewList(nil), nil // swallow errors, return empty
	}

	items := make([]starlark.Value, len(results))
	for i, res := range results {
		d := starlark.NewDict(3)
		d.SetKey(starlark.String("content"), starlark.String(res.Content))
		d.SetKey(starlark.String("source"), starlark.String(res.Source))
		d.SetKey(starlark.String("score"), starlark.Float(float64(res.Score)))
		items[i] = d
	}
	return starlark.NewList(items), nil
}

// --- tui module ---

func (r *Runner) tuiModule() *starlarkstruct.Module {
	return &starlarkstruct.Module{
		Name: "tui",
		Members: starlark.StringDict{
			"connected": starlark.NewBuiltin("tui.connected", r.tuiConnected),
		},
	}
}

func (r *Runner) tuiConnected(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if r.tui == nil {
		return starlark.False, nil
	}
	return starlark.Bool(r.tui.ClientCount() > 0), nil
}

// --- shell module ---

func (r *Runner) shellModule() *starlarkstruct.Module {
	return &starlarkstruct.Module{
		Name: "shell",
		Members: starlark.StringDict{
			"send": starlark.NewBuiltin("shell.send", r.shellSend),
			"list": starlark.NewBuiltin("shell.list", r.shellList),
		},
	}
}

func (r *Runner) shellSend(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var id, text string
	if err := starlark.UnpackPositionalArgs("shell.send", args, kwargs, 2, &id, &text); err != nil {
		return nil, err
	}
	if r.shellMgr == nil {
		return starlark.None, fmt.Errorf("shell manager not available")
	}
	if err := r.shellMgr.WriteToShell(id, []byte(text)); err != nil {
		return starlark.None, err
	}
	return starlark.None, nil
}

func (r *Runner) shellList(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	if r.shellMgr == nil {
		return starlark.NewList(nil), nil
	}
	ids := r.shellMgr.ListShells()
	items := make([]starlark.Value, len(ids))
	for i, id := range ids {
		items[i] = starlark.String(id)
	}
	return starlark.NewList(items), nil
}

// --- log builtin ---

func (r *Runner) logBuiltin(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	parts := make([]string, len(args))
	for i, arg := range args {
		parts[i] = arg.String()
	}
	thread.Print(thread, strings.Join(parts, " "))
	return starlark.None, nil
}
