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

// shortSocketPath returns a short path for Unix sockets (macOS has 104-byte limit).
func shortSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "rt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "t.sock")
}

func TestDaemonRoundTrip(t *testing.T) {
	socketPath := shortSocketPath(t)

	cfg := config.Defaults()
	cfg.Daemon.Socket = socketPath

	d := New(cfg)
	handler := NewHandler(d)
	srv := NewSocketServer(socketPath, handler, d.logger)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	// Connect and send a hook event
	conn, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	event := &protocol.HookEvent{
		Agent:     "claude",
		EventType: "pre-tool-use",
		SessionID: "test-123",
		ToolName:  "Read",
	}

	env, err := protocol.NewEnvelope(protocol.MsgHookEvent, event)
	if err != nil {
		t.Fatalf("NewEnvelope: %v", err)
	}

	conn.SetDeadline(time.Now().Add(2 * time.Second))

	if err := protocol.WriteMessage(conn, env); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}

	respEnv, err := protocol.ReadMessage(conn)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}

	if respEnv.Type != protocol.MsgHookResponse {
		t.Errorf("type = %q, want %q", respEnv.Type, protocol.MsgHookResponse)
	}

	var resp protocol.HookResponse
	if err := respEnv.DecodePayload(&resp); err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}

	// No-op response: empty context, no permission decision
	if resp.Context != "" {
		t.Errorf("context = %q, want empty", resp.Context)
	}
}

func TestDaemonConcurrentConnections(t *testing.T) {
	socketPath := shortSocketPath(t)

	cfg := config.Defaults()
	cfg.Daemon.Socket = socketPath

	d := New(cfg)
	handler := NewHandler(d)
	srv := NewSocketServer(socketPath, handler, d.logger)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop()

	// Fire 10 concurrent requests
	done := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func() {
			conn, err := net.DialTimeout("unix", socketPath, time.Second)
			if err != nil {
				done <- err
				return
			}
			defer conn.Close()

			conn.SetDeadline(time.Now().Add(2 * time.Second))

			event := &protocol.HookEvent{
				Agent:     "claude",
				EventType: "pre-tool-use",
				SessionID: "concurrent-test",
				ToolName:  "Bash",
			}
			env, _ := protocol.NewEnvelope(protocol.MsgHookEvent, event)
			if err := protocol.WriteMessage(conn, env); err != nil {
				done <- err
				return
			}
			_, err = protocol.ReadMessage(conn)
			done <- err
		}()
	}

	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent request %d: %v", i, err)
		}
	}
}
