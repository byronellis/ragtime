package starlark

import (
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/byronellis/ragtime/internal/hook"
	"github.com/byronellis/ragtime/internal/protocol"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// fileOpts enables top-level if/for/while and global reassignment in scripts.
var fileOpts = &syntax.FileOptions{
	TopLevelControl: true,
	GlobalReassign:  true,
}

// TUIState abstracts TUI connectivity checks.
type TUIState interface {
	ClientCount() int
}

// Interactor sends prompts to the TUI and blocks until a response.
type Interactor interface {
	Prompt(text string, interType protocol.InteractionType, defaultVal string, timeoutSec int) protocol.InteractionResponse
}

// ShellWriter can write data to a shell by ID.
type ShellWriter interface {
	WriteToShell(id string, data []byte) error
	ListShells() []string
}

// Runner compiles and executes Starlark scripts for hook actions.
type Runner struct {
	rag        hook.RAGSearcher
	tui        TUIState
	interactor Interactor
	shellMgr   ShellWriter
	logger     *slog.Logger

	mu    sync.RWMutex
	cache map[string]*starlark.Program // keyed by SHA-256 of source
}

// NewRunner creates a Starlark runner with optional RAG and TUI dependencies.
func NewRunner(rag hook.RAGSearcher, tui TUIState, logger *slog.Logger) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{
		rag:    rag,
		tui:    tui,
		logger: logger,
		cache:  make(map[string]*starlark.Program),
	}
}

// SetInteractor connects an interaction manager for TUI prompts.
func (r *Runner) SetInteractor(i Interactor) {
	r.interactor = i
}

// SetShellManager connects a shell manager for Starlark shell.send() support.
func (r *Runner) SetShellManager(sm ShellWriter) {
	r.shellMgr = sm
}

// Execute runs a Starlark script in the context of a hook event and returns the response.
// If script starts with "file://" the source is loaded from disk.
// Script errors are returned as errors, never panics.
func (r *Runner) Execute(script string, event *protocol.HookEvent) (resp *protocol.HookResponse, err error) {
	// Panic recovery as last-resort safety net
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("starlark panic: %v", p)
		}
	}()

	src, filename, err := r.resolveScript(script)
	if err != nil {
		return nil, err
	}

	prog, err := r.compile(src, filename)
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", filename, err)
	}

	// Build predeclared namespace
	respHelper := newResponseHelper(r.interactor, r.shellMgr, event)
	predeclared := r.buildPredeclared(event, respHelper)

	thread := &starlark.Thread{
		Name:  filename,
		Print: func(_ *starlark.Thread, msg string) { r.logger.Info("starlark", "msg", msg) },
	}
	thread.SetMaxExecutionSteps(1_000_000)

	_, err = prog.Init(thread, predeclared)
	if err != nil {
		return nil, fmt.Errorf("execute %s: %w", filename, err)
	}

	return respHelper.toResponse(), nil
}

// ClearCache invalidates all compiled scripts (called on config reload).
func (r *Runner) ClearCache() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache = make(map[string]*starlark.Program)
}

func (r *Runner) resolveScript(script string) (src, filename string, err error) {
	if strings.HasPrefix(script, "file://") {
		path := strings.TrimPrefix(script, "file://")
		data, err := os.ReadFile(path)
		if err != nil {
			return "", "", fmt.Errorf("load script %s: %w", path, err)
		}
		return string(data), path, nil
	}
	return script, "<inline>", nil
}

func (r *Runner) compile(src, filename string) (*starlark.Program, error) {
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(src)))

	r.mu.RLock()
	prog, ok := r.cache[key]
	r.mu.RUnlock()
	if ok {
		return prog, nil
	}

	_, prog, err := starlark.SourceProgramOptions(fileOpts, filename, src, r.predeclaredNames().Has)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	r.cache[key] = prog
	r.mu.Unlock()

	return prog, nil
}

// predeclaredNames returns the set of names that will be available in the script namespace.
// This is needed at compile time so the compiler knows which names are predeclared vs undefined.
func (r *Runner) predeclaredNames() starlark.StringDict {
	return starlark.StringDict{
		"event":        starlark.None,
		"response":     starlark.None,
		"rag":          starlark.None,
		"tui":          starlark.None,
		"shell":        starlark.None,
		"log":          starlark.None,
		"inject_input": starlark.None,
	}
}
