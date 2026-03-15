package rag

import (
	"strings"
	"testing"
)

func TestChunkText_Simple(t *testing.T) {
	text := "Paragraph one.\n\nParagraph two.\n\nParagraph three."
	chunks := ChunkText(text, "test.md", 1000, 50)

	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk (text fits), got %d", len(chunks))
	}
	if !strings.Contains(chunks[0].Content, "Paragraph one") {
		t.Error("chunk should contain paragraph one")
	}
}

func TestChunkText_Split(t *testing.T) {
	// Create text that exceeds chunk size
	var parts []string
	for i := 0; i < 20; i++ {
		parts = append(parts, strings.Repeat("word ", 100))
	}
	text := strings.Join(parts, "\n\n")

	chunks := ChunkText(text, "big.md", 2000, 200)

	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}

	// Each chunk should have content
	for i, c := range chunks {
		if c.Content == "" {
			t.Errorf("chunk %d is empty", i)
		}
		if c.Source != "big.md" {
			t.Errorf("chunk %d source = %q", i, c.Source)
		}
	}
}

func TestChunkText_EmptyInput(t *testing.T) {
	chunks := ChunkText("", "empty.md", 1000, 100)
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for empty input, got %d", len(chunks))
	}
}
