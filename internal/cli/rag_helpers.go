package cli

import (
	"fmt"
	"path/filepath"

	"github.com/byronellis/ragtime/internal/config"
	"github.com/byronellis/ragtime/internal/project"
	"github.com/byronellis/ragtime/internal/rag"
	"github.com/byronellis/ragtime/internal/rag/providers"
)

// resolveRAGConfig loads config and returns embedding config + index directories.
func resolveRAGConfig() (config.EmbeddingsConfig, []string, error) {
	cfg, err := config.Load(".")
	if err != nil {
		return config.EmbeddingsConfig{}, nil, fmt.Errorf("load config: %w", err)
	}

	var indexDirs []string

	// Per-project first (higher priority)
	projDir := project.RagtimeDir(".")
	if projDir != "" {
		indexDirs = append(indexDirs, filepath.Join(projDir, "indexes"))
	}

	// Global
	globalDir := project.GlobalDir()
	if globalDir != "" {
		indexDirs = append(indexDirs, filepath.Join(globalDir, "indexes"))
	}

	return cfg.Embeddings, indexDirs, nil
}

// makeProvider creates an EmbeddingProvider based on config.
func makeProvider(cfg config.EmbeddingsConfig) rag.EmbeddingProvider {
	switch cfg.Provider {
	case "ollama", "":
		return providers.NewOllama(cfg.Endpoint, cfg.Model)
	default:
		// Fall back to ollama
		return providers.NewOllama(cfg.Endpoint, cfg.Model)
	}
}
