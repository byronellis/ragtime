package daemon

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/byronellis/ragtime/internal/db"
	"github.com/byronellis/ragtime/internal/protocol"
)

// Handler dispatches incoming messages to the appropriate subsystem.
type Handler struct {
	daemon *Daemon
}

// NewHandler creates a new request handler.
func NewHandler(d *Daemon) *Handler {
	return &Handler{daemon: d}
}

// Handle processes an incoming envelope and returns a response.
func (h *Handler) Handle(env *protocol.Envelope) (*protocol.Envelope, error) {
	switch env.Type {
	case protocol.MsgHookEvent:
		return h.handleHookEvent(env)
	case protocol.MsgStatuslineEvent:
		return h.handleStatuslineEvent(env)
	case protocol.MsgCommand:
		return h.handleCommand(env)
	default:
		return nil, fmt.Errorf("unknown message type: %s", env.Type)
	}
}

func (h *Handler) handleHookEvent(env *protocol.Envelope) (*protocol.Envelope, error) {
	var event protocol.HookEvent
	if err := env.DecodePayload(&event); err != nil {
		return nil, fmt.Errorf("decode hook event: %w", err)
	}

	// Publish to event bus
	h.daemon.bus.Publish(&event)

	// Evaluate rules
	resp := h.daemon.engine.Evaluate(&event)

	// Process through session manager (dedup, history tracking)
	resp = h.daemon.sessions.ProcessEvent(&event, resp)

	// Broadcast session update to TUI subscribers
	if event.SessionID != "" && h.daemon.subs != nil {
		sess := h.daemon.sessions.Get(event.Agent, event.SessionID)
		if sess != nil {
			info := protocol.SessionInfo{
				Agent:      sess.Agent,
				SessionID:  sess.SessionID,
				StartedAt:  sess.StartedAt,
				EventCount: sess.EventCount(),
			}
			if recent := sess.RecentEvents(1); len(recent) > 0 {
				info.LastEvent = recent[0].Timestamp
			}
			h.daemon.subs.Broadcast(&protocol.StreamEvent{
				Kind:      "session_update",
				Timestamp: time.Now(),
				Session:   &info,
			})
		}
	}

	return protocol.NewEnvelope(protocol.MsgHookResponse, resp)
}

func (h *Handler) handleCommand(env *protocol.Envelope) (*protocol.Envelope, error) {
	var cmd protocol.CommandRequest
	if err := env.DecodePayload(&cmd); err != nil {
		return nil, fmt.Errorf("decode command: %w", err)
	}

	h.daemon.logger.Info("command", "command", cmd.Command)

	switch cmd.Command {
	case "search":
		return h.handleSearch(cmd.Args)
	case "sessions":
		return h.handleSessions()
	case "session-history":
		return h.handleSessionHistory(cmd.Args)
	case "statusline-query":
		return h.handleStatuslineQuery(cmd.Args)
	case "cost-summary":
		return h.handleCostSummary(cmd.Args)
	case "shell-new":
		return h.handleShellNew(cmd.Args)
	case "shell-list":
		return h.handleShellList(cmd.Args)
	case "shell-kill":
		return h.handleShellKill(cmd.Args)
	case "shell-send":
		return h.handleShellSend(cmd.Args)
	case "shell-capture":
		return h.handleShellCapture(cmd.Args)
	default:
		resp := &protocol.CommandResponse{
			Success: false,
			Error:   fmt.Sprintf("command %q not yet implemented", cmd.Command),
		}
		return protocol.NewEnvelope(protocol.MsgCommand, resp)
	}
}

func (h *Handler) handleSessions() (*protocol.Envelope, error) {
	sessions := h.daemon.sessions.List()
	var infos []map[string]any
	for _, s := range sessions {
		info := map[string]any{
			"agent":       s.Agent,
			"session_id":  s.SessionID,
			"started_at":  s.StartedAt,
			"event_count": s.EventCount(),
		}
		if recent := s.RecentEvents(1); len(recent) > 0 {
			info["last_event"] = recent[0].Timestamp
		}
		infos = append(infos, info)
	}
	resp := &protocol.CommandResponse{Success: true, Data: infos}
	return protocol.NewEnvelope(protocol.MsgCommand, resp)
}

func (h *Handler) handleSessionHistory(args map[string]any) (*protocol.Envelope, error) {
	agent, _ := args["agent"].(string)
	sessionID, _ := args["session_id"].(string)
	if agent == "" || sessionID == "" {
		resp := &protocol.CommandResponse{Success: false, Error: "agent and session_id are required"}
		return protocol.NewEnvelope(protocol.MsgCommand, resp)
	}

	sess := h.daemon.sessions.Get(agent, sessionID)
	if sess == nil {
		resp := &protocol.CommandResponse{Success: false, Error: "session not found"}
		return protocol.NewEnvelope(protocol.MsgCommand, resp)
	}

	last := 20
	if v, ok := args["last"].(float64); ok && v > 0 {
		last = int(v)
	}

	events := sess.RecentEvents(last)
	resp := &protocol.CommandResponse{Success: true, Data: events}
	return protocol.NewEnvelope(protocol.MsgCommand, resp)
}

func (h *Handler) handleSearch(args map[string]any) (*protocol.Envelope, error) {
	if h.daemon.rag == nil {
		resp := &protocol.CommandResponse{Success: false, Error: "RAG engine not available"}
		return protocol.NewEnvelope(protocol.MsgCommand, resp)
	}

	collection, _ := args["collection"].(string)
	query, _ := args["query"].(string)
	if collection == "" {
		collection = "sessions"
	}
	if query == "" {
		resp := &protocol.CommandResponse{Success: false, Error: "query is required"}
		return protocol.NewEnvelope(protocol.MsgCommand, resp)
	}

	topK := 10
	if v, ok := args["top_k"].(float64); ok && v > 0 {
		topK = int(v)
	}

	results, err := h.daemon.rag.Search(collection, query, topK)
	if err != nil {
		resp := &protocol.CommandResponse{Success: false, Error: err.Error()}
		return protocol.NewEnvelope(protocol.MsgCommand, resp)
	}

	resp := &protocol.CommandResponse{Success: true, Data: results}
	return protocol.NewEnvelope(protocol.MsgCommand, resp)
}

func (h *Handler) handleStatuslineEvent(env *protocol.Envelope) (*protocol.Envelope, error) {
	var e protocol.StatuslineEvent
	if err := env.DecodePayload(&e); err != nil {
		return nil, fmt.Errorf("decode statusline event: %w", err)
	}

	// Store in database
	if h.daemon.db != nil {
		rawJSON, _ := json.Marshal(e)
		rec := &db.StatuslineRecord{
			Ts:             time.Now(),
			SessionID:      e.SessionID,
			Agent:          e.Agent,
			Model:          e.Model,
			NumTurns:       e.NumTurns,
			CostUSD:        e.CostUSD,
			InputTokens:    e.InputTokens,
			OutputTokens:   e.OutputTokens,
			CacheCreateTok: e.CacheCreateTokens,
			CacheReadTok:   e.CacheReadTokens,
			CWD:            e.CWD,
			RawJSON:        string(rawJSON),
		}
		if err := h.daemon.db.InsertStatusline(rec); err != nil {
			h.daemon.logger.Error("insert statusline", "error", err)
		}
	}

	// Create a HookEvent for the bus so Starlark rules and TUI see it
	raw := map[string]any{
		"model":         e.Model,
		"num_turns":     e.NumTurns,
		"cost_usd":      e.CostUSD,
		"input_tokens":  e.InputTokens,
		"output_tokens": e.OutputTokens,
	}
	if e.ShellID != "" {
		raw["ragtime_shell_id"] = e.ShellID
	}
	hookEvt := &protocol.HookEvent{
		EventType: "statusline",
		SessionID: e.SessionID,
		Agent:     e.Agent,
		CWD:       e.CWD,
		Raw:       raw,
	}
	if e.ShellID != "" {
		hookEvt.Mux = &protocol.MuxInfo{Type: "ragtime", Pane: e.ShellID}
	}
	h.daemon.bus.Publish(hookEvt)

	// Also broadcast statusline-specific stream event
	if h.daemon.subs != nil {
		h.daemon.subs.Broadcast(&protocol.StreamEvent{
			Kind:       "statusline",
			Timestamp:  time.Now(),
			Statusline: &e,
		})
	}

	resp := &protocol.CommandResponse{Success: true}
	return protocol.NewEnvelope(protocol.MsgStatuslineEvent, resp)
}

func (h *Handler) handleStatuslineQuery(args map[string]any) (*protocol.Envelope, error) {
	if h.daemon.db == nil {
		resp := &protocol.CommandResponse{Success: false, Error: "database not available"}
		return protocol.NewEnvelope(protocol.MsgCommand, resp)
	}

	sessionID, _ := args["session"].(string)
	limit := 100
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}

	var since time.Time
	if v, ok := args["since"].(string); ok && v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			since = t
		}
	}

	records, err := h.daemon.db.QueryStatusline(sessionID, since, limit)
	if err != nil {
		resp := &protocol.CommandResponse{Success: false, Error: err.Error()}
		return protocol.NewEnvelope(protocol.MsgCommand, resp)
	}

	resp := &protocol.CommandResponse{Success: true, Data: records}
	return protocol.NewEnvelope(protocol.MsgCommand, resp)
}

func (h *Handler) handleCostSummary(args map[string]any) (*protocol.Envelope, error) {
	if h.daemon.db == nil {
		resp := &protocol.CommandResponse{Success: false, Error: "database not available"}
		return protocol.NewEnvelope(protocol.MsgCommand, resp)
	}

	since := time.Now().Add(-24 * time.Hour) // default: last 24h
	if v, ok := args["since"].(string); ok && v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			since = t
		}
	}

	summary, err := h.daemon.db.QueryStatuslineSummary(since)
	if err != nil {
		resp := &protocol.CommandResponse{Success: false, Error: err.Error()}
		return protocol.NewEnvelope(protocol.MsgCommand, resp)
	}

	resp := &protocol.CommandResponse{Success: true, Data: summary}
	return protocol.NewEnvelope(protocol.MsgCommand, resp)
}

func (h *Handler) handleShellNew(args map[string]any) (*protocol.Envelope, error) {
	if h.daemon.shellMgr == nil {
		resp := &protocol.CommandResponse{Success: false, Error: "shell manager not available"}
		return protocol.NewEnvelope(protocol.MsgCommand, resp)
	}

	// Parse args from the map
	data, _ := json.Marshal(args)
	var req protocol.ShellNewRequest
	json.Unmarshal(data, &req)

	shell, err := h.daemon.shellMgr.New(ShellSpecFromRequest(req))
	if err != nil {
		resp := &protocol.CommandResponse{Success: false, Error: err.Error()}
		return protocol.NewEnvelope(protocol.MsgCommand, resp)
	}

	info := shell.Info()

	// Broadcast shell update
	if h.daemon.subs != nil {
		h.daemon.subs.Broadcast(&protocol.StreamEvent{
			Kind:      "shell_update",
			Timestamp: time.Now(),
			Shell:     info,
		})
	}

	resp := &protocol.CommandResponse{Success: true, Data: info}
	return protocol.NewEnvelope(protocol.MsgCommand, resp)
}

func (h *Handler) handleShellList(args map[string]any) (*protocol.Envelope, error) {
	if h.daemon.shellMgr == nil {
		resp := &protocol.CommandResponse{Success: true, Data: []any{}}
		return protocol.NewEnvelope(protocol.MsgCommand, resp)
	}

	includeStopped := false
	if v, ok := args["include_stopped"].(bool); ok {
		includeStopped = v
	}

	shells := h.daemon.shellMgr.List(includeStopped)
	infos := make([]*protocol.ShellInfo, len(shells))
	for i, s := range shells {
		infos[i] = s.Info()
	}

	resp := &protocol.CommandResponse{Success: true, Data: infos}
	return protocol.NewEnvelope(protocol.MsgCommand, resp)
}

func (h *Handler) handleShellKill(args map[string]any) (*protocol.Envelope, error) {
	if h.daemon.shellMgr == nil {
		resp := &protocol.CommandResponse{Success: false, Error: "shell manager not available"}
		return protocol.NewEnvelope(protocol.MsgCommand, resp)
	}

	data, _ := json.Marshal(args)
	var req protocol.ShellKillRequest
	json.Unmarshal(data, &req)

	if req.ID == "" {
		resp := &protocol.CommandResponse{Success: false, Error: "shell id is required"}
		return protocol.NewEnvelope(protocol.MsgCommand, resp)
	}

	sig := sigFromName(req.Signal)
	if err := h.daemon.shellMgr.Kill(req.ID, sig); err != nil {
		resp := &protocol.CommandResponse{Success: false, Error: err.Error()}
		return protocol.NewEnvelope(protocol.MsgCommand, resp)
	}

	resp := &protocol.CommandResponse{Success: true}
	return protocol.NewEnvelope(protocol.MsgCommand, resp)
}

func (h *Handler) handleShellSend(args map[string]any) (*protocol.Envelope, error) {
	if h.daemon.shellMgr == nil {
		resp := &protocol.CommandResponse{Success: false, Error: "shell manager not available"}
		return protocol.NewEnvelope(protocol.MsgCommand, resp)
	}

	data, _ := json.Marshal(args)
	var req protocol.ShellSendRequest
	json.Unmarshal(data, &req)

	if req.ID == "" {
		resp := &protocol.CommandResponse{Success: false, Error: "shell id is required"}
		return protocol.NewEnvelope(protocol.MsgCommand, resp)
	}

	shell := h.daemon.shellMgr.Get(req.ID)
	if shell == nil {
		resp := &protocol.CommandResponse{Success: false, Error: "shell not found"}
		return protocol.NewEnvelope(protocol.MsgCommand, resp)
	}

	text := req.Text
	if req.Enter {
		text += "\n"
	}
	if err := shell.Write([]byte(text)); err != nil {
		resp := &protocol.CommandResponse{Success: false, Error: err.Error()}
		return protocol.NewEnvelope(protocol.MsgCommand, resp)
	}

	resp := &protocol.CommandResponse{Success: true}
	return protocol.NewEnvelope(protocol.MsgCommand, resp)
}

func (h *Handler) handleShellCapture(args map[string]any) (*protocol.Envelope, error) {
	if h.daemon.shellMgr == nil {
		resp := &protocol.CommandResponse{Success: false, Error: "shell manager not available"}
		return protocol.NewEnvelope(protocol.MsgCommand, resp)
	}

	id, _ := args["id"].(string)
	if id == "" {
		resp := &protocol.CommandResponse{Success: false, Error: "shell id is required"}
		return protocol.NewEnvelope(protocol.MsgCommand, resp)
	}

	shell := h.daemon.shellMgr.Get(id)
	if shell == nil {
		resp := &protocol.CommandResponse{Success: false, Error: "shell not found"}
		return protocol.NewEnvelope(protocol.MsgCommand, resp)
	}

	scrollback := shell.Scrollback().Bytes()
	resp := &protocol.CommandResponse{Success: true, Data: string(scrollback)}
	return protocol.NewEnvelope(protocol.MsgCommand, resp)
}
