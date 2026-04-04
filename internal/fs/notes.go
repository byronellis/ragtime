package fs

// notesStore manages in-memory file buffers for agents/<id>/notes/ directories.
// On file close, content is indexed into a per-shell RAG collection.

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/byronellis/ragtime/internal/rag"
)

type notesStore struct {
	mu      sync.Mutex
	// shellID -> filename -> content
	files   map[string]map[string]noteFile
}

type noteFile struct {
	content []byte
	mtime   time.Time
}

func newNotesStore() *notesStore {
	return &notesStore{files: make(map[string]map[string]noteFile)}
}

// list returns the filenames stored for a shell.
func (s *notesStore) list(shellID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]string, 0, len(s.files[shellID]))
	for name := range s.files[shellID] {
		names = append(names, name)
	}
	return names
}

// get returns the content of a note file. Returns nil if not found.
func (s *notesStore) get(shellID, name string) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.files[shellID]; ok {
		if f, ok := m[name]; ok {
			return f.content
		}
	}
	return nil
}

// write sets or appends content to a note file.
func (s *notesStore) write(shellID, name string, data []byte, offset int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.files[shellID] == nil {
		s.files[shellID] = make(map[string]noteFile)
	}
	f := s.files[shellID][name]
	needed := int(offset) + len(data)
	if needed > len(f.content) {
		buf := make([]byte, needed)
		copy(buf, f.content)
		f.content = buf
	}
	copy(f.content[offset:], data)
	f.mtime = time.Now()
	s.files[shellID][name] = f
}

// delete removes a note file from the store.
func (s *notesStore) delete(shellID, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.files[shellID]; ok {
		delete(m, name)
	}
}

// truncate resizes a note file's content buffer.
func (s *notesStore) truncate(shellID, name string, size int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.files[shellID] == nil {
		return
	}
	f := s.files[shellID][name]
	if int64(len(f.content)) > size {
		f.content = f.content[:size]
	} else {
		buf := make([]byte, size)
		copy(buf, f.content)
		f.content = buf
	}
	f.mtime = time.Now()
	s.files[shellID][name] = f
}

// finalise indexes the note into RAG and keeps it in the store.
func (s *notesStore) finalise(shellID, name string, ragEngine *rag.Engine) error {
	if ragEngine == nil {
		return nil
	}
	s.mu.Lock()
	content := ""
	if m, ok := s.files[shellID]; ok {
		if f, ok := m[name]; ok {
			content = string(f.content)
		}
	}
	s.mu.Unlock()

	if strings.TrimSpace(content) == "" {
		return nil
	}

	collection := fmt.Sprintf("shell-%s-notes", shellID)
	return ragEngine.AddContent(collection, content, name, nil)
}
