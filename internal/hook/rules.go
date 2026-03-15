package hook

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/byronellis/ragtime/internal/config"
	"gopkg.in/yaml.v3"
)

// LoadRulesFromDirs loads rules from one or more directories.
// Rules are YAML files with a single rule per file, or a "rules" key with a list.
func LoadRulesFromDirs(dirs ...string) ([]config.RuleConfig, error) {
	var rules []config.RuleConfig

	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read rules dir %s: %w", dir, err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
				continue
			}

			path := filepath.Join(dir, name)
			fileRules, err := loadRulesFromFile(path)
			if err != nil {
				return nil, fmt.Errorf("load rules from %s: %w", path, err)
			}
			rules = append(rules, fileRules...)
		}
	}

	return rules, nil
}

func loadRulesFromFile(path string) ([]config.RuleConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Try as a list of rules under a "rules" key
	var wrapper struct {
		Rules []config.RuleConfig `yaml:"rules"`
	}
	if err := yaml.Unmarshal(data, &wrapper); err == nil && len(wrapper.Rules) > 0 {
		return wrapper.Rules, nil
	}

	// Try as a single rule
	var rule config.RuleConfig
	if err := yaml.Unmarshal(data, &rule); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	if rule.Name == "" {
		// Use filename as rule name
		rule.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return []config.RuleConfig{rule}, nil
}
