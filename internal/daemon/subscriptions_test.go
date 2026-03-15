package daemon

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/byronellis/ragtime/internal/config"
	"github.com/byronellis/ragtime/internal/protocol"
)

func testDaemon(t *testing.T) *Daemon {
	t.Helper()
	cfg := config.Defaults()
	cfg.Daemon.Socket = filepath.Join(t.TempDir(), "test.sock")
	return New(cfg)
}

func TestSubscriptionManagerRegisterUnregister(t *testing.T) {
	d := testDaemon(t)
	sm := NewSubscriptionManager(d, d.logger)

	// Create a pipe to simulate a connection
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	resp := sm.Register(server)

	if !resp.Success {
		t.Fatal("register should succeed")
	}
	if resp.DaemonInfo.PID != os.Getpid() {
		t.Errorf("pid = %d, want %d", resp.DaemonInfo.PID, os.Getpid())
	}

	sm.mu.RLock()
	count := len(sm.clients)
	sm.mu.RUnlock()
	if count != 1 {
		t.Errorf("client count = %d, want 1", count)
	}

	sm.Unregister(server)

	sm.mu.RLock()
	count = len(sm.clients)
	sm.mu.RUnlock()
	if count != 0 {
		t.Errorf("client count after unregister = %d, want 0", count)
	}
}

func TestSubscriptionManagerSnapshot(t *testing.T) {
	d := testDaemon(t)
	d.startedAt = time.Now().Add(-1 * time.Hour)

	// Create a session so the snapshot includes it
	d.sessions.GetOrCreate("claude", "sess-1")

	sm := NewSubscriptionManager(d, d.logger)

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	resp := sm.Register(server)

	if len(resp.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(resp.Sessions))
	}
	if resp.Sessions[0].Agent != "claude" {
		t.Errorf("session agent = %q, want %q", resp.Sessions[0].Agent, "claude")
	}
	if resp.Sessions[0].SessionID != "sess-1" {
		t.Errorf("session id = %q, want %q", resp.Sessions[0].SessionID, "sess-1")
	}
}

func TestSubscriptionManagerBroadcast(t *testing.T) {
	d := testDaemon(t)
	sm := NewSubscriptionManager(d, d.logger)

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	sm.Register(server)

	event := &protocol.StreamEvent{
		Kind:      "hook_event",
		Timestamp: time.Now(),
		Event: &protocol.HookEvent{
			Agent:    "claude",
			ToolName: "Read",
		},
	}

	// Broadcast in a goroutine since the pipe is synchronous
	done := make(chan struct{})
	go func() {
		sm.Broadcast(event)
		close(done)
	}()

	// Read from client side
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	env, err := protocol.ReadMessage(client)
	if err != nil {
		t.Fatalf("read broadcast: %v", err)
	}

	<-done

	if env.Type != protocol.MsgEvent {
		t.Errorf("type = %q, want %q", env.Type, protocol.MsgEvent)
	}

	var got protocol.StreamEvent
	if err := env.DecodePayload(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Kind != "hook_event" {
		t.Errorf("kind = %q, want %q", got.Kind, "hook_event")
	}
	if got.Event.ToolName != "Read" {
		t.Errorf("tool = %q, want %q", got.Event.ToolName, "Read")
	}
}

func TestSubscriptionManagerBroadcastRemovesBrokenClients(t *testing.T) {
	d := testDaemon(t)
	sm := NewSubscriptionManager(d, d.logger)

	server, client := net.Pipe()
	sm.Register(server)

	// Close the client side to break the pipe
	client.Close()
	server.Close()

	event := &protocol.StreamEvent{
		Kind:      "hook_event",
		Timestamp: time.Now(),
		Event:     &protocol.HookEvent{Agent: "test"},
	}

	sm.Broadcast(event)

	sm.mu.RLock()
	count := len(sm.clients)
	sm.mu.RUnlock()
	if count != 0 {
		t.Errorf("broken client should be removed, count = %d", count)
	}
}

func TestSubscriptionManagerBroadcastNoClients(t *testing.T) {
	d := testDaemon(t)
	sm := NewSubscriptionManager(d, d.logger)

	// Should not panic
	sm.Broadcast(&protocol.StreamEvent{
		Kind:      "hook_event",
		Timestamp: time.Now(),
		Event:     &protocol.HookEvent{Agent: "test"},
	})
}
