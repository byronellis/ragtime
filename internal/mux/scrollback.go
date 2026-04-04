package mux

import (
	"sync"
	"time"
)

const defaultScrollbackBytes = 1 * 1024 * 1024 // 1MB

// ScrollbackEntry is a timestamped chunk of PTY output.
type ScrollbackEntry struct {
	Ts   time.Time
	Data []byte
}

// Scrollback is a fixed-size ring buffer of PTY output.
type Scrollback struct {
	mu      sync.RWMutex
	entries []ScrollbackEntry
	head    int // next write position
	count   int // number of valid entries
	size    int // current total bytes stored
	maxSize int // max bytes before eviction
}

// NewScrollback creates a scrollback buffer with the given max byte capacity.
func NewScrollback(maxBytes int) *Scrollback {
	if maxBytes <= 0 {
		maxBytes = defaultScrollbackBytes
	}
	return &Scrollback{
		entries: make([]ScrollbackEntry, 0, 1024),
		maxSize: maxBytes,
	}
}

// Append adds a chunk of PTY output to the scrollback.
func (s *Scrollback) Append(data []byte) {
	if len(data) == 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	entry := ScrollbackEntry{
		Ts:   time.Now(),
		Data: make([]byte, len(data)),
	}
	copy(entry.Data, data)

	// Evict oldest entries until we have room
	for s.size+len(data) > s.maxSize && s.count > 0 {
		oldest := s.oldestIndex()
		s.size -= len(s.entries[oldest].Data)
		s.count--
		s.entries[oldest] = ScrollbackEntry{} // help GC
		if s.count == 0 {
			s.head = 0
		}
	}

	// Append or overwrite
	if s.head < len(s.entries) {
		s.entries[s.head] = entry
	} else {
		s.entries = append(s.entries, entry)
	}
	s.head++
	if s.head >= cap(s.entries)*2 {
		// Compact if we've grown too much
		s.compact()
	}
	s.count++
	s.size += len(data)
}

// oldestIndex returns the index of the oldest entry in the ring.
func (s *Scrollback) oldestIndex() int {
	if s.count <= len(s.entries) {
		return s.head - s.count
	}
	return 0
}

// compact rebuilds the entries slice to reclaim memory.
func (s *Scrollback) compact() {
	snap := s.snapshot()
	s.entries = make([]ScrollbackEntry, len(snap), max(len(snap)*2, 1024))
	copy(s.entries, snap)
	s.head = len(snap)
	s.count = len(snap)
}

// snapshot returns all valid entries in order (oldest first), caller must hold lock.
func (s *Scrollback) snapshot() []ScrollbackEntry {
	if s.count == 0 {
		return nil
	}
	start := s.head - s.count
	if start < 0 {
		start = 0
	}
	result := make([]ScrollbackEntry, 0, s.count)
	for i := start; i < s.head && i < len(s.entries); i++ {
		if len(s.entries[i].Data) > 0 {
			result = append(result, s.entries[i])
		}
	}
	return result
}

// Snapshot returns all entries in order (oldest first).
func (s *Scrollback) Snapshot() []ScrollbackEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot()
}

// Since returns entries after the given time.
func (s *Scrollback) Since(t time.Time) []ScrollbackEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	all := s.snapshot()
	for i, e := range all {
		if !e.Ts.Before(t) {
			return all[i:]
		}
	}
	return nil
}

// Bytes returns all scrollback content as a contiguous byte slice.
func (s *Scrollback) Bytes() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries := s.snapshot()
	if len(entries) == 0 {
		return nil
	}

	total := 0
	for _, e := range entries {
		total += len(e.Data)
	}
	buf := make([]byte, 0, total)
	for _, e := range entries {
		buf = append(buf, e.Data...)
	}
	return buf
}
