package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/byronellis/ragtime/internal/config"
	"github.com/byronellis/ragtime/internal/daemon"
	"github.com/byronellis/ragtime/internal/hook"
	"github.com/byronellis/ragtime/internal/hook/adapters"
	"github.com/byronellis/ragtime/internal/project"
	"github.com/byronellis/ragtime/internal/protocol"
	"github.com/byronellis/ragtime/internal/rag"
	"github.com/byronellis/ragtime/internal/rag/providers"
	ragstarlark "github.com/byronellis/ragtime/internal/starlark"
	ragtui "github.com/byronellis/ragtime/internal/tui"
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
	cmd.Flags().Bool("test", false, "run locally without a daemon, print human-readable results")
	cmd.Flags().String("tool", "", "tool name for synthetic events (test mode only)")
	cmd.Flags().String("input", "", "JSON object for tool_input (test mode only)")
	cmd.Flags().StringArray("rule", nil, "path to a rule YAML file (repeatable, test mode only)")
	cmd.Flags().Bool("tui", false, "show interactive TUI modals for response.prompt() calls (test mode only)")
	cmd.Flags().Bool("verbose", false, "show detailed rule matching info (test mode only)")

	return cmd
}

func runHook(cmd *cobra.Command, args []string) error {
	testMode, _ := cmd.Flags().GetBool("test")
	if testMode {
		return runHookTest(cmd)
	}
	return runHookLive(cmd)
}

// runHookLive is the normal mode: relay to daemon.
func runHookLive(cmd *cobra.Command) error {
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

	// Debug: log raw stdin to file for troubleshooting
	if f, err := os.OpenFile(filepath.Join(project.GlobalDir(), "hook-stdin.log"),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
		fmt.Fprintf(f, "[%s] agent=%s event=%s stdin=%s\n",
			time.Now().Format(time.RFC3339), agent, eventType, string(stdinData))
		f.Close()
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

	// If running inside a ragtime shell, attach mux info for correlation
	if shellID := os.Getenv("RAGTIME_SHELL_ID"); shellID != "" {
		event.Mux = &protocol.MuxInfo{Type: "ragtime", Pane: shellID}
		if event.Raw == nil {
			event.Raw = make(map[string]any)
		}
		event.Raw["ragtime_shell_id"] = shellID
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

	// Set deadline for the entire exchange.
	// Interactive events (permission-request) may block on TUI modals,
	// so allow a longer deadline for those.
	deadline := 5 * time.Second
	if eventType == "permission-request" || eventType == "pre-tool-use" {
		deadline = 60 * time.Second
	}
	conn.SetDeadline(time.Now().Add(deadline))

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
			os.Stdout.Write([]byte("\n"))
		}
	}

	return nil
}

// runHookTest runs the hook engine locally without a daemon for testing rules.
func runHookTest(cmd *cobra.Command) error {
	agent, _ := cmd.Flags().GetString("agent")
	eventType, _ := cmd.Flags().GetString("event")
	toolName, _ := cmd.Flags().GetString("tool")
	inputJSON, _ := cmd.Flags().GetString("input")
	rulePaths, _ := cmd.Flags().GetStringArray("rule")
	useTUI, _ := cmd.Flags().GetBool("tui")
	verbose, _ := cmd.Flags().GetBool("verbose")

	// Defaults for test mode
	if agent == "" {
		agent = "claude"
	}
	if eventType == "" {
		eventType = "pre-tool-use"
	}

	// Build event from stdin or flags
	event, err := buildTestEvent(cmd, agent, eventType, toolName, inputJSON)
	if err != nil {
		return err
	}

	// Load config
	cfg, err := config.Load(".")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Load rules: from --rule flags if given, otherwise from config dirs
	var rules []config.RuleConfig
	if len(rulePaths) > 0 {
		rules, err = hook.LoadRulesFromFiles(rulePaths...)
		if err != nil {
			return fmt.Errorf("load rule files: %w", err)
		}
	} else {
		rules = loadAllRules(cfg)
	}

	// Set up logger
	logLevel := slog.LevelWarn
	if verbose {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))

	// Create engine
	engine := hook.NewEngine(rules, logger)

	// Wire up RAG if available
	ragEngine := initTestRAG(cfg, logger)
	if ragEngine != nil {
		engine.SetRAG(ragEngine)
	}

	// Wire up Starlark runner with CLI interactor for prompts
	starlarkRunner := ragstarlark.NewRunner(ragEngine, nil, logger)
	if useTUI {
		starlarkRunner.SetInteractor(&ragtui.TestInteractor{})
	} else {
		starlarkRunner.SetInteractor(&cliInteractor{})
	}
	engine.SetScripts(starlarkRunner)

	// Run evaluation
	start := time.Now()
	resp := engine.Evaluate(event)
	elapsed := time.Since(start)

	// Print results
	printTestResults(event, resp, rules, elapsed, verbose)

	// Always show agent-formatted output in test mode
	var agentOutput any
	switch agent {
	case "claude":
		agentOutput = adapters.FormatClaudeResponse(resp, eventType)
	}
	if agentOutput != nil {
		data, _ := json.MarshalIndent(agentOutput, "", "  ")
		if string(data) != "{}" {
			fmt.Printf("\n--- Agent Output (%s) ---\n%s\n", agent, data)
		}
	}

	return nil
}

// buildTestEvent constructs a HookEvent from stdin or flags.
func buildTestEvent(cmd *cobra.Command, agent, eventType, toolName, inputJSON string) (*protocol.HookEvent, error) {
	// If --tool flag is set, prefer synthetic event over stdin
	if toolName != "" {
		event := &protocol.HookEvent{
			Agent:     agent,
			EventType: eventType,
			SessionID: "test",
			ToolName:  toolName,
			CWD:       mustGetwd(),
		}
		if inputJSON != "" {
			var toolInput map[string]any
			if err := json.Unmarshal([]byte(inputJSON), &toolInput); err != nil {
				return nil, fmt.Errorf("parse --input JSON: %w", err)
			}
			event.ToolInput = toolInput
		}
		return event, nil
	}

	// Check if stdin has data (not a terminal)
	stat, _ := os.Stdin.Stat()
	hasPipe := (stat.Mode() & os.ModeCharDevice) == 0

	if hasPipe {
		// Read from stdin — could be raw agent JSON or a HookEvent
		stdinData, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		if len(stdinData) == 0 {
			return nil, fmt.Errorf("stdin is empty; provide event JSON or use --tool flag")
		}

		// Try parsing as agent format first
		switch agent {
		case "claude":
			event, err := adapters.ParseClaudeEvent(stdinData, eventType)
			if err == nil {
				return event, nil
			}
		}

		// Try as a generic HookEvent
		var event protocol.HookEvent
		if err := json.Unmarshal(stdinData, &event); err != nil {
			return nil, fmt.Errorf("parse event JSON: %w", err)
		}
		if event.Agent == "" {
			event.Agent = agent
		}
		if event.EventType == "" {
			event.EventType = eventType
		}
		return &event, nil
	}

	// No stdin and no --tool flag
	return nil, fmt.Errorf("provide event JSON on stdin or use --tool to create a synthetic event")
}

func mustGetwd() string {
	cwd, _ := os.Getwd()
	return cwd
}

// loadAllRules loads rules from config + global + project dirs (mirrors daemon logic).
func loadAllRules(cfg *config.Config) []config.RuleConfig {
	rules := append([]config.RuleConfig{}, cfg.Rules...)

	globalDir := project.GlobalDir()
	if globalDir != "" {
		dirRules, err := hook.LoadRulesFromDirs(filepath.Join(globalDir, "rules"))
		if err == nil {
			rules = append(rules, dirRules...)
		}
	}

	cwd, _ := os.Getwd()
	projDir := project.RagtimeDir(cwd)
	if projDir != "" {
		dirRules, err := hook.LoadRulesFromDirs(filepath.Join(projDir, "rules"))
		if err == nil {
			rules = append(rules, dirRules...)
		}
	}

	return rules
}

// initTestRAG creates a RAG engine for test mode if configured.
func initTestRAG(cfg *config.Config, logger *slog.Logger) hook.RAGSearcher {
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

	provider := providers.NewOllama(cfg.Embeddings.Endpoint, cfg.Embeddings.Model)
	return rag.NewEngine(indexDirs, provider, logger)
}

func printTestResults(event *protocol.HookEvent, resp *protocol.HookResponse, rules []config.RuleConfig, elapsed time.Duration, verbose bool) {
	// Header
	fmt.Printf("=== Hook Test ===\n")
	fmt.Printf("Event:  %s / %s", event.EventType, event.ToolName)
	if path := extractTestPath(event.ToolInput); path != "" {
		fmt.Printf(" (%s)", path)
	}
	fmt.Println()
	fmt.Printf("Agent:  %s\n", event.Agent)
	fmt.Printf("Rules:  %d loaded\n", len(rules))
	fmt.Printf("Time:   %s\n", elapsed.Round(time.Microsecond))
	fmt.Println()

	// Matched rules
	if len(resp.MatchedRules) > 0 {
		fmt.Printf("Matched rules: %s\n", strings.Join(resp.MatchedRules, ", "))
	} else {
		fmt.Println("Matched rules: (none)")
	}

	// Permission decision
	if resp.PermissionDecision != "" {
		fmt.Printf("Permission:    %s", resp.PermissionDecision)
		if resp.DenyReason != "" {
			fmt.Printf(" — %s", resp.DenyReason)
		}
		fmt.Println()
	}

	// Context
	if resp.Context != "" {
		fmt.Println()
		fmt.Println("--- Injected Context ---")
		fmt.Println(resp.Context)
	}

	// Verbose: show all rules and their match status
	if verbose {
		fmt.Println()
		fmt.Println("--- Rule Details ---")
		for _, rule := range rules {
			matched := hook.Match(event, rule.Match)
			status := "SKIP"
			if matched {
				status = "MATCH"
			}
			fmt.Printf("  [%s] %s", status, rule.Name)
			if rule.Match.Event != "" {
				fmt.Printf("  event=%s", rule.Match.Event)
			}
			if rule.Match.Tool != "" {
				fmt.Printf("  tool=%s", rule.Match.Tool)
			}
			if rule.Match.Agent != "" {
				fmt.Printf("  agent=%s", rule.Match.Agent)
			}
			if rule.Match.PathGlob != "" {
				fmt.Printf("  path=%s", rule.Match.PathGlob)
			}
			fmt.Println()
		}
	}
}

func extractTestPath(toolInput map[string]any) string {
	if toolInput == nil {
		return ""
	}
	for _, key := range []string{"file_path", "path", "command"} {
		if v, ok := toolInput[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}

// cliInteractor implements starlark.Interactor for test mode.
// It prints prompts to stderr and reads responses from the terminal.
type cliInteractor struct{}

func (ci *cliInteractor) Prompt(text string, interType protocol.InteractionType, defaultVal string, timeoutSec int) protocol.InteractionResponse {
	fmt.Fprintf(os.Stderr, "\n--- Interaction Prompt ---\n%s\n", text)

	switch interType {
	case protocol.InteractionOKCancel:
		fmt.Fprintf(os.Stderr, "Options: [ok] cancel (default: %s, timeout: %ds)\n", defaultVal, timeoutSec)
	case protocol.InteractionApproveDenyCancel:
		fmt.Fprintf(os.Stderr, "Options: [approve] deny cancel (default: %s, timeout: %ds)\n", defaultVal, timeoutSec)
	case protocol.InteractionFreeform:
		fmt.Fprintf(os.Stderr, "Enter response (default: %s, timeout: %ds)\n", defaultVal, timeoutSec)
	}

	// Check if terminal is interactive
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		// Not a terminal — return default
		fmt.Fprintf(os.Stderr, "  → (non-interactive) using default: %s\n", defaultVal)
		return protocol.InteractionResponse{Value: defaultVal}
	}

	fmt.Fprintf(os.Stderr, "> ")
	var input string
	fmt.Scanln(&input)
	input = strings.TrimSpace(input)
	if input == "" {
		input = defaultVal
	}

	return protocol.InteractionResponse{Value: input}
}
