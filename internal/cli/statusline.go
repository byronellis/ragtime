package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/byronellis/ragtime/internal/config"
	"github.com/byronellis/ragtime/internal/daemon"
	"github.com/byronellis/ragtime/internal/protocol"
	"github.com/spf13/cobra"
)

func statuslineLog(format string, args ...any) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(home, ".ragtime", "statusline.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[%s] "+format+"\n", append([]any{time.Now().Format(time.RFC3339)}, args...)...)
}

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

	statuslineLog("called agent=%s", agent)

	// Read JSON from stdin
	stdinData, err := io.ReadAll(os.Stdin)
	if err != nil {
		statuslineLog("stdin read error: %v", err)
		return nil
	}
	if len(stdinData) == 0 {
		statuslineLog("stdin empty, nothing to record")
		return nil
	}

	statuslineLog("stdin bytes=%d data=%s", len(stdinData), stdinData)

	// Parse the statusline JSON
	var event protocol.StatuslineEvent
	if err := json.Unmarshal(stdinData, &event); err != nil {
		statuslineLog("json parse error: %v", err)
		return nil
	}
	event.Agent = agent

	statuslineLog("parsed session_id=%s model=%s cost=%.4f used_pct=%d", event.SessionID, event.Model.ID, event.Cost.TotalCostUSD, event.ContextWindow.UsedPercentage)

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
			statuslineLog("config load error: %v", err)
			return nil
		}
		socketPath = cfg.Daemon.Socket
	}

	statuslineLog("socket=%s", socketPath)

	// Ensure daemon is running
	if err := daemon.EnsureRunning(socketPath); err != nil {
		statuslineLog("daemon start error: %v", err)
		return nil
	}

	// Connect to daemon
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		statuslineLog("dial error: %v", err)
		return nil
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	// Send statusline event
	env, err := protocol.NewEnvelope(protocol.MsgStatuslineEvent, &event)
	if err != nil {
		statuslineLog("envelope error: %v", err)
		return nil
	}
	if err := protocol.WriteMessage(conn, env); err != nil {
		statuslineLog("write error: %v", err)
		return nil
	}

	// Read response (just an ack)
	_, _ = protocol.ReadMessage(conn)

	statuslineLog("event sent successfully")

	// No stdout output — Claude Code StatusUpdate hook doesn't expect any
	return nil
}
