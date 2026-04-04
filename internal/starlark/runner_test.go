package starlark

import (
	"testing"

	"os"

	"github.com/byronellis/ragtime/internal/hook"
	"github.com/byronellis/ragtime/internal/protocol"
)

// mockRAG implements hook.RAGSearcher for testing.
type mockRAG struct {
	results []hook.SearchResult
	err     error
}

func (m *mockRAG) Search(collection, query string, topK int) ([]hook.SearchResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.results, nil
}

// mockTUI implements TUIState for testing.
type mockTUI struct {
	clients int
}

func (m *mockTUI) ClientCount() int { return m.clients }

func testRunner() *Runner {
	return NewRunner(nil, nil, nil)
}

func TestExecute_InjectContext(t *testing.T) {
	r := testRunner()
	event := &protocol.HookEvent{
		Agent:     "claude",
		EventType: "pre-tool-use",
		ToolName:  "Read",
		ToolInput: map[string]any{"file_path": "/tmp/test.go"},
	}

	resp, err := r.Execute(`response.inject_context("hello from starlark")`, event)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Context != "hello from starlark" {
		t.Errorf("context = %q, want 'hello from starlark'", resp.Context)
	}
}

func TestExecute_Approve(t *testing.T) {
	r := testRunner()
	event := &protocol.HookEvent{EventType: "pre-tool-use", ToolName: "Read"}

	resp, err := r.Execute(`response.approve()`, event)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.PermissionDecision != protocol.PermAllow {
		t.Errorf("decision = %q, want allow", resp.PermissionDecision)
	}
}

func TestExecute_Deny(t *testing.T) {
	r := testRunner()
	event := &protocol.HookEvent{EventType: "pre-tool-use", ToolName: "Bash"}

	resp, err := r.Execute(`response.deny("not allowed")`, event)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.PermissionDecision != protocol.PermDeny {
		t.Errorf("decision = %q, want deny", resp.PermissionDecision)
	}
	if resp.DenyReason != "not allowed" {
		t.Errorf("reason = %q, want 'not allowed'", resp.DenyReason)
	}
}

func TestExecute_Ask(t *testing.T) {
	r := testRunner()
	event := &protocol.HookEvent{EventType: "pre-tool-use"}

	resp, err := r.Execute(`response.ask()`, event)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.PermissionDecision != protocol.PermAsk {
		t.Errorf("decision = %q, want ask", resp.PermissionDecision)
	}
}

func TestExecute_ReadEventFields(t *testing.T) {
	r := testRunner()
	event := &protocol.HookEvent{
		Agent:     "claude",
		EventType: "pre-tool-use",
		SessionID: "sess-123",
		ToolName:  "Read",
		ToolInput: map[string]any{"file_path": "/foo/bar.go"},
		CWD:       "/home/user/project",
	}

	script := `
if event.agent == "claude" and event.tool_name == "Read":
    path = event.tool_input.get("file_path", "")
    if "bar.go" in path:
        response.inject_context("found bar.go")
`
	resp, err := r.Execute(script, event)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Context != "found bar.go" {
		t.Errorf("context = %q, want 'found bar.go'", resp.Context)
	}
}

func TestExecute_MultipleContextInjections(t *testing.T) {
	r := testRunner()
	event := &protocol.HookEvent{EventType: "pre-tool-use"}

	script := `
response.inject_context("first")
response.inject_context("second")
`
	resp, err := r.Execute(script, event)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Context != "first\n\n---\n\nsecond" {
		t.Errorf("context = %q", resp.Context)
	}
}

func TestExecute_TUIConnected(t *testing.T) {
	r := NewRunner(nil, &mockTUI{clients: 1}, nil)
	event := &protocol.HookEvent{EventType: "pre-tool-use", ToolName: "Bash"}

	script := `
if tui.connected():
    response.ask()
else:
    response.approve()
`
	resp, err := r.Execute(script, event)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.PermissionDecision != protocol.PermAsk {
		t.Errorf("decision = %q, want ask (tui connected)", resp.PermissionDecision)
	}
}

func TestExecute_TUIDisconnected(t *testing.T) {
	r := NewRunner(nil, &mockTUI{clients: 0}, nil)
	event := &protocol.HookEvent{EventType: "pre-tool-use", ToolName: "Bash"}

	script := `
if tui.connected():
    response.ask()
else:
    response.approve()
`
	resp, err := r.Execute(script, event)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.PermissionDecision != protocol.PermAllow {
		t.Errorf("decision = %q, want allow (tui disconnected)", resp.PermissionDecision)
	}
}

func TestExecute_TUINil(t *testing.T) {
	r := NewRunner(nil, nil, nil)
	event := &protocol.HookEvent{EventType: "pre-tool-use"}

	resp, err := r.Execute(`
if tui.connected():
    response.deny("should not happen")
`, event)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.PermissionDecision != "" {
		t.Errorf("decision = %q, want empty (nil tui returns false)", resp.PermissionDecision)
	}
}

func TestExecute_RAGSearch(t *testing.T) {
	r := NewRunner(&mockRAG{
		results: []hook.SearchResult{
			{Content: "auth docs here", Source: "docs/auth.md", Score: 0.9},
		},
	}, nil, nil)
	event := &protocol.HookEvent{EventType: "pre-tool-use", ToolName: "Read"}

	script := `
results = rag.search("docs", "authentication")
for r in results:
    if r["score"] > 0.5:
        response.inject_context(r["content"])
`
	resp, err := r.Execute(script, event)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Context != "auth docs here" {
		t.Errorf("context = %q, want 'auth docs here'", resp.Context)
	}
}

func TestExecute_RAGNil(t *testing.T) {
	r := NewRunner(nil, nil, nil)
	event := &protocol.HookEvent{EventType: "pre-tool-use"}

	// Should not error, just return empty results
	resp, err := r.Execute(`
results = rag.search("docs", "anything")
if len(results) > 0:
    response.inject_context("found something")
`, event)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Context != "" {
		t.Errorf("context = %q, want empty (nil rag)", resp.Context)
	}
}

func TestExecute_CompileError(t *testing.T) {
	r := testRunner()
	event := &protocol.HookEvent{EventType: "pre-tool-use"}

	_, err := r.Execute(`this is not valid python`, event)
	if err == nil {
		t.Fatal("expected compile error")
	}
}

func TestExecute_RuntimeError(t *testing.T) {
	r := testRunner()
	event := &protocol.HookEvent{EventType: "pre-tool-use"}

	_, err := r.Execute(`x = 1 / 0`, event)
	if err == nil {
		t.Fatal("expected runtime error")
	}
}

func TestExecute_MaxSteps(t *testing.T) {
	r := testRunner()
	event := &protocol.HookEvent{EventType: "pre-tool-use"}

	_, err := r.Execute(`
x = 0
for i in range(10000000):
    x += 1
`, event)
	if err == nil {
		t.Fatal("expected max steps error")
	}
}

func TestExecute_NoOp(t *testing.T) {
	r := testRunner()
	event := &protocol.HookEvent{EventType: "pre-tool-use"}

	resp, err := r.Execute(`x = 1 + 1`, event)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Context != "" || resp.PermissionDecision != "" {
		t.Error("no-op script should produce empty response")
	}
}

func TestExecute_CachesPrograms(t *testing.T) {
	r := testRunner()
	event := &protocol.HookEvent{EventType: "pre-tool-use"}
	script := `response.inject_context("cached")`

	// Execute twice with same script
	r.Execute(script, event)
	r.Execute(script, event)

	r.mu.RLock()
	cacheSize := len(r.cache)
	r.mu.RUnlock()

	if cacheSize != 1 {
		t.Errorf("cache size = %d, want 1 (same script compiled once)", cacheSize)
	}
}

func TestClearCache(t *testing.T) {
	r := testRunner()
	event := &protocol.HookEvent{EventType: "pre-tool-use"}

	r.Execute(`x = 1`, event)

	r.mu.RLock()
	before := len(r.cache)
	r.mu.RUnlock()

	r.ClearCache()

	r.mu.RLock()
	after := len(r.cache)
	r.mu.RUnlock()

	if before == 0 {
		t.Error("cache should have entries before clear")
	}
	if after != 0 {
		t.Error("cache should be empty after clear")
	}
}

func TestExecute_Log(t *testing.T) {
	r := testRunner()
	event := &protocol.HookEvent{EventType: "pre-tool-use", ToolName: "Read"}

	// Log should not error
	resp, err := r.Execute(`log("processing", event.tool_name)`, event)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.PermissionDecision != "" {
		t.Error("log-only script should not set decision")
	}
}

func TestExecute_ConditionalLogic(t *testing.T) {
	r := NewRunner(nil, &mockTUI{clients: 2}, nil)

	// Test file safe auto-approve
	event := &protocol.HookEvent{
		EventType: "pre-tool-use",
		ToolName:  "Write",
		ToolInput: map[string]any{"file_path": "/project/internal/foo_test.go"},
	}

	script := `
path = event.tool_input.get("file_path", "")
if path.endswith("_test.go"):
    response.approve()
elif tui.connected():
    response.ask()
else:
    response.deny("no TUI connected for write")
`
	resp, err := r.Execute(script, event)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.PermissionDecision != protocol.PermAllow {
		t.Errorf("test file should be auto-approved, got %q", resp.PermissionDecision)
	}

	// Non-test file with TUI connected
	event.ToolInput["file_path"] = "/project/internal/foo.go"
	resp, err = r.Execute(script, event)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.PermissionDecision != protocol.PermAsk {
		t.Errorf("non-test file with TUI should ask, got %q", resp.PermissionDecision)
	}
}

func TestExecute_DenyNoReason(t *testing.T) {
	r := testRunner()
	event := &protocol.HookEvent{EventType: "pre-tool-use"}

	resp, err := r.Execute(`response.deny()`, event)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.PermissionDecision != protocol.PermDeny {
		t.Errorf("decision = %q, want deny", resp.PermissionDecision)
	}
	if resp.DenyReason != "" {
		t.Errorf("reason = %q, want empty", resp.DenyReason)
	}
}

func TestExecute_FileScript(t *testing.T) {
	// Write a temporary .star file
	tmp := t.TempDir()
	starFile := tmp + "/test.star"
	os.WriteFile(starFile, []byte(`response.inject_context("from file")`), 0o644)

	r := testRunner()
	event := &protocol.HookEvent{EventType: "pre-tool-use"}

	resp, err := r.Execute("file://"+starFile, event)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Context != "from file" {
		t.Errorf("context = %q, want 'from file'", resp.Context)
	}
}

func TestExecute_FileScriptNotFound(t *testing.T) {
	r := testRunner()
	event := &protocol.HookEvent{EventType: "pre-tool-use"}

	_, err := r.Execute("file:///nonexistent/path.star", event)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestExecute_NestedToolInput(t *testing.T) {
	r := testRunner()
	event := &protocol.HookEvent{
		EventType: "pre-tool-use",
		ToolName:  "Agent",
		ToolInput: map[string]any{
			"description": "explore code",
			"options":     map[string]any{"depth": "deep"},
			"tags":        []any{"search", "code"},
		},
	}

	script := `
desc = event.tool_input.get("description", "")
if "explore" in desc:
    response.inject_context("exploring: " + desc)
`
	resp, err := r.Execute(script, event)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Context != "exploring: explore code" {
		t.Errorf("context = %q", resp.Context)
	}
}

func TestExecute_RAGSearchWithTopK(t *testing.T) {
	r := NewRunner(&mockRAG{
		results: []hook.SearchResult{
			{Content: "result 1", Score: 0.9},
			{Content: "result 2", Score: 0.8},
			{Content: "result 3", Score: 0.3},
		},
	}, nil, nil)
	event := &protocol.HookEvent{EventType: "pre-tool-use"}

	script := `
results = rag.search("docs", "query", top_k=2)
for r in results:
    if r["score"] > 0.5:
        response.inject_context(r["content"])
`
	resp, err := r.Execute(script, event)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// All 3 returned by mock (mock ignores top_k), but only score > 0.5 injected
	if resp.Context == "" {
		t.Error("should have injected context")
	}
}

func TestExecute_ResponseAttrNames(t *testing.T) {
	rh := newResponseHelper(nil, nil, nil)
	names := rh.AttrNames()
	expected := map[string]bool{"inject_context": true, "approve": true, "deny": true, "ask": true, "prompt": true, "set_output": true, "agent": true}
	for _, n := range names {
		if !expected[n] {
			t.Errorf("unexpected attr: %s", n)
		}
	}
	// Unknown attr returns nil
	val, err := rh.Attr("nonexistent")
	if err != nil || val != nil {
		t.Errorf("unknown attr should return nil, nil; got %v, %v", val, err)
	}
}

func TestGoToStarlark_Types(t *testing.T) {
	// Exercise all branches of goToStarlark
	_ = goToStarlark("string")
	_ = goToStarlark(42.5)
	_ = goToStarlark(true)
	_ = goToStarlark(nil)
	_ = goToStarlark(map[string]any{"key": "val"})
	_ = goToStarlark([]any{"a", 1})
	_ = goToStarlark(struct{}{}) // fallback to fmt.Sprintf
}

// mockInteractor implements Interactor for testing.
type mockInteractor struct {
	response protocol.InteractionResponse
}

func (m *mockInteractor) Prompt(text string, interType protocol.InteractionType, defaultVal string, timeoutSec int) protocol.InteractionResponse {
	return m.response
}

func TestExecute_PromptWithInteractor(t *testing.T) {
	r := testRunner()
	r.SetInteractor(&mockInteractor{
		response: protocol.InteractionResponse{Value: "approve"},
	})
	event := &protocol.HookEvent{EventType: "pre-tool-use", ToolName: "Bash"}

	script := `
result = response.prompt(text="Allow this command?", type="approve_deny_cancel", default="deny", timeout=10)
if result == "approve":
    response.approve()
else:
    response.deny("user denied")
`
	resp, err := r.Execute(script, event)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.PermissionDecision != protocol.PermAllow {
		t.Errorf("decision = %q, want allow", resp.PermissionDecision)
	}
}

func TestExecute_PromptNoInteractor(t *testing.T) {
	r := testRunner() // no interactor set
	event := &protocol.HookEvent{EventType: "pre-tool-use"}

	// Without interactor, prompt returns default value
	script := `
result = response.prompt(text="Question?", default="cancel")
if result == "cancel":
    response.deny("no TUI")
`
	resp, err := r.Execute(script, event)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.PermissionDecision != protocol.PermDeny {
		t.Errorf("decision = %q, want deny (default returned)", resp.PermissionDecision)
	}
}

func TestExecute_InjectInputNoMux(t *testing.T) {
	r := testRunner()
	event := &protocol.HookEvent{EventType: "pre-tool-use"}

	// No mux detected — should error
	_, err := r.Execute(`inject_input([{"keys": "y"}])`, event)
	if err == nil {
		t.Fatal("expected error for inject_input without mux")
	}
}

func TestExecute_InjectInputBadArgs(t *testing.T) {
	r := testRunner()
	event := &protocol.HookEvent{EventType: "pre-tool-use"}

	_, err := r.Execute(`inject_input("not a list")`, event)
	if err == nil {
		t.Fatal("expected error for non-list arg")
	}
}

func TestExecute_SetOutput(t *testing.T) {
	r := testRunner()
	event := &protocol.HookEvent{Agent: "claude", EventType: "pre-tool-use"}

	resp, err := r.Execute(`
response.set_output("customField", "hello")
response.set_output("nested", {"key": "value", "num": 42})
`, event)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if resp.OutputOverrides == nil {
		t.Fatal("expected OutputOverrides to be set")
	}
	if resp.OutputOverrides["customField"] != "hello" {
		t.Errorf("customField = %v, want hello", resp.OutputOverrides["customField"])
	}
	nested, ok := resp.OutputOverrides["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested = %T, want map", resp.OutputOverrides["nested"])
	}
	if nested["key"] != "value" {
		t.Errorf("nested.key = %v, want value", nested["key"])
	}
}

func TestExecute_ResponseAgent(t *testing.T) {
	r := testRunner()
	event := &protocol.HookEvent{Agent: "claude", EventType: "pre-tool-use"}

	resp, err := r.Execute(`
if response.agent == "claude":
  response.inject_context("agent is claude")
`, event)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if resp.Context != "agent is claude" {
		t.Errorf("context = %q, want 'agent is claude'", resp.Context)
	}
}

func TestExecute_ResponseAgentEmpty(t *testing.T) {
	r := testRunner()
	event := &protocol.HookEvent{EventType: "pre-tool-use"}

	resp, err := r.Execute(`
if response.agent == "":
  response.inject_context("no agent")
`, event)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if resp.Context != "no agent" {
		t.Errorf("context = %q, want 'no agent'", resp.Context)
	}
}

func TestStarlarkToGo(t *testing.T) {
	tests := []struct {
		name  string
		input string
		key   string
		want  any
	}{
		{"string", `response.set_output("k", "hello")`, "k", "hello"},
		{"int", `response.set_output("k", 42)`, "k", int64(42)},
		{"float", `response.set_output("k", 3.14)`, "k", 3.14},
		{"bool", `response.set_output("k", True)`, "k", true},
		{"none", `response.set_output("k", None)`, "k", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testRunner()
			event := &protocol.HookEvent{EventType: "pre-tool-use"}
			resp, err := r.Execute(tt.input, event)
			if err != nil {
				t.Fatalf("execute error: %v", err)
			}
			got := resp.OutputOverrides[tt.key]
			if got != tt.want {
				t.Errorf("got %v (%T), want %v (%T)", got, got, tt.want, tt.want)
			}
		})
	}
}

func TestDetectMux_FromEvent(t *testing.T) {
	event := &protocol.HookEvent{
		Mux: &protocol.MuxInfo{Type: "tmux", Pane: "%5"},
	}
	mux := detectMux(event)
	if mux == nil || mux.Type != "tmux" || mux.Pane != "%5" {
		t.Errorf("detectMux from event = %v", mux)
	}
}

func TestDetectMux_NilEvent(t *testing.T) {
	// Without env vars set, should return nil
	mux := detectMux(nil)
	// Can't guarantee env vars aren't set in CI, so just check it doesn't panic
	_ = mux
}
