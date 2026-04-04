package cli

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/byronellis/ragtime/internal/config"
	"github.com/byronellis/ragtime/internal/daemon"
	"github.com/byronellis/ragtime/internal/protocol"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newShCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sh",
		Short: "Manage PTY shell sessions",
		Long:  "Create, attach to, and manage background PTY shell sessions in the ragtime daemon.",
	}

	cmd.AddCommand(
		newShNewCmd(),
		newShListCmd(),
		newShAttachCmd(),
		newShSendCmd(),
		newShCaptureCmd(),
		newShKillCmd(),
	)

	return cmd
}

func newShNewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new [-- command [args...]]",
		Short: "Start a new shell session",
		RunE:  runShNew,
	}
	cmd.Flags().String("name", "", "name for the shell session")
	cmd.Flags().String("cwd", "", "working directory")
	cmd.Flags().StringArray("env", nil, "environment variables (KEY=VALUE)")
	cmd.Flags().Bool("attach", false, "attach immediately after creating")
	return cmd
}

func newShListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List shell sessions",
		RunE:  runShList,
	}
}

func newShAttachCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "attach <id>",
		Short: "Attach to a shell session's PTY",
		Args:  cobra.ExactArgs(1),
		RunE:  runShAttach,
	}
}

func newShSendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "send <id> <text>",
		Short: "Send text to a shell session",
		Args:  cobra.ExactArgs(2),
		RunE:  runShSend,
	}
	cmd.Flags().Bool("enter", false, "append a newline after the text")
	return cmd
}

func newShCaptureCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "capture <id>",
		Short: "Capture scrollback from a shell session",
		Args:  cobra.ExactArgs(1),
		RunE:  runShCapture,
	}
}

func newShKillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kill <id>",
		Short: "Kill a shell session",
		Args:  cobra.ExactArgs(1),
		RunE:  runShKill,
	}
	cmd.Flags().String("signal", "SIGTERM", "signal to send (SIGTERM or SIGKILL)")
	return cmd
}

func runShNew(cmd *cobra.Command, args []string) error {
	name, _ := cmd.Flags().GetString("name")
	cwd, _ := cmd.Flags().GetString("cwd")
	envVars, _ := cmd.Flags().GetStringArray("env")
	attach, _ := cmd.Flags().GetBool("attach")

	// Command comes after --
	command := args

	reqArgs := map[string]any{
		"command": command,
	}
	if name != "" {
		reqArgs["name"] = name
	}
	if cwd != "" {
		reqArgs["cwd"] = cwd
	}
	if len(envVars) > 0 {
		reqArgs["env"] = envVars
	}

	resp, err := sendCommand("shell-new", reqArgs)
	if err != nil {
		return fmt.Errorf("create shell: %w", err)
	}

	// Parse shell info from response
	data, _ := json.Marshal(resp.Data)
	var info protocol.ShellInfo
	json.Unmarshal(data, &info)

	fmt.Printf("Shell %s started (pid %d)\n", info.ID, info.PID)

	if attach {
		return doShellAttach(info.ID)
	}
	return nil
}

func runShList(cmd *cobra.Command, args []string) error {
	resp, err := sendCommand("shell-list", map[string]any{"include_stopped": true})
	if err != nil {
		return err
	}

	data, _ := json.Marshal(resp.Data)
	var infos []protocol.ShellInfo
	json.Unmarshal(data, &infos)

	if len(infos) == 0 {
		fmt.Println("No shells")
		return nil
	}

	fmt.Printf("%-10s %-12s %-8s %-20s %s\n", "ID", "NAME", "STATE", "STARTED", "COMMAND")
	for _, info := range infos {
		name := info.Name
		if name == "" {
			name = "-"
		}
		cmdStr := strings.Join(info.Command, " ")
		if len(cmdStr) > 40 {
			cmdStr = cmdStr[:37] + "..."
		}
		fmt.Printf("%-10s %-12s %-8s %-20s %s\n",
			info.ID, name, info.State,
			info.StartedAt.Format("2006-01-02 15:04:05"),
			cmdStr,
		)
	}
	return nil
}

func runShAttach(cmd *cobra.Command, args []string) error {
	return doShellAttach(args[0])
}

func doShellAttach(id string) error {
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
		return fmt.Errorf("daemon not running: %w", err)
	}

	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer conn.Close()

	// Get current terminal size
	var cols, rows uint16 = 80, 24
	if w, h, err := term.GetSize(int(os.Stdin.Fd())); err == nil {
		cols = uint16(w)
		rows = uint16(h)
	}

	// Send attach request
	req := &protocol.ShellAttachRequest{
		ID:   id,
		Cols: cols,
		Rows: rows,
	}
	env, err := protocol.NewEnvelope(protocol.MsgShellAttach, req)
	if err != nil {
		return err
	}
	if err := protocol.WriteMessage(conn, env); err != nil {
		return fmt.Errorf("send attach: %w", err)
	}

	// Set terminal to raw mode
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("set raw mode: %w", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	// Handle SIGWINCH for terminal resize
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH)
	defer signal.Stop(sigCh)

	done := make(chan struct{})

	// Goroutine: read from conn (shell output), write to stdout
	go func() {
		defer close(done)
		for {
			msg, err := protocol.ReadMessage(conn)
			if err != nil {
				return
			}
			if msg.Type == protocol.MsgShellOutput {
				var out protocol.ShellOutputMessage
				if err := msg.DecodePayload(&out); err == nil {
					os.Stdout.Write(out.Data)
				}
			}
		}
	}()

	// Goroutine: handle SIGWINCH
	go func() {
		for {
			select {
			case <-sigCh:
				if w, h, err := term.GetSize(int(os.Stdin.Fd())); err == nil {
					resize := &protocol.ShellResizeMessage{
						ID:   id,
						Cols: uint16(w),
						Rows: uint16(h),
					}
					resizeEnv, err := protocol.NewEnvelope(protocol.MsgShellResize, resize)
					if err == nil {
						protocol.WriteMessage(conn, resizeEnv)
					}
				}
			case <-done:
				return
			}
		}
	}()

	// Read from stdin, send to shell
	buf := make([]byte, 4096)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			break
		}
		if n == 0 {
			continue
		}

		data := buf[:n]

		// Check for detach sequence: ctrl+\ (0x1c)
		for _, b := range data {
			if b == 0x1c {
				fmt.Fprintf(os.Stderr, "\r\n[detached from %s]\r\n", id)
				return nil
			}
		}

		input := &protocol.ShellInputMessage{
			ID:   id,
			Data: data,
		}
		inputEnv, err := protocol.NewEnvelope(protocol.MsgShellInput, input)
		if err != nil {
			break
		}
		if err := protocol.WriteMessage(conn, inputEnv); err != nil {
			break
		}
	}

	<-done
	return nil
}

func runShSend(cmd *cobra.Command, args []string) error {
	enter, _ := cmd.Flags().GetBool("enter")

	_, err := sendCommand("shell-send", map[string]any{
		"id":    args[0],
		"text":  args[1],
		"enter": enter,
	})
	return err
}

func runShCapture(cmd *cobra.Command, args []string) error {
	resp, err := sendCommand("shell-capture", map[string]any{
		"id": args[0],
	})
	if err != nil {
		return err
	}

	if s, ok := resp.Data.(string); ok {
		fmt.Print(s)
	}
	return nil
}

func runShKill(cmd *cobra.Command, args []string) error {
	sig, _ := cmd.Flags().GetString("signal")

	_, err := sendCommand("shell-kill", map[string]any{
		"id":     args[0],
		"signal": sig,
	})
	if err != nil {
		return err
	}
	fmt.Printf("Shell %s killed\n", args[0])
	return nil
}
