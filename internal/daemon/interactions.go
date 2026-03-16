package daemon

import (
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/byronellis/ragtime/internal/protocol"
)

type pendingInteraction struct {
	request  protocol.InteractionRequest
	respChan chan protocol.InteractionResponse
	timer    *time.Timer
}

// InteractionManager coordinates TUI prompts between Starlark scripts and TUI clients.
type InteractionManager struct {
	mu      sync.Mutex
	pending map[string]*pendingInteraction // keyed by request ID
	queue   []*pendingInteraction          // waiting for TUI to connect
	subs    *SubscriptionManager
	counter atomic.Int64
	logger  *slog.Logger
}

// NewInteractionManager creates a new interaction manager.
func NewInteractionManager(subs *SubscriptionManager, logger *slog.Logger) *InteractionManager {
	return &InteractionManager{
		pending: make(map[string]*pendingInteraction),
		subs:    subs,
		logger:  logger,
	}
}

// Prompt sends an interaction request to the TUI and blocks until a response
// is received or the timeout expires. Called from Starlark goroutines.
func (im *InteractionManager) Prompt(text string, interType protocol.InteractionType, defaultVal string, timeoutSec int) protocol.InteractionResponse {
	if timeoutSec <= 0 {
		timeoutSec = 30
	}

	id := fmt.Sprintf("int-%d", im.counter.Add(1))
	req := protocol.InteractionRequest{
		ID:         id,
		Text:       text,
		Type:       interType,
		Default:    defaultVal,
		TimeoutSec: timeoutSec,
	}

	pi := &pendingInteraction{
		request:  req,
		respChan: make(chan protocol.InteractionResponse, 1),
	}

	im.mu.Lock()
	im.pending[id] = pi

	if im.subs.ClientCount() > 0 {
		im.mu.Unlock()
		im.dispatch(pi)
	} else {
		// No TUI connected — queue it and start a timeout for the full duration
		im.queue = append(im.queue, pi)
		im.mu.Unlock()
		im.startTimeout(pi)
		im.logger.Debug("interaction queued (no TUI)", "id", id)
	}

	// Block until response
	resp := <-pi.respChan

	im.mu.Lock()
	delete(im.pending, id)
	im.mu.Unlock()

	return resp
}

// HandleResponse routes a TUI response back to the waiting Starlark goroutine.
func (im *InteractionManager) HandleResponse(resp protocol.InteractionResponse) {
	im.mu.Lock()
	pi, ok := im.pending[resp.ID]
	im.mu.Unlock()

	if !ok {
		im.logger.Debug("interaction response for unknown ID", "id", resp.ID)
		return
	}

	if pi.timer != nil {
		pi.timer.Stop()
	}

	select {
	case pi.respChan <- resp:
	default:
		// Channel already has a response (timeout raced with user response)
	}
}

// DrainQueue dispatches any queued interactions to newly connected TUI clients.
// Called when a TUI client connects.
func (im *InteractionManager) DrainQueue() {
	im.mu.Lock()
	queued := im.queue
	im.queue = nil
	im.mu.Unlock()

	for _, pi := range queued {
		// Stop the existing timeout and restart with a fresh one on dispatch
		if pi.timer != nil {
			pi.timer.Stop()
		}
		im.dispatch(pi)
	}
}

func (im *InteractionManager) dispatch(pi *pendingInteraction) {
	im.logger.Debug("dispatching interaction to TUI", "id", pi.request.ID)

	im.subs.Broadcast(&protocol.StreamEvent{
		Kind:        "interaction_request",
		Timestamp:   time.Now(),
		Interaction: &pi.request,
	})

	// Timer starts now — when the TUI actually receives the request
	im.startTimeout(pi)
}

func (im *InteractionManager) startTimeout(pi *pendingInteraction) {
	pi.timer = time.AfterFunc(time.Duration(pi.request.TimeoutSec)*time.Second, func() {
		im.logger.Debug("interaction timed out", "id", pi.request.ID, "default", pi.request.Default)
		select {
		case pi.respChan <- protocol.InteractionResponse{
			ID:       pi.request.ID,
			Value:    pi.request.Default,
			TimedOut: true,
		}:
		default:
		}
	})
}

// CancelPending cancels all pending interactions with the default response.
// Called when a TUI client disconnects.
func (im *InteractionManager) CancelPending() {
	im.mu.Lock()
	pending := make([]*pendingInteraction, 0, len(im.pending))
	for _, pi := range im.pending {
		pending = append(pending, pi)
	}
	im.mu.Unlock()

	for _, pi := range pending {
		if pi.timer != nil {
			pi.timer.Stop()
		}
		select {
		case pi.respChan <- protocol.InteractionResponse{
			ID:       pi.request.ID,
			Value:    pi.request.Default,
			TimedOut: true,
		}:
		default:
		}
	}
}
