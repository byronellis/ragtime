package tui

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/byronellis/ragtime/internal/protocol"
)

// startTestServer starts a Unix socket server that handles subscribe requests
// and can broadcast events. Returns the socket path and a broadcast function.
func startTestServer(t *testing.T) (string, func(*protocol.StreamEvent)) {
	t.Helper()

	dir := t.TempDir()
	sockPath := filepath.Join(dir, "t.sock")

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	os.Chmod(sockPath, 0o700)

	type client struct {
		conn net.Conn
	}
	clients := make(chan *client, 10)

	// Accept loop
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				env, err := protocol.ReadMessage(c)
				if err != nil {
					return
				}
				if env.Type != protocol.MsgSubscribe {
					return
				}

				resp := &protocol.SubscribeResponse{
					Success: true,
					DaemonInfo: protocol.DaemonInfo{
						PID:        os.Getpid(),
						StartedAt:  time.Now(),
						SocketPath: sockPath,
						RuleCount:  2,
					},
				}
				respEnv, _ := protocol.NewEnvelope(protocol.MsgSubscribe, resp)
				protocol.WriteMessage(c, respEnv)

				clients <- &client{conn: c}
			}(conn)
		}
	}()

	t.Cleanup(func() {
		ln.Close()
		close(clients)
	})

	broadcast := func(event *protocol.StreamEvent) {
		// Drain and send to all clients we've seen
		for {
			select {
			case cl := <-clients:
				env, _ := protocol.NewEnvelope(protocol.MsgEvent, event)
				protocol.WriteMessage(cl.conn, env)
			default:
				return
			}
		}
	}

	return sockPath, broadcast
}

func TestClientConnect(t *testing.T) {
	sockPath, _ := startTestServer(t)

	client, info, err := Connect(sockPath)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Close()

	if !info.Success {
		t.Fatal("expected success")
	}
	if info.DaemonInfo.PID != os.Getpid() {
		t.Errorf("pid = %d, want %d", info.DaemonInfo.PID, os.Getpid())
	}
	if info.DaemonInfo.RuleCount != 2 {
		t.Errorf("rules = %d, want 2", info.DaemonInfo.RuleCount)
	}
}

func TestClientConnectFailure(t *testing.T) {
	_, _, err := Connect("/tmp/nonexistent-ragtime-test.sock")
	if err == nil {
		t.Fatal("expected error connecting to nonexistent socket")
	}
}

func TestClientReadLoop(t *testing.T) {
	sockPath, _ := startTestServer(t)

	client, _, err := Connect(sockPath)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Close()

	// Create a minimal tea.Program just to receive messages
	// We'll use a channel-based approach instead
	received := make(chan tea.Msg, 10)
	p := tea.NewProgram(readLoopTestModel{ch: received}, tea.WithoutRenderer())

	go client.ReadLoop(p)

	// Close the connection to trigger disconnect
	time.Sleep(50 * time.Millisecond)
	client.Close()

	// The read loop should send a DisconnectedMsg
	select {
	case <-time.After(2 * time.Second):
		// Read loop exits on close, which is expected
	case msg := <-received:
		if _, ok := msg.(DisconnectedMsg); !ok {
			t.Errorf("expected DisconnectedMsg, got %T", msg)
		}
	}
}

func TestClientClose(t *testing.T) {
	sockPath, _ := startTestServer(t)

	client, _, err := Connect(sockPath)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if err := client.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// readLoopTestModel is a minimal tea.Model for testing
type readLoopTestModel struct {
	ch chan<- tea.Msg
}

func (m readLoopTestModel) Init() tea.Cmd { return nil }
func (m readLoopTestModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	select {
	case m.ch <- msg:
	default:
	}
	return m, tea.Quit
}
func (m readLoopTestModel) View() string { return "" }
