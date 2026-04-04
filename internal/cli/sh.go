package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
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
		Use:   "attach <id-or-name>",
		Short: "Attach to a shell session's PTY",
		Args:  cobra.ExactArgs(1),
		RunE:  runShAttach,
	}
}

func newShSendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "send <id-or-name> <text>",
		Short: "Send text to a shell session",
		Args:  cobra.ExactArgs(2),
		RunE:  runShSend,
	}
	cmd.Flags().Bool("enter", false, "append a newline after the text")
	return cmd
}

func newShCaptureCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "capture <id-or-name>",
		Short: "Capture scrollback from a shell session",
		Args:  cobra.ExactArgs(1),
		RunE:  runShCapture,
	}
}

func newShKillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kill <id-or-name>",
		Short: "Kill a shell session",
		Args:  cobra.ExactArgs(1),
		RunE:  runShKill,
	}
	cmd.Flags().String("signal", "SIGTERM", "signal to send (SIGTERM or SIGKILL)")
	return cmd
}

// resolveShellID looks up a shell by ID or name. Returns the ID and display name.
func resolveShellID(nameOrID string) (id, name string, err error) {
	resp, err := sendCommand("shell-list", map[string]any{"include_stopped": false})
	if err != nil {
		return "", "", fmt.Errorf("list shells: %w", err)
	}

	data, _ := json.Marshal(resp.Data)
	var infos []protocol.ShellInfo
	json.Unmarshal(data, &infos)

	// Exact ID match first
	for _, info := range infos {
		if info.ID == nameOrID {
			n := info.Name
			if n == "" {
				n = info.ID[:8]
			}
			return info.ID, n, nil
		}
	}

	// Name match (case-insensitive prefix)
	var matches []protocol.ShellInfo
	lower := strings.ToLower(nameOrID)
	for _, info := range infos {
		if strings.HasPrefix(strings.ToLower(info.Name), lower) {
			matches = append(matches, info)
		}
	}
	switch len(matches) {
	case 0:
		return "", "", fmt.Errorf("no shell found with ID or name %q", nameOrID)
	case 1:
		n := matches[0].Name
		if n == "" {
			n = matches[0].ID[:8]
		}
		return matches[0].ID, n, nil
	default:
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = fmt.Sprintf("%s (%s)", m.Name, m.ID[:8])
		}
		return "", "", fmt.Errorf("ambiguous name %q matches: %s", nameOrID, strings.Join(names, ", "))
	}
}

func runShNew(cmd *cobra.Command, args []string) error {
	name, _ := cmd.Flags().GetString("name")
	cwd, _ := cmd.Flags().GetString("cwd")
	envVars, _ := cmd.Flags().GetStringArray("env")
	attach, _ := cmd.Flags().GetBool("attach")

	command := args

	reqArgs := map[string]any{"command": command}
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

	data, _ := json.Marshal(resp.Data)
	var info protocol.ShellInfo
	json.Unmarshal(data, &info)

	displayName := info.Name
	if displayName == "" {
		displayName = info.ID[:8]
	}
	fmt.Printf("Shell %s started (pid %d)\n", displayName, info.PID)

	if attach {
		return doShellAttach(info.ID, displayName, info.Command)
	}
	return nil
}

func runShList(cmd *cobra.Command, args []string) error {
	resp, err := sendCommand("shell-list", map[string]any{"include_stopped": false})
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
			info.ID[:8], name, info.State,
			info.StartedAt.Format("2006-01-02 15:04:05"),
			cmdStr,
		)
	}
	return nil
}

func runShAttach(cmd *cobra.Command, args []string) error {
	id, name, err := resolveShellID(args[0])
	if err != nil {
		return err
	}

	// Get full shell info for command display
	resp, _ := sendCommand("shell-list", map[string]any{"include_stopped": false})
	var command []string
	if resp != nil {
		data, _ := json.Marshal(resp.Data)
		var infos []protocol.ShellInfo
		json.Unmarshal(data, &infos)
		for _, info := range infos {
			if info.ID == id {
				command = info.Command
				break
			}
		}
	}

	return doShellAttach(id, name, command)
}

func runShSend(cmd *cobra.Command, args []string) error {
	enter, _ := cmd.Flags().GetBool("enter")

	id, _, err := resolveShellID(args[0])
	if err != nil {
		return err
	}

	_, err = sendCommand("shell-send", map[string]any{
		"id":    id,
		"text":  args[1],
		"enter": enter,
	})
	return err
}

func runShCapture(cmd *cobra.Command, args []string) error {
	id, _, err := resolveShellID(args[0])
	if err != nil {
		return err
	}

	resp, err := sendCommand("shell-capture", map[string]any{"id": id})
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

	id, name, err := resolveShellID(args[0])
	if err != nil {
		return err
	}

	_, err = sendCommand("shell-kill", map[string]any{
		"id":     id,
		"signal": sig,
	})
	if err != nil {
		return err
	}
	fmt.Printf("Shell %s killed\n", name)
	return nil
}

// ---------------------------------------------------------------------------
// Attach implementation
// ---------------------------------------------------------------------------

// doShellAttach connects to the daemon and attaches to a PTY shell session.
// id is the shell ID, displayName and command are for the status bar.
func doShellAttach(id, displayName string, command []string) error {
	socketPath := flagSocket
	if socketPath == "" {
		cfg, err := config.Load(".")
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		socketPath = cfg.Daemon.Socket
	}

	if err := daemon.EnsureRunning(socketPath); err != nil {
		return fmt.Errorf("daemon not running: %w", err)
	}

	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer conn.Close()

	// Get current terminal size; reserve one row for status bar.
	cols, rows := 80, 24
	if w, h, err := term.GetSize(int(os.Stdin.Fd())); err == nil {
		cols, rows = w, h
	}
	ptyRows := rows - 1 // shell PTY is one row shorter

	// Send attach request with PTY size (one row shorter for status bar).
	req := &protocol.ShellAttachRequest{
		ID:   id,
		Cols: uint16(cols),
		Rows: uint16(ptyRows),
	}
	env, err := protocol.NewEnvelope(protocol.MsgShellAttach, req)
	if err != nil {
		return err
	}
	if err := protocol.WriteMessage(conn, env); err != nil {
		return fmt.Errorf("send attach: %w", err)
	}

	// Set terminal to raw mode.
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("raw mode: %w", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	// Set scroll region to rows 1..ptyRows so our status bar on the last line
	// is never scrolled over by the shell.
	fmt.Fprintf(os.Stdout, "\033[1;%dr", ptyRows)
	defer func() {
		// Restore full scroll region, clear status bar line, reset.
		fmt.Fprintf(os.Stdout, "\033[r\033[%d;1H\033[2K\033[?25h", rows)
	}()

	// Draw initial status bar.
	cmdName := "shell"
	if len(command) > 0 {
		cmdName = command[0]
		if i := strings.LastIndex(cmdName, "/"); i >= 0 {
			cmdName = cmdName[i+1:]
		}
	}
	writeStatusBar(os.Stdout, cols, rows, displayName, cmdName)

	// Mutex for conn writes — stdout goroutine and SIGWINCH goroutine both write.
	var connMu sync.Mutex
	writeConn := func(msg any, msgType protocol.MessageType) {
		e, err := protocol.NewEnvelope(msgType, msg)
		if err != nil {
			return
		}
		connMu.Lock()
		defer connMu.Unlock()
		protocol.WriteMessage(conn, e)
	}

	// detachCh is closed when the user requests detach (ctrl+\).
	detachCh := make(chan struct{})
	// outputDone is closed when the conn read goroutine exits (shell exited or conn dropped).
	outputDone := make(chan struct{})

	// Goroutine: conn -> stdout (shell output).
	go func() {
		defer close(outputDone)
		for {
			msg, err := protocol.ReadMessage(conn)
			if err != nil {
				return
			}
			if msg.Type == protocol.MsgShellOutput {
				var out protocol.ShellOutputMessage
				if err := msg.DecodePayload(&out); err == nil {
					os.Stdout.Write(out.Data)
					// Redraw status bar after each output chunk.
					writeStatusBar(os.Stdout, cols, rows, displayName, cmdName)
				}
			}
		}
	}()

	// Goroutine: SIGWINCH -> resize PTY.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH)
	go func() {
		for {
			select {
			case <-sigCh:
				if w, h, err := term.GetSize(int(os.Stdin.Fd())); err == nil {
					cols, rows = w, h
					ptyRows = rows - 1
					fmt.Fprintf(os.Stdout, "\033[1;%dr", ptyRows)
					writeStatusBar(os.Stdout, cols, rows, displayName, cmdName)
					writeConn(&protocol.ShellResizeMessage{
						ID:   id,
						Cols: uint16(cols),
						Rows: uint16(ptyRows),
					}, protocol.MsgShellResize)
				}
			case <-outputDone:
				return
			case <-detachCh:
				return
			}
		}
	}()

	// Goroutine: stdin -> conn (keyboard input).
	// This goroutine is intentionally left running after doShellAttach returns;
	// it will exit on the next write error once conn is closed.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				return
			}
			if n == 0 {
				continue
			}

			data := buf[:n]

			// ctrl+\ (0x1c) detaches.
			hasDetach := false
			for _, b := range data {
				if b == 0x1c {
					hasDetach = true
					break
				}
			}
			if hasDetach {
				close(detachCh)
				return
			}

			writeConn(&protocol.ShellInputMessage{ID: id, Data: data}, protocol.MsgShellInput)
		}
	}()

	// Wait for shell exit or user detach.
	select {
	case <-outputDone:
		fmt.Fprintf(os.Stdout, "\r\n\033[%d;1H\033[2K", rows)
		fmt.Fprintf(os.Stdout, "\r\n[shell exited]\r\n")
	case <-detachCh:
		fmt.Fprintf(os.Stdout, "\r\n\033[%d;1H\033[2K", rows)
		fmt.Fprintf(os.Stdout, "\r\n[detached from %s]\r\n", displayName)
	}

	signal.Stop(sigCh)
	return nil
}

// ---------------------------------------------------------------------------
// Status bar
// ---------------------------------------------------------------------------

// writeStatusBar draws a zellij-style status bar on the last terminal row.
// Uses only standard ANSI 256-color codes; no powerline/nerd fonts needed.
func writeStatusBar(w *os.File, cols, rows int, name, cmdName string) {
	var b bytes.Buffer

	// Save cursor, move to last row.
	fmt.Fprintf(&b, "\033[s\033[%d;1H\033[2K", rows)

	// ── Left section ─────────────────────────────────────────────────────────

	// Brand pill: " rt " white-on-teal.
	b.WriteString("\033[0;38;5;255;48;5;30m rt \033[0m")

	// Divider: teal-on-darkgray arrow shape using half-block ▌
	b.WriteString("\033[38;5;30;48;5;237m\u258c\033[0m")

	// Shell name: bright white on dark gray.
	fmt.Fprintf(&b, "\033[0;38;5;255;48;5;237m  %s  \033[0m", name)

	// Divider bar.
	b.WriteString("\033[38;5;244;48;5;235m\u258c\033[0m")

	// Command name: medium gray on slightly darker gray.
	fmt.Fprintf(&b, "\033[0;38;5;248;48;5;235m  %s  \033[0m", cmdName)

	// ── Right section ────────────────────────────────────────────────────────

	hint := " ctrl+\\ detach "
	// Measure left content: "rt"(3) + "▌"(1) + "  name  "(len+4) + "▌"(1) + "  cmd  "(len+4)
	leftLen := 3 + 1 + len(name) + 4 + 1 + len(cmdName) + 4
	rightStart := cols - len(hint)
	if rightStart > leftLen+1 {
		fmt.Fprintf(&b, "\033[%d;%dH", rows, rightStart)
		b.WriteString("\033[0;38;5;244;48;5;235m")
		b.WriteString(hint)
		b.WriteString("\033[0m")
	}

	// Restore cursor.
	b.WriteString("\033[u")

	w.Write(b.Bytes())
}
