package cli

import (
	"fmt"
	"net"
	"time"

	"github.com/byronellis/ragtime/internal/config"
	"github.com/byronellis/ragtime/internal/protocol"
)

// sendCommand dials the daemon, sends a command, and returns the response.
func sendCommand(cmd string, args map[string]any) (*protocol.CommandResponse, error) {
	socketPath := flagSocket
	if socketPath == "" {
		cfg, err := config.Load(".")
		if err != nil {
			return nil, fmt.Errorf("load config: %w", err)
		}
		socketPath = cfg.Daemon.Socket
	}

	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect to daemon: %w", err)
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
		return nil, fmt.Errorf("send command: %w", err)
	}

	respEnv, err := protocol.ReadMessage(conn)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var resp protocol.CommandResponse
	if err := respEnv.DecodePayload(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("%s", resp.Error)
	}
	return &resp, nil
}
