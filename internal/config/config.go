package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/byronellis/ragtime/internal/project"
	"gopkg.in/yaml.v3"
)

// Config is the top-level ragtime configuration.
type Config struct {
	Daemon     DaemonConfig     `yaml:"daemon"`
	Agents     AgentsConfig     `yaml:"agents"`
	Embeddings EmbeddingsConfig `yaml:"embeddings"`
	Rules      []RuleConfig     `yaml:"rules,omitempty"`
}

// DaemonConfig holds daemon settings.
type DaemonConfig struct {
	Socket   string `yaml:"socket"`
	HTTPPort int    `yaml:"http_port"`
	LogLevel string `yaml:"log_level"`
}

// AgentsConfig holds per-agent settings.
type AgentsConfig struct {
	Claude AgentConfig `yaml:"claude"`
	Gemini AgentConfig `yaml:"gemini"`
}

// AgentConfig holds settings for a single agent.
type AgentConfig struct {
	Enabled bool `yaml:"enabled"`
}

// EmbeddingsConfig holds embedding provider settings.
type EmbeddingsConfig struct {
	Provider string `yaml:"provider"`
	Endpoint string `yaml:"endpoint"`
	Model    string `yaml:"model"`
	APIKey   string `yaml:"api_key,omitempty"`
}

// RuleConfig is the YAML representation of a hook rule.
type RuleConfig struct {
	Name    string      `yaml:"name"`
	Match   MatchConfig `yaml:"match"`
	Actions []Action    `yaml:"actions"`
}

// MatchConfig defines what events a rule matches.
type MatchConfig struct {
	Agent    string `yaml:"agent,omitempty"`
	Event    string `yaml:"event,omitempty"`
	Tool     string `yaml:"tool,omitempty"`
	PathGlob string `yaml:"path_glob,omitempty"`
}

// Action defines what to do when a rule matches.
type Action struct {
	Type        string   `yaml:"type"`
	Content     string   `yaml:"content,omitempty"`
	Script      string   `yaml:"script,omitempty"`
	Countdown   int      `yaml:"countdown,omitempty"`
	Reason      string   `yaml:"reason,omitempty"`
	Collections []string `yaml:"collections,omitempty"`
	QueryFrom   string   `yaml:"query_from,omitempty"`
	TopK        int      `yaml:"top_k,omitempty"`
}

// Defaults returns a Config with sensible default values.
func Defaults() *Config {
	globalDir := project.GlobalDir()
	return &Config{
		Daemon: DaemonConfig{
			Socket:   filepath.Join(globalDir, "daemon.sock"),
			HTTPPort: 7483,
			LogLevel: "info",
		},
		Agents: AgentsConfig{
			Claude: AgentConfig{Enabled: true},
			Gemini: AgentConfig{Enabled: true},
		},
		Embeddings: EmbeddingsConfig{
			Provider: "ollama",
			Endpoint: "http://localhost:11434",
			Model:    "nomic-embed-text",
		},
	}
}

// Load reads the global config, merges any per-project overrides, and returns the result.
// If cwd is empty, it defaults to the current working directory.
func Load(cwd string) (*Config, error) {
	if cwd == "" {
		cwd = "."
	}

	cfg := Defaults()

	// Load global config
	globalDir := project.GlobalDir()
	if globalDir != "" {
		if err := mergeFromFile(cfg, filepath.Join(globalDir, "config.yaml")); err != nil {
			return nil, fmt.Errorf("global config: %w", err)
		}
	}

	// Load per-project config
	projDir := project.RagtimeDir(cwd)
	if projDir != "" {
		if err := mergeFromFile(cfg, filepath.Join(projDir, "config.yaml")); err != nil {
			return nil, fmt.Errorf("project config: %w", err)
		}
	}

	// Expand ~ in paths after all config sources are merged
	cfg.Daemon.Socket = expandHome(cfg.Daemon.Socket)

	return cfg, nil
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[1:])
}

func mergeFromFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return yaml.Unmarshal(data, cfg)
}
