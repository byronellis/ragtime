package hook

import (
	"log/slog"
	"sync"

	"github.com/byronellis/ragtime/internal/config"
	"github.com/byronellis/ragtime/internal/protocol"
)

// RAGSearcher is the interface the hook engine uses to perform RAG searches.
// Implemented by the RAG engine; nil until RAG is initialized.
type RAGSearcher interface {
	Search(collection, query string, topK int) ([]SearchResult, error)
}

// SearchResult represents a single RAG search result.
type SearchResult struct {
	Content  string  `json:"content"`
	Source   string  `json:"source"`
	Score    float32 `json:"score"`
}

// Engine evaluates hook events against rules and produces responses.
type Engine struct {
	mu     sync.RWMutex
	rules  []config.RuleConfig
	rag    RAGSearcher
	logger *slog.Logger
}

// NewEngine creates a hook engine with the given rules.
func NewEngine(rules []config.RuleConfig, logger *slog.Logger) *Engine {
	return &Engine{
		rules:  rules,
		logger: logger,
	}
}

// SetRAG connects a RAG searcher to the engine.
func (e *Engine) SetRAG(rag RAGSearcher) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rag = rag
}

// SetRules replaces the current rule set (used by hot reload).
func (e *Engine) SetRules(rules []config.RuleConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules = rules
	e.logger.Info("rules reloaded", "count", len(rules))
}

// Rules returns the current rule set.
func (e *Engine) Rules() []config.RuleConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.rules
}

// RuleCount returns the number of loaded rules.
func (e *Engine) RuleCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.rules)
}

// Evaluate processes a hook event against all rules and returns an aggregated response.
func (e *Engine) Evaluate(event *protocol.HookEvent) *protocol.HookResponse {
	e.mu.RLock()
	rules := e.rules
	rag := e.rag
	e.mu.RUnlock()

	resp := &protocol.HookResponse{}
	var contextParts []string

	for _, rule := range rules {
		if !Match(event, rule.Match) {
			continue
		}

		e.logger.Debug("rule matched", "rule", rule.Name, "event", event.EventType)

		for _, action := range rule.Actions {
			switch action.Type {
			case "inject-context":
				if action.Content != "" {
					contextParts = append(contextParts, action.Content)
				}

			case "approve":
				resp.PermissionDecision = protocol.PermAllow

			case "deny":
				resp.PermissionDecision = protocol.PermDeny
				resp.DenyReason = action.Reason

			case "rag-search":
				if rag == nil {
					e.logger.Warn("rag-search action but no RAG engine configured", "rule", rule.Name)
					continue
				}
				query := extractQueryFromEvent(event, action.QueryFrom)
				if query == "" {
					continue
				}
				topK := action.TopK
				if topK <= 0 {
					topK = 5
				}
				for _, collection := range action.Collections {
					results, err := rag.Search(collection, query, topK)
					if err != nil {
						e.logger.Error("rag search failed", "collection", collection, "error", err)
						continue
					}
					for _, r := range results {
						contextParts = append(contextParts, r.Content)
					}
				}

			case "log":
				e.logger.Info("hook event (log action)",
					"rule", rule.Name,
					"agent", event.Agent,
					"event", event.EventType,
					"tool", event.ToolName,
				)
			}
		}

		// First approve/deny wins for permission decisions
		if resp.PermissionDecision != "" {
			break
		}
	}

	if len(contextParts) > 0 {
		for i, part := range contextParts {
			if i > 0 {
				resp.Context += "\n\n---\n\n"
			}
			resp.Context += part
		}
	}

	return resp
}

// extractQueryFromEvent extracts a search query from the event based on the query_from config.
func extractQueryFromEvent(event *protocol.HookEvent, queryFrom string) string {
	if queryFrom == "" {
		// Default: use tool name + path as query
		path := extractPath(event)
		if path != "" {
			return event.ToolName + " " + path
		}
		return event.ToolName
	}

	// Support dotted paths like "tool_input.file_path"
	parts := splitDotPath(queryFrom)
	var current any = map[string]any{
		"tool_name":  event.ToolName,
		"tool_input": event.ToolInput,
		"agent":      event.Agent,
		"event_type": event.EventType,
	}

	for _, key := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = m[key]
	}

	if s, ok := current.(string); ok {
		return s
	}
	return ""
}

func splitDotPath(path string) []string {
	var parts []string
	current := ""
	for _, c := range path {
		if c == '.' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}
