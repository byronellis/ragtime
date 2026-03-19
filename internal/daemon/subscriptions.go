package daemon

import (
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	"github.com/byronellis/ragtime/internal/protocol"
)

// clientState tracks a connected TUI subscriber.
type clientState struct {
	conn    net.Conn
	writeMu sync.Mutex
}

// sseClient is an SSE (web browser) subscriber channel.
type sseClient struct {
	ch chan *protocol.StreamEvent
}

// SubscriptionManager manages connected TUI clients that receive streaming events.
type SubscriptionManager struct {
	mu         sync.RWMutex
	clients    map[net.Conn]*clientState
	sseClients map[int]*sseClient
	sseNextID  int
	daemon     *Daemon
	logger     *slog.Logger
}

// NewSubscriptionManager creates a new subscription manager.
func NewSubscriptionManager(d *Daemon, logger *slog.Logger) *SubscriptionManager {
	return &SubscriptionManager{
		clients:    make(map[net.Conn]*clientState),
		sseClients: make(map[int]*sseClient),
		daemon:     d,
		logger:     logger,
	}
}

// RegisterSSE adds a web browser SSE subscriber and returns (id, channel).
func (sm *SubscriptionManager) RegisterSSE() (int, <-chan *protocol.StreamEvent) {
	ch := make(chan *protocol.StreamEvent, 64)
	sm.mu.Lock()
	id := sm.sseNextID
	sm.sseNextID++
	sm.sseClients[id] = &sseClient{ch: ch}
	sm.mu.Unlock()
	sm.logger.Info("web client connected")
	return id, ch
}

// UnregisterSSE removes a web SSE subscriber.
func (sm *SubscriptionManager) UnregisterSSE(id int) {
	sm.mu.Lock()
	if c, ok := sm.sseClients[id]; ok {
		close(c.ch)
		delete(sm.sseClients, id)
	}
	sm.mu.Unlock()
	sm.logger.Info("web client disconnected")
}

// Snapshot returns the current daemon state for new subscribers.
func (sm *SubscriptionManager) Snapshot() protocol.SubscribeResponse {
	sessions := sm.daemon.sessions.List()
	infos := make([]protocol.SessionInfo, len(sessions))
	for i, s := range sessions {
		infos[i] = protocol.SessionInfo{
			Agent:      s.Agent,
			SessionID:  s.SessionID,
			StartedAt:  s.StartedAt,
			EventCount: s.EventCount(),
		}
		if recent := s.RecentEvents(1); len(recent) > 0 {
			infos[i].LastEvent = recent[0].Timestamp
		}
	}
	return protocol.SubscribeResponse{
		Success: true,
		DaemonInfo: protocol.DaemonInfo{
			PID:        os.Getpid(),
			StartedAt:  sm.daemon.startedAt,
			SocketPath: sm.daemon.cfg.Daemon.Socket,
			RuleCount:  sm.daemon.engine.RuleCount(),
		},
		Sessions: infos,
	}
}

// Register adds a TUI client and returns the initial state snapshot.
func (sm *SubscriptionManager) Register(conn net.Conn) *protocol.SubscribeResponse {
	sm.mu.Lock()
	sm.clients[conn] = &clientState{conn: conn}
	sm.mu.Unlock()

	sm.logger.Info("tui client connected")

	snap := sm.Snapshot()
	return &snap
}

// ClientCount returns the number of connected TUI clients.
func (sm *SubscriptionManager) ClientCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.clients)
}

// Unregister removes a TUI client.
func (sm *SubscriptionManager) Unregister(conn net.Conn) {
	sm.mu.Lock()
	delete(sm.clients, conn)
	sm.mu.Unlock()

	sm.logger.Info("tui client disconnected")
}

// Broadcast sends a stream event to all connected TUI clients.
// Clients that fail to write are automatically removed.
func (sm *SubscriptionManager) Broadcast(event *protocol.StreamEvent) {
	env, err := protocol.NewEnvelope(protocol.MsgEvent, event)
	if err != nil {
		sm.logger.Error("marshal stream event", "error", err)
		return
	}

	sm.mu.RLock()
	clients := make([]*clientState, 0, len(sm.clients))
	for _, cs := range sm.clients {
		clients = append(clients, cs)
	}
	sseClients := make([]*sseClient, 0, len(sm.sseClients))
	for _, sc := range sm.sseClients {
		sseClients = append(sseClients, sc)
	}
	sm.mu.RUnlock()

	// Fan out to SSE web clients (non-blocking — drop if consumer is slow)
	for _, sc := range sseClients {
		select {
		case sc.ch <- event:
		default:
		}
	}

	var failed []net.Conn
	for _, cs := range clients {
		cs.writeMu.Lock()
		cs.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		writeErr := protocol.WriteMessage(cs.conn, env)
		cs.conn.SetWriteDeadline(time.Time{})
		cs.writeMu.Unlock()

		if writeErr != nil {
			sm.logger.Debug("broadcast write failed", "error", writeErr)
			failed = append(failed, cs.conn)
		}
	}

	for _, conn := range failed {
		sm.Unregister(conn)
	}
}
