package mux

import (
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/byronellis/ragtime/internal/db"
	"github.com/google/uuid"
)

// ShellSpec describes the parameters for creating a new shell.
type ShellSpec struct {
	Name    string
	Command []string // if empty, use $SHELL
	CWD     string   // if empty, use current dir
	Env     []string // KEY=VALUE pairs to add
}

// ShellManager manages the lifecycle of shell processes.
type ShellManager struct {
	mu     sync.RWMutex
	shells map[string]*Shell
	db     *db.DB
	logger *slog.Logger
}

// NewShellManager creates a new shell manager.
func NewShellManager(database *db.DB, logger *slog.Logger) *ShellManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &ShellManager{
		shells: make(map[string]*Shell),
		db:     database,
		logger: logger,
	}
}

// New creates and starts a new shell.
func (m *ShellManager) New(spec ShellSpec) (*Shell, error) {
	id := uuid.New().String()[:8]

	cwd := spec.CWD
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	shell := &Shell{
		ID:      id,
		Name:    spec.Name,
		Command: spec.Command,
		CWD:     cwd,
		logger:  m.logger,
		onExit: func(shellID string, exitCode int) {
			m.logger.Info("shell exited", "id", shellID, "exit_code", exitCode)
			if m.db != nil {
				ec := exitCode
				state := "stopped"
				m.db.UpdateShellState(shellID, state, &ec)
			}
		},
	}

	// Add extra env vars
	if len(spec.Env) > 0 {
		shell.cmd = nil // will be set in start()
	}

	if err := shell.start(); err != nil {
		return nil, fmt.Errorf("start shell: %w", err)
	}

	m.mu.Lock()
	m.shells[id] = shell
	m.mu.Unlock()

	// Record in database
	if m.db != nil {
		rec := &db.ShellRecord{
			ID:        id,
			Name:      spec.Name,
			Command:   shell.Command,
			CWD:       cwd,
			State:     "running",
			StartedAt: shell.StartedAt,
		}
		if err := m.db.InsertShell(rec); err != nil {
			m.logger.Error("insert shell record", "error", err)
		}
	}

	m.logger.Info("shell started", "id", id, "command", shell.Command, "pid", shell.cmd.Process.Pid)
	return shell, nil
}

// Get returns a shell by ID.
func (m *ShellManager) Get(id string) *Shell {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.shells[id]
}

// List returns all shells (optionally including stopped ones).
func (m *ShellManager) List(includeStopped bool) []*Shell {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Shell, 0, len(m.shells))
	for _, s := range m.shells {
		if !includeStopped && s.State != ShellRunning {
			continue
		}
		result = append(result, s)
	}
	return result
}

// WriteToShell writes data to a shell's PTY stdin.
func (m *ShellManager) WriteToShell(id string, data []byte) error {
	m.mu.RLock()
	shell, ok := m.shells[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("shell %s not found", id)
	}
	return shell.Write(data)
}

// ListShells returns the IDs of all running shells.
func (m *ShellManager) ListShells() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]string, 0, len(m.shells))
	for id, s := range m.shells {
		if s.State == ShellRunning {
			ids = append(ids, id)
		}
	}
	return ids
}

// Kill sends a signal to a shell.
func (m *ShellManager) Kill(id string, sig os.Signal) error {
	m.mu.RLock()
	shell, ok := m.shells[id]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("shell %s not found", id)
	}
	return shell.Kill(sig)
}
