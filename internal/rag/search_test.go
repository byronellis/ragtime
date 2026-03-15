package rag

import "testing"

func TestCosineSimilarity(t *testing.T) {
	// Identical vectors
	a := []float32{1, 0, 0}
	b := []float32{1, 0, 0}
	sim := CosineSimilarity(a, b)
	if sim < 0.999 {
		t.Errorf("identical vectors: sim = %f, want ~1.0", sim)
	}

	// Orthogonal vectors
	c := []float32{0, 1, 0}
	sim = CosineSimilarity(a, c)
	if sim > 0.001 {
		t.Errorf("orthogonal vectors: sim = %f, want ~0.0", sim)
	}

	// Opposite vectors
	d := []float32{-1, 0, 0}
	sim = CosineSimilarity(a, d)
	if sim > -0.999 {
		t.Errorf("opposite vectors: sim = %f, want ~-1.0", sim)
	}
}

func TestBruteForceSearch(t *testing.T) {
	chunks := []Chunk{
		{ID: "0", Content: "doc about dogs"},
		{ID: "1", Content: "doc about cats"},
		{ID: "2", Content: "doc about fish"},
	}

	// Vectors designed so chunk 1 is closest to query
	vectors := [][]float32{
		{0.1, 0.9, 0.0}, // dogs
		{0.9, 0.1, 0.0}, // cats
		{0.0, 0.0, 1.0}, // fish
	}

	query := []float32{0.8, 0.2, 0.0} // similar to cats

	results := BruteForceSearch(query, vectors, chunks, 2)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].Chunk.ID != "1" {
		t.Errorf("top result should be chunk 1 (cats), got %s", results[0].Chunk.ID)
	}
}
