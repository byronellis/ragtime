package rag

import (
	"strings"
)

const (
	DefaultChunkSize    = 2000 // characters
	DefaultChunkOverlap = 200  // characters
)

// Chunk represents a piece of text with metadata.
type Chunk struct {
	ID       string            `json:"id"`
	Content  string            `json:"content"`
	Source   string            `json:"source"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ChunkText splits text into overlapping chunks.
func ChunkText(text, source string, chunkSize, overlap int) []Chunk {
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	if overlap <= 0 {
		overlap = DefaultChunkOverlap
	}
	if overlap >= chunkSize {
		overlap = chunkSize / 4
	}

	// First try splitting on paragraph boundaries (double newline)
	paragraphs := strings.Split(text, "\n\n")

	var chunks []Chunk
	var current strings.Builder
	chunkIdx := 0

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		// If adding this paragraph would exceed chunk size, emit current chunk
		if current.Len() > 0 && current.Len()+len(para)+2 > chunkSize {
			chunks = append(chunks, Chunk{
				ID:      chunkID(source, chunkIdx),
				Content: strings.TrimSpace(current.String()),
				Source:  source,
			})
			chunkIdx++

			// Keep overlap from end of current chunk
			content := current.String()
			current.Reset()
			if len(content) > overlap {
				current.WriteString(content[len(content)-overlap:])
			}
		}

		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		current.WriteString(para)

		// If a single paragraph exceeds chunk size, force split it
		for current.Len() > chunkSize {
			text := current.String()
			chunks = append(chunks, Chunk{
				ID:      chunkID(source, chunkIdx),
				Content: strings.TrimSpace(text[:chunkSize]),
				Source:  source,
			})
			chunkIdx++

			current.Reset()
			remaining := text[chunkSize-overlap:]
			current.WriteString(remaining)
		}
	}

	// Emit final chunk
	if current.Len() > 0 {
		chunks = append(chunks, Chunk{
			ID:      chunkID(source, chunkIdx),
			Content: strings.TrimSpace(current.String()),
			Source:  source,
		})
	}

	return chunks
}

func chunkID(source string, idx int) string {
	return strings.ReplaceAll(source, "/", "_") + "_" + itoa(idx)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var digits []byte
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}
