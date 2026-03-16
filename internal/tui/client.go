package tui

import (
	"fmt"
	"net"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/byronellis/ragtime/internal/protocol"
)

// EventMsg wraps a stream event as a Bubble Tea message.
type EventMsg struct {
	Event protocol.StreamEvent
}

// ConnectedMsg is sent after the initial subscribe handshake succeeds.
type ConnectedMsg struct {
	Info protocol.SubscribeResponse
}

// DisconnectedMsg is sent when the daemon connection is lost.
type DisconnectedMsg struct {
	Err error
}

// Client manages the connection to the ragtime daemon.
type Client struct {
	conn    net.Conn
	writeMu sync.Mutex
}

// Connect dials the daemon socket and performs the subscribe handshake.
// Returns the client and the initial state snapshot.
func Connect(socketPath string) (*Client, *protocol.SubscribeResponse, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, nil, fmt.Errorf("connect to daemon: %w", err)
	}

	// Send subscribe request
	env, err := protocol.NewEnvelope(protocol.MsgSubscribe, &protocol.SubscribeRequest{})
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("create subscribe request: %w", err)
	}
	if err := protocol.WriteMessage(conn, env); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("send subscribe request: %w", err)
	}

	// Read subscribe response
	respEnv, err := protocol.ReadMessage(conn)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("read subscribe response: %w", err)
	}

	var resp protocol.SubscribeResponse
	if err := respEnv.DecodePayload(&resp); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("decode subscribe response: %w", err)
	}

	if !resp.Success {
		conn.Close()
		return nil, nil, fmt.Errorf("subscribe failed: %s", resp.Error)
	}

	return &Client{conn: conn}, &resp, nil
}

// ReadLoop reads stream events from the daemon and sends them as tea messages.
// It runs until the connection is closed or an error occurs.
func (c *Client) ReadLoop(p *tea.Program) {
	for {
		env, err := protocol.ReadMessage(c.conn)
		if err != nil {
			p.Send(DisconnectedMsg{Err: err})
			return
		}

		switch env.Type {
		case protocol.MsgEvent:
			var event protocol.StreamEvent
			if err := env.DecodePayload(&event); err != nil {
				continue
			}

			// Route interaction requests to the modal
			if event.Kind == "interaction_request" && event.Interaction != nil {
				p.Send(InteractionRequestMsg{Request: *event.Interaction})
				continue
			}

			p.Send(EventMsg{Event: event})
		}
	}
}

// SendInteractionResponse sends the user's interaction response back to the daemon.
func (c *Client) SendInteractionResponse(resp protocol.InteractionResponse) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	env, err := protocol.NewEnvelope(protocol.MsgInteractionResponse, &resp)
	if err != nil {
		return err
	}
	return protocol.WriteMessage(c.conn, env)
}

// Close shuts down the connection.
func (c *Client) Close() error {
	return c.conn.Close()
}
