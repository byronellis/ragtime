package rag

import (
	"math"
	"sort"
)

// SearchResult represents a single search result with score.
type SearchResult struct {
	Chunk Chunk   `json:"chunk"`
	Score float32 `json:"score"`
}

// CosineSimilarity computes cosine similarity between two vectors.
func CosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}

	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return float32(dot / denom)
}

// BruteForceSearch finds the top-k most similar vectors to the query.
func BruteForceSearch(query []float32, vectors [][]float32, chunks []Chunk, topK int) []SearchResult {
	if len(vectors) == 0 || topK <= 0 {
		return nil
	}

	type scored struct {
		idx   int
		score float32
	}

	scores := make([]scored, len(vectors))
	for i, v := range vectors {
		scores[i] = scored{idx: i, score: CosineSimilarity(query, v)}
	}

	sort.Slice(scores, func(i, j int) bool {
		return scores[i].score > scores[j].score
	})

	if topK > len(scores) {
		topK = len(scores)
	}

	results := make([]SearchResult, topK)
	for i := 0; i < topK; i++ {
		results[i] = SearchResult{
			Chunk: chunks[scores[i].idx],
			Score: scores[i].score,
		}
	}

	return results
}
