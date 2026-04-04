package fs

// shellWatcher attaches to a running shell via MsgShellAttach and buffers
// all output so FUSE Read calls can stream it like `tail -f`.

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/byronellis/ragtime/internal/protocol"
)

type shellWatcher struct {
	conn    net.Conn
	mu      sync.Mutex
	cond    *sync.Cond
	buf     []byte // data received but not yet consumed
	offset  int64  // total bytes delivered to readers so far
	closed  bool
}

// newShellWatcher connects to the daemon, sends MsgShellAttach for the given
// shell, and starts streaming output into the internal buffer.
func newShellWatcher(socketPath, shellID string, cols, rows uint16) (*shellWatcher, error) {
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect daemon: %w", err)
	}

	req := &protocol.ShellAttachRequest{
		ID:   shellID,
		Cols: cols,
		Rows: rows,
	}
	env, err := protocol.NewEnvelope(protocol.MsgShellAttach, req)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if err := protocol.WriteMessage(conn, env); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send attach: %w", err)
	}

	w := &shellWatcher{conn: conn}
	w.cond = sync.NewCond(&w.mu)

	go w.readLoop()
	return w, nil
}

// readLoop reads MsgShellOutput frames from the daemon and appends them to buf.
func (w *shellWatcher) readLoop() {
	defer func() {
		w.mu.Lock()
		w.closed = true
		w.cond.Broadcast()
		w.mu.Unlock()
	}()

	for {
		msg, err := protocol.ReadMessage(w.conn)
		if err != nil {
			if err != io.EOF {
				// connection closed by caller or daemon restart — not an error
			}
			return
		}
		if msg.Type != protocol.MsgShellOutput {
			continue
		}
		var out protocol.ShellOutputMessage
		if err := msg.DecodePayload(&out); err != nil || len(out.Data) == 0 {
			continue
		}

		w.mu.Lock()
		w.buf = append(w.buf, out.Data...)
		w.cond.Broadcast()
		w.mu.Unlock()
	}
}

// Read fills buf starting at the logical byte offset, blocking until data is
// available. Returns 0 only when the watcher is closed and all data consumed.
func (w *shellWatcher) Read(buf []byte, offset int64) int {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Wait until there's data at this offset or the stream is closed.
	for {
		have := w.offset + int64(len(w.buf))
		if offset < have {
			break
		}
		if w.closed {
			return 0
		}
		w.cond.Wait()
	}

	// Bytes in buf that are at or past offset.
	skip := int(offset - w.offset)
	available := w.buf[skip:]
	n := copy(buf, available)

	// Advance offset and drain delivered data.
	w.offset += int64(skip + n)
	w.buf = w.buf[skip+n:]

	return n
}

// Close shuts down the watcher and disconnects from the daemon.
func (w *shellWatcher) Close() {
	w.conn.Close() // causes readLoop to exit
	w.mu.Lock()
	w.closed = true
	w.cond.Broadcast()
	w.mu.Unlock()
}
