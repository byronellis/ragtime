package daemon

import (
	"net"
	"testing"
	"time"

	"github.com/byronellis/ragtime/internal/protocol"
)

func TestStreamSubscription(t *testing.T) {
	socketPath := shortSocketPath(t)

	d := testDaemon(t)
	d.cfg.Daemon.Socket = socketPath
	d.startedAt = time.Now()

	handler := NewHandler(d)
	subs := NewSubscriptionManager(d, d.logger)
	srv := NewSocketServer(socketPath, handler, subs, d.logger)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	// Connect as a TUI subscriber
	conn, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(5 * time.Second))

	// Send subscribe request
	subReq, _ := protocol.NewEnvelope(protocol.MsgSubscribe, &protocol.SubscribeRequest{})
	if err := protocol.WriteMessage(conn, subReq); err != nil {
		t.Fatalf("WriteMessage subscribe: %v", err)
	}

	// Read subscribe response
	respEnv, err := protocol.ReadMessage(conn)
	if err != nil {
		t.Fatalf("ReadMessage subscribe resp: %v", err)
	}

	if respEnv.Type != protocol.MsgSubscribe {
		t.Errorf("type = %q, want %q", respEnv.Type, protocol.MsgSubscribe)
	}

	var resp protocol.SubscribeResponse
	if err := respEnv.DecodePayload(&resp); err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	if !resp.Success {
		t.Fatalf("subscribe failed: %s", resp.Error)
	}
	if resp.DaemonInfo.PID == 0 {
		t.Error("PID should be set")
	}

	// Broadcast an event
	event := &protocol.StreamEvent{
		Kind:      "hook_event",
		Timestamp: time.Now(),
		Event: &protocol.HookEvent{
			Agent:    "claude",
			ToolName: "Bash",
		},
	}
	subs.Broadcast(event)

	// Read the streamed event
	eventEnv, err := protocol.ReadMessage(conn)
	if err != nil {
		t.Fatalf("ReadMessage event: %v", err)
	}

	if eventEnv.Type != protocol.MsgEvent {
		t.Errorf("event type = %q, want %q", eventEnv.Type, protocol.MsgEvent)
	}

	var got protocol.StreamEvent
	if err := eventEnv.DecodePayload(&got); err != nil {
		t.Fatalf("DecodePayload event: %v", err)
	}
	if got.Event.ToolName != "Bash" {
		t.Errorf("tool = %q, want %q", got.Event.ToolName, "Bash")
	}
}

func TestStreamDisconnectCleansUp(t *testing.T) {
	socketPath := shortSocketPath(t)

	d := testDaemon(t)
	d.cfg.Daemon.Socket = socketPath
	d.startedAt = time.Now()

	handler := NewHandler(d)
	subs := NewSubscriptionManager(d, d.logger)
	srv := NewSocketServer(socketPath, handler, subs, d.logger)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	// Connect as subscriber
	conn, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	conn.SetDeadline(time.Now().Add(5 * time.Second))

	subReq, _ := protocol.NewEnvelope(protocol.MsgSubscribe, &protocol.SubscribeRequest{})
	protocol.WriteMessage(conn, subReq)
	protocol.ReadMessage(conn) // read response

	// Verify client is registered
	subs.mu.RLock()
	count := len(subs.clients)
	subs.mu.RUnlock()
	if count != 1 {
		t.Fatalf("client count = %d, want 1", count)
	}

	// Disconnect
	conn.Close()

	// Give the server time to notice the disconnect
	time.Sleep(100 * time.Millisecond)

	// Broadcast should clean up the broken client
	subs.Broadcast(&protocol.StreamEvent{
		Kind:      "hook_event",
		Timestamp: time.Now(),
		Event:     &protocol.HookEvent{Agent: "test"},
	})

	subs.mu.RLock()
	count = len(subs.clients)
	subs.mu.RUnlock()
	if count != 0 {
		t.Errorf("client count after disconnect = %d, want 0", count)
	}
}

func TestStreamAndRequestResponseCoexist(t *testing.T) {
	socketPath := shortSocketPath(t)

	d := testDaemon(t)
	d.cfg.Daemon.Socket = socketPath
	d.startedAt = time.Now()

	handler := NewHandler(d)
	subs := NewSubscriptionManager(d, d.logger)
	srv := NewSocketServer(socketPath, handler, subs, d.logger)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	// Connect a streaming subscriber
	streamConn, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		t.Fatalf("Dial stream: %v", err)
	}
	defer streamConn.Close()
	streamConn.SetDeadline(time.Now().Add(5 * time.Second))

	subReq, _ := protocol.NewEnvelope(protocol.MsgSubscribe, &protocol.SubscribeRequest{})
	protocol.WriteMessage(streamConn, subReq)
	protocol.ReadMessage(streamConn)

	// Connect a regular request-response client
	reqConn, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		t.Fatalf("Dial request: %v", err)
	}
	defer reqConn.Close()
	reqConn.SetDeadline(time.Now().Add(2 * time.Second))

	event := &protocol.HookEvent{
		Agent:     "claude",
		EventType: "pre-tool-use",
		SessionID: "test-coexist",
		ToolName:  "Read",
	}
	env, _ := protocol.NewEnvelope(protocol.MsgHookEvent, event)
	protocol.WriteMessage(reqConn, env)

	respEnv, err := protocol.ReadMessage(reqConn)
	if err != nil {
		t.Fatalf("ReadMessage response: %v", err)
	}
	if respEnv.Type != protocol.MsgHookResponse {
		t.Errorf("response type = %q, want %q", respEnv.Type, protocol.MsgHookResponse)
	}
}
