package fs

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/byronellis/ragtime/internal/protocol"
)

// daemonClient sends commands to the ragtime daemon and returns responses.
type daemonClient struct {
	socketPath string
}

func newDaemonClient(socketPath string) *daemonClient {
	return &daemonClient{socketPath: socketPath}
}

func (c *daemonClient) command(cmd string, args map[string]any) (*protocol.CommandResponse, error) {
	conn, err := net.DialTimeout("unix", c.socketPath, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect daemon: %w", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	env, err := protocol.NewEnvelope(protocol.MsgCommand, &protocol.CommandRequest{
		Command: cmd,
		Args:    args,
	})
	if err != nil {
		return nil, err
	}
	if err := protocol.WriteMessage(conn, env); err != nil {
		return nil, fmt.Errorf("send: %w", err)
	}

	respEnv, err := protocol.ReadMessage(conn)
	if err != nil {
		return nil, fmt.Errorf("recv: %w", err)
	}

	var resp protocol.CommandResponse
	if err := respEnv.DecodePayload(&resp); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	return &resp, nil
}

// listSessions returns all active sessions.
func (c *daemonClient) listSessions() ([]sessionSummary, error) {
	resp, err := c.command("sessions", nil)
	if err != nil {
		return nil, err
	}
	data, _ := json.Marshal(resp.Data)
	var sessions []sessionSummary
	json.Unmarshal(data, &sessions)
	return sessions, nil
}

// sessionHistory returns recent events for a session.
func (c *daemonClient) sessionHistory(agent, sessionID string, last int) ([]map[string]any, error) {
	if last <= 0 {
		last = 1000
	}
	resp, err := c.command("session-history", map[string]any{
		"agent":      agent,
		"session_id": sessionID,
		"last":       float64(last),
	})
	if err != nil {
		return nil, err
	}
	data, _ := json.Marshal(resp.Data)
	var events []map[string]any
	json.Unmarshal(data, &events)
	return events, nil
}

// listShells returns all running shells.
func (c *daemonClient) listShells() ([]protocol.ShellInfo, error) {
	resp, err := c.command("shell-list", map[string]any{"include_stopped": false})
	if err != nil {
		return nil, err
	}
	data, _ := json.Marshal(resp.Data)
	var shells []protocol.ShellInfo
	json.Unmarshal(data, &shells)
	return shells, nil
}

// captureShell returns the scrollback buffer for a shell.
func (c *daemonClient) captureShell(id string) (string, error) {
	resp, err := c.command("shell-capture", map[string]any{"id": id})
	if err != nil {
		return "", err
	}
	s, _ := resp.Data.(string)
	return s, nil
}

// sessionSummary is what the daemon returns for the sessions command.
type sessionSummary struct {
	Agent      string    `json:"agent"`
	SessionID  string    `json:"session_id"`
	StartedAt  time.Time `json:"started_at"`
	EventCount int       `json:"event_count"`
	LastEvent  time.Time `json:"last_event,omitempty"`
}
