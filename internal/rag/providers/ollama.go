package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/byronellis/ragtime/internal/rag"
)

// Ollama implements EmbeddingProvider using the Ollama HTTP API.
type Ollama struct {
	endpoint string
	model    string
	dims     int
	client   *http.Client
}

type ollamaEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type ollamaEmbedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// NewOllama creates an Ollama embedding provider.
func NewOllama(endpoint, model string) *Ollama {
	return &Ollama{
		endpoint: endpoint,
		model:    model,
		client:   &http.Client{},
	}
}

// Embed generates embeddings via the Ollama /api/embed endpoint.
func (o *Ollama) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	reqBody := ollamaEmbedRequest{
		Model: o.model,
		Input: texts,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", o.endpoint+"/api/embed", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embed request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama returned %d: %s", resp.StatusCode, string(body))
	}

	var result ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(result.Embeddings) == 0 {
		return nil, fmt.Errorf("no embeddings returned")
	}

	// Cache dimensions from first result
	if o.dims == 0 && len(result.Embeddings) > 0 {
		o.dims = len(result.Embeddings[0])
	}

	return result.Embeddings, nil
}

// Dimensions returns the embedding vector dimensions.
func (o *Ollama) Dimensions() int {
	return o.dims
}

// compile-time check
var _ rag.EmbeddingProvider = (*Ollama)(nil)
