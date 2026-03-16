package session

import (
	"strings"
	"testing"
	"time"

	"github.com/byronellis/ragtime/internal/protocol"
)

func newTestIndexer() *SessionIndexer {
	return &SessionIndexer{
		lastIndexed: make(map[string]int),
	}
}

func TestFormatEventsAsText_PromptAndResponse(t *testing.T) {
	idx := newTestIndexer()
	now := time.Now()

	events := []Event{
		{Timestamp: now, EventType: "user-prompt-submit", Detail: "implement auth middleware"},
		{Timestamp: now.Add(time.Second), EventType: "pre-tool-use", ToolName: "Read", Detail: "auth.go"},
		{Timestamp: now.Add(2 * time.Second), EventType: "pre-tool-use", ToolName: "Edit", Detail: "auth.go"},
		{Timestamp: now.Add(3 * time.Second), EventType: "stop", Response: "I've implemented the auth middleware."},
	}
	hook := &protocol.HookEvent{Agent: "claude", SessionID: "s1", CWD: "/home/user/myapp"}

	text := idx.formatEventsAsText(events, hook)

	if !strings.Contains(text, "User: implement auth middleware") {
		t.Error("should contain user prompt")
	}
	if !strings.Contains(text, "Agent: I've implemented the auth middleware.") {
		t.Error("should contain agent response")
	}
	if !strings.Contains(text, "Start:") || !strings.Contains(text, "End:") {
		t.Error("should contain RFC3339 correlation timestamps")
	}
	if !strings.Contains(text, "Tool calls: 2") {
		t.Errorf("should show tool call count, got:\n%s", text)
	}
	// Should NOT contain individual tool details
	if strings.Contains(text, "Read: auth.go") {
		t.Error("should not inline tool details")
	}
	if strings.Contains(text, "Edited:") {
		t.Error("should not inline edit details")
	}
}

func TestFormatEventsAsText_Empty(t *testing.T) {
	idx := newTestIndexer()
	hook := &protocol.HookEvent{Agent: "claude", SessionID: "s1", CWD: "/tmp"}

	text := idx.formatEventsAsText(nil, hook)
	if text != "" {
		t.Errorf("empty events should return empty, got %q", text)
	}
}

func TestFormatEventsAsText_ToolsOnlyNoConversation(t *testing.T) {
	idx := newTestIndexer()
	now := time.Now()

	// Only tool events, no prompt or response — should produce empty
	events := []Event{
		{Timestamp: now, EventType: "pre-tool-use", ToolName: "Read", Detail: "foo.go"},
		{Timestamp: now.Add(time.Second), EventType: "post-tool-use", ToolName: "Read"},
	}
	hook := &protocol.HookEvent{Agent: "claude", SessionID: "s1", CWD: "/tmp"}

	text := idx.formatEventsAsText(events, hook)
	if text != "" {
		t.Errorf("tool-only turn should return empty, got %q", text)
	}
}

func TestFormatEventsAsText_DeniedAction(t *testing.T) {
	idx := newTestIndexer()
	now := time.Now()

	events := []Event{
		{Timestamp: now, EventType: "user-prompt-submit", Detail: "delete everything"},
		{Timestamp: now.Add(time.Second), EventType: "pre-tool-use", ToolName: "Bash", Detail: "rm -rf /", Decision: "deny"},
	}
	hook := &protocol.HookEvent{Agent: "claude", SessionID: "s1", CWD: "/tmp"}

	text := idx.formatEventsAsText(events, hook)
	if !strings.Contains(text, "Denied: Bash: rm -rf /") {
		t.Errorf("should contain denied action, got:\n%s", text)
	}
}

func TestFormatEventsAsText_ContextInjected(t *testing.T) {
	idx := newTestIndexer()
	now := time.Now()

	events := []Event{
		{Timestamp: now, EventType: "user-prompt-submit", Detail: "what does the auth do?"},
		{Timestamp: now.Add(time.Second), EventType: "pre-tool-use", ToolName: "Read", HasContext: true},
	}
	hook := &protocol.HookEvent{Agent: "claude", SessionID: "s1", CWD: "/tmp"}

	text := idx.formatEventsAsText(events, hook)
	if !strings.Contains(text, "Context injected: yes") {
		t.Errorf("should indicate context injection, got:\n%s", text)
	}
}

func TestFormatEventsAsText_TimestampCorrelation(t *testing.T) {
	idx := newTestIndexer()
	start := time.Date(2026, 3, 15, 14, 30, 0, 0, time.UTC)
	end := start.Add(5 * time.Minute)

	events := []Event{
		{Timestamp: start, EventType: "user-prompt-submit", Detail: "fix the bug"},
		{Timestamp: end, EventType: "stop", Response: "Fixed."},
	}
	hook := &protocol.HookEvent{Agent: "claude", SessionID: "s1", CWD: "/project"}

	text := idx.formatEventsAsText(events, hook)

	// Should contain machine-readable RFC3339 timestamps
	if !strings.Contains(text, "2026-03-15T14:30:00Z") {
		t.Errorf("should contain start RFC3339 timestamp, got:\n%s", text)
	}
	if !strings.Contains(text, "2026-03-15T14:35:00Z") {
		t.Errorf("should contain end RFC3339 timestamp, got:\n%s", text)
	}
}

func TestFormatEventsAsText_LongResponseTruncated(t *testing.T) {
	idx := newTestIndexer()
	now := time.Now()

	longResp := strings.Repeat("x", 3000)
	events := []Event{
		{Timestamp: now, EventType: "user-prompt-submit", Detail: "explain"},
		{Timestamp: now.Add(time.Second), EventType: "stop", Response: longResp},
	}
	hook := &protocol.HookEvent{Agent: "claude", SessionID: "s1", CWD: "/tmp"}

	text := idx.formatEventsAsText(events, hook)
	if !strings.Contains(text, "...") {
		t.Error("long response should be truncated")
	}
	// Should be truncated to ~2000 chars
	agentLine := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "Agent: ") {
			agentLine = line
			break
		}
	}
	// "Agent: " is 7 chars + 2000 content + "..." = 2010
	if len(agentLine) > 2020 {
		t.Errorf("agent response should be ~2010 chars, got %d", len(agentLine))
	}
}

func TestFormatEventsAsText_MatchedRules(t *testing.T) {
	idx := newTestIndexer()
	now := time.Now()

	events := []Event{
		{Timestamp: now, EventType: "user-prompt-submit", Detail: "read the config"},
		{Timestamp: now.Add(time.Second), EventType: "pre-tool-use", ToolName: "Read", MatchedRules: []string{"auto-approve-reads", "inject-docs"}},
		{Timestamp: now.Add(2 * time.Second), EventType: "pre-tool-use", ToolName: "Read", MatchedRules: []string{"auto-approve-reads"}},
		{Timestamp: now.Add(3 * time.Second), EventType: "stop", Response: "Done."},
	}
	hook := &protocol.HookEvent{Agent: "claude", SessionID: "s1", CWD: "/tmp"}

	text := idx.formatEventsAsText(events, hook)

	if !strings.Contains(text, "Rules: auto-approve-reads, inject-docs") {
		t.Errorf("should list deduped matched rules, got:\n%s", text)
	}
}

func TestFormatEventsAsText_NoRules(t *testing.T) {
	idx := newTestIndexer()
	now := time.Now()

	events := []Event{
		{Timestamp: now, EventType: "user-prompt-submit", Detail: "hello"},
		{Timestamp: now.Add(time.Second), EventType: "stop", Response: "Hi."},
	}
	hook := &protocol.HookEvent{Agent: "claude", SessionID: "s1", CWD: "/tmp"}

	text := idx.formatEventsAsText(events, hook)
	if strings.Contains(text, "Rules:") {
		t.Error("should not include Rules line when none matched")
	}
}
