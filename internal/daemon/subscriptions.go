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

// SubscriptionManager manages connected TUI clients that receive streaming events.
type SubscriptionManager struct {
	mu      sync.RWMutex
	clients map[net.Conn]*clientState
	daemon  *Daemon
	logger  *slog.Logger
}

// NewSubscriptionManager creates a new subscription manager.
func NewSubscriptionManager(d *Daemon, logger *slog.Logger) *SubscriptionManager {
	return &SubscriptionManager{
		clients: make(map[net.Conn]*clientState),
		daemon:  d,
		logger:  logger,
	}
}

// Register adds a TUI client and returns the initial state snapshot.
func (sm *SubscriptionManager) Register(conn net.Conn) *protocol.SubscribeResponse {
	sm.mu.Lock()
	sm.clients[conn] = &clientState{conn: conn}
	sm.mu.Unlock()

	sm.logger.Info("tui client connected")

	// Build snapshot from daemon state
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

	return &protocol.SubscribeResponse{
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
	sm.mu.RUnlock()

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
