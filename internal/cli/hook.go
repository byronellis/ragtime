package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/byronellis/ragtime/internal/config"
	"github.com/byronellis/ragtime/internal/daemon"
	"github.com/byronellis/ragtime/internal/hook/adapters"
	"github.com/byronellis/ragtime/internal/protocol"
	"github.com/spf13/cobra"
)

func newHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook",
		Short: "Handle an agent hook event",
		Long:  "Reads a hook event from stdin, relays it to the daemon, and writes the response to stdout. Invoked by agent hook systems (Claude Code, Gemini CLI).",
		RunE:  runHook,
	}

	cmd.Flags().String("agent", "", "agent platform (claude, gemini)")
	cmd.Flags().String("event", "", "event type (pre-tool-use, post-tool-use, stop, notification, etc.)")

	return cmd
}

func runHook(cmd *cobra.Command, args []string) error {
	agent, _ := cmd.Flags().GetString("agent")
	eventType, _ := cmd.Flags().GetString("event")

	if agent == "" || eventType == "" {
		return fmt.Errorf("--agent and --event flags are required")
	}

	// Read stdin
	stdinData, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}

	// Parse into universal event based on agent type
	var event *protocol.HookEvent
	switch agent {
	case "claude":
		event, err = adapters.ParseClaudeEvent(stdinData, eventType)
		if err != nil {
			return fmt.Errorf("parse claude event: %w", err)
		}
	default:
		return fmt.Errorf("unsupported agent: %s", agent)
	}

	// Resolve socket path
	socketPath := flagSocket
	if socketPath == "" {
		cfg, err := config.Load(".")
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		socketPath = cfg.Daemon.Socket
	}

	// Ensure daemon is running
	if err := daemon.EnsureRunning(socketPath); err != nil {
		// If we can't start daemon, exit silently (don't break agent)
		return nil
	}

	// Connect to daemon
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		// Fail silently — don't break the agent
		return nil
	}
	defer conn.Close()

	// Set deadline for the entire exchange
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	// Send event
	env, err := protocol.NewEnvelope(protocol.MsgHookEvent, event)
	if err != nil {
		return nil
	}
	if err := protocol.WriteMessage(conn, env); err != nil {
		return nil
	}

	// Read response
	respEnv, err := protocol.ReadMessage(conn)
	if err != nil {
		return nil
	}

	var hookResp protocol.HookResponse
	if err := respEnv.DecodePayload(&hookResp); err != nil {
		return nil
	}

	// Format response for the specific agent
	var output any
	switch agent {
	case "claude":
		output = adapters.FormatClaudeResponse(&hookResp, eventType)
	}

	// Write JSON to stdout
	if output != nil {
		data, err := json.Marshal(output)
		if err != nil {
			return nil
		}
		// Only write if there's meaningful content
		if string(data) != "{}" {
			os.Stdout.Write(data)
		}
	}

	return nil
}
