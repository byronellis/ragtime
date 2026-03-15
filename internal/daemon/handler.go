package daemon

import (
	"fmt"

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

	// Publish to event bus (for session tracking, etc.)
	h.daemon.bus.Publish(&event)

	// Evaluate rules
	resp := h.daemon.engine.Evaluate(&event)

	return protocol.NewEnvelope(protocol.MsgHookResponse, resp)
}

func (h *Handler) handleCommand(env *protocol.Envelope) (*protocol.Envelope, error) {
	var cmd protocol.CommandRequest
	if err := env.DecodePayload(&cmd); err != nil {
		return nil, fmt.Errorf("decode command: %w", err)
	}

	h.daemon.logger.Info("command", "command", cmd.Command)

	resp := &protocol.CommandResponse{
		Success: false,
		Error:   fmt.Sprintf("command %q not yet implemented", cmd.Command),
	}
	return protocol.NewEnvelope(protocol.MsgCommand, resp)
}
