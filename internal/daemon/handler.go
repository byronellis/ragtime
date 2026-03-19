package daemon

import (
	"fmt"
	"time"

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
