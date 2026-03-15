package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/byronellis/ragtime/internal/config"
)

// Daemon is the central ragtime process.
type Daemon struct {
	cfg    *config.Config
	socket *SocketServer
	logger *slog.Logger
}

// New creates a new Daemon with the given config.
func New(cfg *config.Config) *Daemon {
	return &Daemon{
		cfg:    cfg,
		logger: slog.Default(),
	}
}

// Run starts the daemon and blocks until interrupted.
func (d *Daemon) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Ensure state directory exists
	stateDir := filepath.Dir(d.cfg.Daemon.Socket)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	// Write PID file
	pidPath := filepath.Join(stateDir, "daemon.pid")
	if err := writePIDFile(pidPath); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	defer os.Remove(pidPath)

	// Create and start the request handler
	handler := NewHandler(d)

	// Start socket server
	d.socket = NewSocketServer(d.cfg.Daemon.Socket, handler, d.logger)
	if err := d.socket.Start(); err != nil {
		return fmt.Errorf("start socket: %w", err)
	}
	defer d.socket.Stop()

	d.logger.Info("daemon started",
		"socket", d.cfg.Daemon.Socket,
		"pid", os.Getpid(),
	)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		d.logger.Info("shutting down", "signal", sig)
	case <-ctx.Done():
		d.logger.Info("context cancelled")
	}

	return nil
}
