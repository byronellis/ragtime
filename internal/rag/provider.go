package rag

import "context"

// EmbeddingProvider generates embedding vectors from text.
type EmbeddingProvider interface {
	// Embed generates embedding vectors for the given texts.
	Embed(ctx context.Context, texts []string) ([][]float32, error)

	// Dimensions returns the number of dimensions in the embedding vectors.
	Dimensions() int
}
