package daemon

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"

	"github.com/byronellis/ragtime/internal/protocol"
)

// RequestHandler processes incoming messages and returns responses.
type RequestHandler interface {
	Handle(env *protocol.Envelope) (*protocol.Envelope, error)
}

// SocketServer listens on a Unix socket and dispatches requests.
type SocketServer struct {
	path     string
	handler  RequestHandler
	subs     *SubscriptionManager
	logger   *slog.Logger
	listener net.Listener
	wg       sync.WaitGroup
	done     chan struct{}
}

// NewSocketServer creates a new Unix socket server.
func NewSocketServer(path string, handler RequestHandler, subs *SubscriptionManager, logger *slog.Logger) *SocketServer {
	return &SocketServer{
		path:    path,
		handler: handler,
		subs:    subs,
		logger:  logger,
		done:    make(chan struct{}),
	}
}

// Start begins listening on the Unix socket.
func (s *SocketServer) Start() error {
	// Remove stale socket file
	os.Remove(s.path)

	ln, err := net.Listen("unix", s.path)
	if err != nil {
		return err
	}
	// Make socket accessible only to the current user
	os.Chmod(s.path, 0o700)

	s.listener = ln

	s.wg.Add(1)
	go s.acceptLoop()

	return nil
}

// Stop shuts down the socket server and waits for connections to finish.
func (s *SocketServer) Stop() {
	close(s.done)
	s.listener.Close()
	s.wg.Wait()
	os.Remove(s.path)
}

func (s *SocketServer) acceptLoop() {
	defer s.wg.Done()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				s.logger.Error("accept error", "error", err)
				continue
			}
		}

		s.wg.Add(1)
		go s.handleConn(conn)
	}
}

func (s *SocketServer) handleConn(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	env, err := protocol.ReadMessage(conn)
	if err != nil {
		if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			s.logger.Error("read message", "error", err)
		}
		return
	}

	// Streaming subscription — keeps connection open
	if env.Type == protocol.MsgSubscribe {
		s.handleStreamConn(conn, env)
		return
	}

	// Standard request-response
	resp, err := s.handler.Handle(env)
	if err != nil {
		s.logger.Error("handle message", "type", env.Type, "error", err)
		errResp, _ := protocol.NewEnvelope(protocol.MsgHookResponse, &protocol.HookResponse{})
		protocol.WriteMessage(conn, errResp)
		return
	}

	if err := protocol.WriteMessage(conn, resp); err != nil {
		s.logger.Error("write response", "error", err)
	}
}

// handleStreamConn manages a persistent TUI subscription connection.
func (s *SocketServer) handleStreamConn(conn net.Conn, env *protocol.Envelope) {
	// Register and send initial snapshot
	resp := s.subs.Register(conn)
	snapEnv, err := protocol.NewEnvelope(protocol.MsgSubscribe, resp)
	if err != nil {
		s.logger.Error("marshal subscribe response", "error", err)
		return
	}
	if err := protocol.WriteMessage(conn, snapEnv); err != nil {
		s.logger.Error("write subscribe response", "error", err)
		return
	}

	defer s.subs.Unregister(conn)

	// Read loop — blocks until client disconnects.
	// TUI can send MsgCommand while receiving streamed events.
	for {
		msg, err := protocol.ReadMessage(conn)
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
				s.logger.Debug("stream read", "error", err)
			}
			return
		}

		if msg.Type == protocol.MsgCommand {
			cmdResp, err := s.handler.Handle(msg)
			if err != nil {
				s.logger.Error("stream command", "error", err)
				continue
			}
			// Find the client state to use its write mutex
			s.subs.mu.RLock()
			cs := s.subs.clients[conn]
			s.subs.mu.RUnlock()
			if cs != nil {
				cs.writeMu.Lock()
				protocol.WriteMessage(conn, cmdResp)
				cs.writeMu.Unlock()
			}
		}
	}
}
