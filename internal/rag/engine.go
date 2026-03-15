package rag

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/byronellis/ragtime/internal/hook"
)

// Engine orchestrates RAG indexing and search.
type Engine struct {
	indexDirs []string // directories to look for collections
	provider  EmbeddingProvider
	logger    *slog.Logger
}

// NewEngine creates a RAG engine.
func NewEngine(indexDirs []string, provider EmbeddingProvider, logger *slog.Logger) *Engine {
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{
		indexDirs: indexDirs,
		provider:  provider,
		logger:    logger,
	}
}

// Search finds the most relevant chunks in a collection.
func (e *Engine) Search(collection, query string, topK int) ([]hook.SearchResult, error) {
	col, err := e.openCollection(collection)
	if err != nil {
		return nil, fmt.Errorf("open collection %q: %w", collection, err)
	}

	chunks, err := col.LoadChunks()
	if err != nil {
		return nil, fmt.Errorf("load chunks: %w", err)
	}
	vectors, err := col.LoadVectors()
	if err != nil {
		return nil, fmt.Errorf("load vectors: %w", err)
	}

	if len(chunks) == 0 {
		return nil, nil
	}

	// Embed the query
	queryVecs, err := e.provider.Embed(context.Background(), []string{query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(queryVecs) == 0 {
		return nil, fmt.Errorf("no query embedding returned")
	}

	// Search
	results := BruteForceSearch(queryVecs[0], vectors, chunks, topK)

	// Convert to hook.SearchResult
	hookResults := make([]hook.SearchResult, len(results))
	for i, r := range results {
		hookResults[i] = hook.SearchResult{
			Content: r.Chunk.Content,
			Source:  r.Chunk.Source,
			Score:   r.Score,
		}
	}

	return hookResults, nil
}

// Index creates or updates a collection from files in a directory.
func (e *Engine) Index(name, sourcePath string) error {
	absPath, err := filepath.Abs(sourcePath)
	if err != nil {
		return fmt.Errorf("resolve path: %w", err)
	}

	// Collect all text files
	var texts []struct {
		path    string
		content string
	}

	err = filepath.WalkDir(absPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip hidden directories
			if strings.HasPrefix(d.Name(), ".") && path != absPath {
				return filepath.SkipDir
			}
			return nil
		}
		if !isTextFile(d.Name()) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			e.logger.Warn("skip file", "path", path, "error", err)
			return nil
		}
		relPath, _ := filepath.Rel(absPath, path)
		texts = append(texts, struct {
			path    string
			content string
		}{path: relPath, content: string(data)})
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk %s: %w", absPath, err)
	}

	if len(texts) == 0 {
		return fmt.Errorf("no text files found in %s", sourcePath)
	}

	// Chunk all files
	var allChunks []Chunk
	for _, t := range texts {
		chunks := ChunkText(t.content, t.path, DefaultChunkSize, DefaultChunkOverlap)
		allChunks = append(allChunks, chunks...)
	}

	e.logger.Info("indexing", "collection", name, "files", len(texts), "chunks", len(allChunks))

	// Embed all chunks in batches
	batchSize := 32
	var allVectors [][]float32

	for i := 0; i < len(allChunks); i += batchSize {
		end := i + batchSize
		if end > len(allChunks) {
			end = len(allChunks)
		}

		batch := make([]string, end-i)
		for j, c := range allChunks[i:end] {
			batch[j] = c.Content
		}

		vecs, err := e.provider.Embed(context.Background(), batch)
		if err != nil {
			return fmt.Errorf("embed batch %d-%d: %w", i, end, err)
		}
		allVectors = append(allVectors, vecs...)
	}

	// Create/update collection
	colDir := filepath.Join(e.indexDirs[0], name) // use first dir (global or project)
	meta := CollectionMeta{
		Name:       name,
		Provider:   "ollama",
		Model:      "",
		Dimensions: e.provider.Dimensions(),
		ChunkCount: len(allChunks),
	}

	col, err := CreateCollection(colDir, meta)
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	return col.SaveChunksAndVectors(allChunks, allVectors)
}

// AddContent adds text content directly to a collection.
func (e *Engine) AddContent(collection, content, source string, metadata map[string]string) error {
	chunks := []Chunk{{
		ID:       chunkID(source, 0),
		Content:  content,
		Source:   source,
		Metadata: metadata,
	}}

	vecs, err := e.provider.Embed(context.Background(), []string{content})
	if err != nil {
		return fmt.Errorf("embed content: %w", err)
	}

	col, err := e.openCollection(collection)
	if err != nil {
		// Collection doesn't exist, create it
		colDir := filepath.Join(e.indexDirs[0], collection)
		meta := CollectionMeta{
			Name:       collection,
			Provider:   "ollama",
			Dimensions: e.provider.Dimensions(),
		}
		col, err = CreateCollection(colDir, meta)
		if err != nil {
			return fmt.Errorf("create collection: %w", err)
		}
	}

	return col.AppendChunksAndVectors(chunks, vecs)
}

// ListCollections returns metadata for all collections.
func (e *Engine) ListCollections() ([]CollectionMeta, error) {
	var collections []CollectionMeta

	for _, dir := range e.indexDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			col, err := OpenCollection(filepath.Join(dir, entry.Name()))
			if err != nil {
				continue
			}
			collections = append(collections, col.Meta())
		}
	}

	return collections, nil
}

// DeleteCollection removes a collection.
func (e *Engine) DeleteCollection(name string) error {
	for _, dir := range e.indexDirs {
		colDir := filepath.Join(dir, name)
		if _, err := os.Stat(colDir); err == nil {
			return os.RemoveAll(colDir)
		}
	}
	return fmt.Errorf("collection %q not found", name)
}

func (e *Engine) openCollection(name string) (*Collection, error) {
	for _, dir := range e.indexDirs {
		colDir := filepath.Join(dir, name)
		col, err := OpenCollection(colDir)
		if err == nil {
			return col, nil
		}
	}
	return nil, fmt.Errorf("collection %q not found", name)
}

func isTextFile(name string) bool {
	textExts := map[string]bool{
		".md": true, ".txt": true, ".go": true, ".py": true,
		".js": true, ".ts": true, ".jsx": true, ".tsx": true,
		".rs": true, ".java": true, ".c": true, ".h": true,
		".cpp": true, ".hpp": true, ".rb": true, ".sh": true,
		".yaml": true, ".yml": true, ".json": true, ".toml": true,
		".html": true, ".css": true, ".sql": true, ".proto": true,
		".star": true, ".lua": true, ".vim": true, ".el": true,
		".cfg": true, ".ini": true, ".conf": true, ".xml": true,
		".svg": true, ".tex": true, ".r": true, ".R": true,
		".swift": true, ".kt": true, ".scala": true, ".zig": true,
	}
	return textExts[filepath.Ext(name)]
}
