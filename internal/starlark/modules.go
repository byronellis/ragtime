package starlark

import (
	"fmt"
	"strings"

	"github.com/byronellis/ragtime/internal/protocol"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// buildPredeclared creates the namespace of globals available to Starlark scripts.
func (r *Runner) buildPredeclared(event *protocol.HookEvent, resp *responseHelper) starlark.StringDict {
	return starlark.StringDict{
		"event":    eventToStarlark(event),
		"response": resp,
		"rag":      r.ragModule(),
		"tui":      r.tuiModule(),
		"log":      starlark.NewBuiltin("log", r.logBuiltin),
	}
}

// --- event struct ---

func eventToStarlark(e *protocol.HookEvent) *starlarkstruct.Struct {
	toolInput := starlark.NewDict(len(e.ToolInput))
	for k, v := range e.ToolInput {
		toolInput.SetKey(starlark.String(k), goToStarlark(v))
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

// --- response helper ---

type responseHelper struct {
	context    []string
	decision   protocol.PermissionDecision
	denyReason string
}

func newResponseHelper() *responseHelper {
	return &responseHelper{}
}

func (rh *responseHelper) toResponse() *protocol.HookResponse {
	resp := &protocol.HookResponse{
		PermissionDecision: rh.decision,
		DenyReason:         rh.denyReason,
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
	return []string{"inject_context", "approve", "deny", "ask"}
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

func (rh *responseHelper) ask(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	rh.decision = protocol.PermAsk
	return starlark.None, nil
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

// --- log builtin ---

func (r *Runner) logBuiltin(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	parts := make([]string, len(args))
	for i, arg := range args {
		parts[i] = arg.String()
	}
	thread.Print(thread, strings.Join(parts, " "))
	return starlark.None, nil
}

