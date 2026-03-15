package rag

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

// CollectionMeta holds metadata about a collection.
type CollectionMeta struct {
	Name       string `json:"name"`
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	Dimensions int    `json:"dimensions"`
	ChunkCount int    `json:"chunk_count"`
}

// Collection manages chunks and vectors on disk.
type Collection struct {
	dir  string
	meta CollectionMeta
}

// OpenCollection opens an existing collection or returns an error.
func OpenCollection(dir string) (*Collection, error) {
	metaPath := filepath.Join(dir, "meta.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("read collection meta: %w", err)
	}
	var meta CollectionMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse collection meta: %w", err)
	}
	return &Collection{dir: dir, meta: meta}, nil
}

// CreateCollection creates a new collection directory and metadata.
func CreateCollection(dir string, meta CollectionMeta) (*Collection, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	c := &Collection{dir: dir, meta: meta}
	return c, c.saveMeta()
}

// Meta returns the collection metadata.
func (c *Collection) Meta() CollectionMeta {
	return c.meta
}

// LoadChunks reads all chunks from disk.
func (c *Collection) LoadChunks() ([]Chunk, error) {
	path := filepath.Join(c.dir, "chunks.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var chunks []Chunk
	return chunks, json.Unmarshal(data, &chunks)
}

// LoadVectors reads all vectors from disk.
func (c *Collection) LoadVectors() ([][]float32, error) {
	path := filepath.Join(c.dir, "vectors.bin")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	dims := c.meta.Dimensions
	if dims <= 0 {
		return nil, fmt.Errorf("invalid dimensions: %d", dims)
	}

	floatSize := 4
	vectorSize := dims * floatSize
	if len(data)%vectorSize != 0 {
		return nil, fmt.Errorf("corrupt vectors file: %d bytes, dims=%d", len(data), dims)
	}

	count := len(data) / vectorSize
	vectors := make([][]float32, count)
	for i := 0; i < count; i++ {
		vec := make([]float32, dims)
		for j := 0; j < dims; j++ {
			offset := (i*dims + j) * floatSize
			bits := binary.LittleEndian.Uint32(data[offset : offset+floatSize])
			vec[j] = math.Float32frombits(bits)
		}
		vectors[i] = vec
	}

	return vectors, nil
}

// SaveChunksAndVectors writes chunks and vectors to disk.
func (c *Collection) SaveChunksAndVectors(chunks []Chunk, vectors [][]float32) error {
	// Save chunks
	chunksData, err := json.MarshalIndent(chunks, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal chunks: %w", err)
	}
	if err := os.WriteFile(filepath.Join(c.dir, "chunks.json"), chunksData, 0o644); err != nil {
		return fmt.Errorf("write chunks: %w", err)
	}

	// Save vectors as packed float32
	dims := 0
	if len(vectors) > 0 {
		dims = len(vectors[0])
	}
	vecData := make([]byte, len(vectors)*dims*4)
	for i, vec := range vectors {
		for j, v := range vec {
			offset := (i*dims + j) * 4
			binary.LittleEndian.PutUint32(vecData[offset:], math.Float32bits(v))
		}
	}
	if err := os.WriteFile(filepath.Join(c.dir, "vectors.bin"), vecData, 0o644); err != nil {
		return fmt.Errorf("write vectors: %w", err)
	}

	// Update meta
	c.meta.ChunkCount = len(chunks)
	if dims > 0 {
		c.meta.Dimensions = dims
	}
	return c.saveMeta()
}

// AppendChunksAndVectors adds new chunks and vectors to an existing collection.
func (c *Collection) AppendChunksAndVectors(newChunks []Chunk, newVectors [][]float32) error {
	existing, err := c.LoadChunks()
	if err != nil {
		return err
	}
	existingVecs, err := c.LoadVectors()
	if err != nil {
		return err
	}

	allChunks := append(existing, newChunks...)
	allVectors := append(existingVecs, newVectors...)

	return c.SaveChunksAndVectors(allChunks, allVectors)
}

func (c *Collection) saveMeta() error {
	data, err := json.MarshalIndent(&c.meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(c.dir, "meta.json"), data, 0o644)
}
