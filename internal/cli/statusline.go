package cli

import (
	"encoding/json"
	"io"
	"net"
	"os"
	"time"

	"github.com/byronellis/ragtime/internal/config"
	"github.com/byronellis/ragtime/internal/daemon"
	"github.com/byronellis/ragtime/internal/protocol"
	"github.com/spf13/cobra"
)

func newStatuslineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "statusline",
		Short: "Record a statusline event from an AI agent",
		Long:  "Reads statusline JSON from stdin and forwards it to the daemon for recording. Used as a Claude Code StatusUpdate hook.",
		RunE:  runStatusline,
	}

	cmd.Flags().String("agent", "claude", "agent platform")

	return cmd
}

func runStatusline(cmd *cobra.Command, args []string) error {
	agent, _ := cmd.Flags().GetString("agent")

	// Read JSON from stdin
	stdinData, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil // fail silently
	}
	if len(stdinData) == 0 {
		return nil
	}

	// Parse the statusline JSON
	var event protocol.StatuslineEvent
	if err := json.Unmarshal(stdinData, &event); err != nil {
		return nil // fail silently
	}
	event.Agent = agent

	// If running inside a ragtime shell, tag the event for correlation
	if shellID := os.Getenv("RAGTIME_SHELL_ID"); shellID != "" {
		event.ShellID = shellID
	}

	// Resolve socket path: prefer RAGTIME_SOCKET env (fast path for shells),
	// then flag, then config discovery
	socketPath := flagSocket
	if socketPath == "" {
		if s := os.Getenv("RAGTIME_SOCKET"); s != "" {
			socketPath = s
		}
	}
	if socketPath == "" {
		cfg, err := config.Load(".")
		if err != nil {
			return nil
		}
		socketPath = cfg.Daemon.Socket
	}

	// Ensure daemon is running
	if err := daemon.EnsureRunning(socketPath); err != nil {
		return nil
	}

	// Connect to daemon
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return nil
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	// Send statusline event
	env, err := protocol.NewEnvelope(protocol.MsgStatuslineEvent, &event)
	if err != nil {
		return nil
	}
	if err := protocol.WriteMessage(conn, env); err != nil {
		return nil
	}

	// Read response (just an ack)
	_, _ = protocol.ReadMessage(conn)

	// No stdout output — Claude Code StatusUpdate hook doesn't expect any
	return nil
}
