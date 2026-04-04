package mux

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	pty "github.com/creack/pty/v2"

	"github.com/byronellis/ragtime/internal/protocol"
)

// ShellState represents the lifecycle state of a shell process.
type ShellState string

const (
	ShellRunning ShellState = "running"
	ShellStopped ShellState = "stopped"
	ShellKilled  ShellState = "killed"
)

// Shell represents a running process in a PTY.
type Shell struct {
	ID        string
	Name      string
	Command   []string
	CWD       string
	State     ShellState
	StartedAt time.Time
	ExtraEnv  []string // additional KEY=VALUE pairs injected alongside RAGTIME_SHELL_ID

	ptmx       *os.File // PTY master
	cmd        *exec.Cmd
	scrollback *Scrollback

	subMu       sync.Mutex
	subscribers map[string]chan []byte
	subID       atomic.Int64

	logger *slog.Logger
	doneCh chan struct{}

	// Called when shell exits
	onExit func(id string, exitCode int)
}

// start forks the command into a PTY. Called internally by manager.
func (s *Shell) start() error {
	if len(s.Command) == 0 {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
		s.Command = []string{shell}
	}

	s.cmd = exec.Command(s.Command[0], s.Command[1:]...)
	if s.CWD != "" {
		s.cmd.Dir = s.CWD
	}

	// Inherit environment, add RAGTIME_SHELL_ID and any extra vars
	s.cmd.Env = append(os.Environ(), fmt.Sprintf("RAGTIME_SHELL_ID=%s", s.ID))
	s.cmd.Env = append(s.cmd.Env, s.ExtraEnv...)

	// Start with default size
	ws := &pty.Winsize{Rows: 50, Cols: 220}
	ptmx, err := pty.StartWithSize(s.cmd, ws)
	if err != nil {
		return fmt.Errorf("start pty: %w", err)
	}
	s.ptmx = ptmx
	s.State = ShellRunning
	s.StartedAt = time.Now()
	s.doneCh = make(chan struct{})
	s.scrollback = NewScrollback(defaultScrollbackBytes)
	s.subscribers = make(map[string]chan []byte)

	// Read goroutine: PTY output -> scrollback + subscribers
	go s.readLoop()

	// Wait goroutine: process exit detection
	go s.waitLoop()

	return nil
}

func (s *Shell) readLoop() {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])

			s.scrollback.Append(data)

			// Broadcast to subscribers
			s.subMu.Lock()
			for _, ch := range s.subscribers {
				select {
				case ch <- data:
				default:
					// Drop if subscriber is slow
				}
			}
			s.subMu.Unlock()
		}
		if err != nil {
			if err != io.EOF {
				s.logger.Debug("pty read error", "id", s.ID, "error", err)
			}
			return
		}
	}
}

func (s *Shell) waitLoop() {
	exitCode := 0
	err := s.cmd.Wait()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	s.ptmx.Close()

	if s.State == ShellKilled {
		// Already marked as killed
	} else {
		s.State = ShellStopped
	}

	// Close subscriber channels
	s.subMu.Lock()
	for id, ch := range s.subscribers {
		close(ch)
		delete(s.subscribers, id)
	}
	s.subMu.Unlock()

	close(s.doneCh)

	if s.onExit != nil {
		s.onExit(s.ID, exitCode)
	}
}

// Subscribe registers a channel to receive PTY output.
// Returns a channel and unsubscribe function.
func (s *Shell) Subscribe(id string) (<-chan []byte, func()) {
	ch := make(chan []byte, 256)
	s.subMu.Lock()
	s.subscribers[id] = ch
	s.subMu.Unlock()

	unsub := func() {
		s.subMu.Lock()
		if _, ok := s.subscribers[id]; ok {
			delete(s.subscribers, id)
			// Don't close the channel here; the reader should drain it
		}
		s.subMu.Unlock()
	}
	return ch, unsub
}

// Write sends data to the PTY stdin (keyboard input).
func (s *Shell) Write(data []byte) error {
	if s.ptmx == nil {
		return fmt.Errorf("shell not running")
	}
	_, err := s.ptmx.Write(data)
	return err
}

// Resize updates the PTY terminal size.
func (s *Shell) Resize(cols, rows uint16) error {
	if s.ptmx == nil {
		return fmt.Errorf("shell not running")
	}
	return pty.Setsize(s.ptmx, &pty.Winsize{Rows: rows, Cols: cols})
}

// Kill sends a signal to the process.
func (s *Shell) Kill(sig os.Signal) error {
	if s.cmd == nil || s.cmd.Process == nil {
		return fmt.Errorf("no process")
	}
	s.State = ShellKilled
	return s.cmd.Process.Signal(sig)
}

// Scrollback returns the scrollback buffer.
func (s *Shell) Scrollback() *Scrollback {
	return s.scrollback
}

// Info returns a ShellInfo protocol struct.
func (s *Shell) Info() *protocol.ShellInfo {
	info := &protocol.ShellInfo{
		ID:        s.ID,
		Name:      s.Name,
		Command:   s.Command,
		CWD:       s.CWD,
		State:     string(s.State),
		StartedAt: s.StartedAt,
	}
	if s.cmd != nil && s.cmd.Process != nil {
		info.PID = s.cmd.Process.Pid
	}
	return info
}

// Done returns a channel that is closed when the shell exits.
func (s *Shell) Done() <-chan struct{} {
	return s.doneCh
}
