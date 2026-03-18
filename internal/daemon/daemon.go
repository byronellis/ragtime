package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/byronellis/ragtime/internal/bus"
	"github.com/byronellis/ragtime/internal/config"
	"github.com/byronellis/ragtime/internal/hook"
	"github.com/byronellis/ragtime/internal/project"
	"github.com/byronellis/ragtime/internal/protocol"
	"github.com/byronellis/ragtime/internal/rag"
	"github.com/byronellis/ragtime/internal/rag/providers"
	"github.com/byronellis/ragtime/internal/session"
	ragstarlark "github.com/byronellis/ragtime/internal/starlark"
)

// Daemon is the central ragtime process.
type Daemon struct {
	cfg       *config.Config
	socket    *SocketServer
	engine    *hook.Engine
	sessions  *session.Manager
	indexer   *session.SessionIndexer
	rag       *rag.Engine
	bus       *bus.Bus
	subs         *SubscriptionManager
	interactions *InteractionManager
	startedAt    time.Time
	logger       *slog.Logger
}

// New creates a new Daemon with the given config.
func New(cfg *config.Config) *Daemon {
	logger := slog.Default()
	return &Daemon{
		cfg:      cfg,
		engine:   hook.NewEngine(nil, logger),
		sessions: session.NewManager(logger),
		bus:      bus.New(),
		logger:   logger,
	}
}

// Run starts the daemon and blocks until interrupted.
func (d *Daemon) Run(ctx context.Context) error {
	d.startedAt = time.Now()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Ensure state directory exists
	stateDir := filepath.Dir(d.cfg.Daemon.Socket)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	// Acquire exclusive lock to prevent multiple daemons
	lockPath := filepath.Join(stateDir, "daemon.lock")
	lock, err := acquireLock(lockPath)
	if err != nil {
		return err
	}
	defer lock.release()

	// Write PID file
	pidPath := filepath.Join(stateDir, "daemon.pid")
	if err := writePIDFile(pidPath); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}
	defer os.Remove(pidPath)

	// Load rules and initialize engine
	rules := d.loadAllRules()
	d.engine = hook.NewEngine(rules, d.logger)

	// Initialize RAG engine and connect to hook engine
	ragEngine := d.initRAG()
	d.rag = ragEngine
	if ragEngine != nil {
		d.engine.SetRAG(ragEngine)
	}

	// Start session indexer for background RAG indexing
	indexerProvider := providers.NewOllama(d.cfg.Embeddings.Endpoint, d.cfg.Embeddings.Model)
	d.indexer = session.NewSessionIndexer(indexerProvider, d.logger)
	d.indexer.Start()
	defer d.indexer.Stop()

	// Subscribe to bus for session indexing
	d.bus.Subscribe(func(event *protocol.HookEvent) {
		if event.SessionID == "" {
			return
		}
		sess := d.sessions.Get(event.Agent, event.SessionID)
		if sess == nil {
			return
		}
		d.indexer.OnEvent(event, sess)
	})

	// Start config watcher for hot reload
	watcher, err := d.startWatcher()
	if err != nil {
		d.logger.Warn("hot reload disabled", "error", err)
	} else if watcher != nil {
		defer watcher.Stop()
	}

	// Create subscription manager for TUI streaming
	d.subs = NewSubscriptionManager(d, d.logger)
	d.interactions = NewInteractionManager(d.subs, d.logger)

	// Initialize Starlark runner with RAG, TUI state, and interactions
	starlarkRunner := ragstarlark.NewRunner(ragEngine, d.subs, d.logger)
	starlarkRunner.SetInteractor(d.interactions)
	d.engine.SetScripts(starlarkRunner)

	// Subscribe to bus for TUI broadcasting
	d.bus.Subscribe(func(event *protocol.HookEvent) {
		d.subs.Broadcast(&protocol.StreamEvent{
			Kind:      "hook_event",
			Timestamp: time.Now(),
			Event:     event,
		})
	})

	// Create and start the request handler
	handler := NewHandler(d)

	// Start socket server
	d.socket = NewSocketServer(d.cfg.Daemon.Socket, handler, d.subs, d.logger)
	if err := d.socket.Start(); err != nil {
		return fmt.Errorf("start socket: %w", err)
	}
	defer d.socket.Stop()

	d.logger.Info("daemon started",
		"socket", d.cfg.Daemon.Socket,
		"pid", os.Getpid(),
		"rules", len(rules),
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

func (d *Daemon) loadAllRules() []config.RuleConfig {
	// Start with rules from config
	rules := append([]config.RuleConfig{}, d.cfg.Rules...)

	// Load from global rules dir
	globalDir := project.GlobalDir()
	if globalDir != "" {
		dirRules, err := hook.LoadRulesFromDirs(filepath.Join(globalDir, "rules"))
		if err != nil {
			d.logger.Error("load global rules", "error", err)
		} else {
			rules = append(rules, dirRules...)
		}
	}

	// Load from per-project rules dir
	cwd, _ := os.Getwd()
	projDir := project.RagtimeDir(cwd)
	if projDir != "" {
		dirRules, err := hook.LoadRulesFromDirs(filepath.Join(projDir, "rules"))
		if err != nil {
			d.logger.Error("load project rules", "error", err)
		} else {
			rules = append(rules, dirRules...)
		}
	}

	return rules
}

func (d *Daemon) initRAG() *rag.Engine {
	var indexDirs []string

	cwd, _ := os.Getwd()
	projDir := project.RagtimeDir(cwd)
	if projDir != "" {
		indexDirs = append(indexDirs, filepath.Join(projDir, "indexes"))
	}

	globalDir := project.GlobalDir()
	if globalDir != "" {
		indexDirs = append(indexDirs, filepath.Join(globalDir, "indexes"))
	}

	if len(indexDirs) == 0 {
		return nil
	}

	provider := providers.NewOllama(d.cfg.Embeddings.Endpoint, d.cfg.Embeddings.Model)
	return rag.NewEngine(indexDirs, provider, d.logger)
}

func (d *Daemon) startWatcher() (*config.Watcher, error) {
	w, err := config.NewWatcher(func(paths []string) {
		rules := d.loadAllRules()
		d.engine.SetRules(rules)
	}, d.logger)
	if err != nil {
		return nil, err
	}

	globalDir := project.GlobalDir()
	if globalDir != "" {
		w.Watch(globalDir, filepath.Join(globalDir, "rules"))
	}

	cwd, _ := os.Getwd()
	projDir := project.RagtimeDir(cwd)
	if projDir != "" {
		w.Watch(projDir, filepath.Join(projDir, "rules"))
	}

	w.Start()
	return w, nil
}
