package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	defaultSlackSecretFile = "slack-secret.yaml"
)

// SlackSecretConfig stores Slack users that can access documents marked as secret.
type SlackSecretConfig struct {
	AllowedUsers []string `json:"allowed_users" yaml:"allowed_users"`
}

// DefaultSlackSecretConfigPath returns the default allowlist location.
func DefaultSlackSecretConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".", ".config", "ragent", defaultSlackSecretFile)
	}
	return filepath.Join(home, ".config", "ragent", defaultSlackSecretFile)
}

// LoadSlackSecretConfig reads the Slack secret allowlist from YAML.
// Missing files and invalid YAML are treated as "no allowlist" (fail-closed behavior).
func LoadSlackSecretConfig(path string) (*SlackSecretConfig, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultSlackSecretConfigPath()
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &SlackSecretConfig{AllowedUsers: nil}, nil
		}
		return nil, fmt.Errorf("failed to read slack secret config %q: %w", path, err)
	}

	var cfg SlackSecretConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		fmt.Printf("failed to parse slack secret config %q: %v\n", path, err)
		return &SlackSecretConfig{AllowedUsers: nil}, nil
	}

	return &SlackSecretConfig{AllowedUsers: normalizeSlackUsers(cfg.AllowedUsers)}, nil
}

// IsAllowed checks whether the specified Slack user ID is allowlisted.
func (c *SlackSecretConfig) IsAllowed(userID string) bool {
	if c == nil {
		return false
	}

	normalizedUser := strings.TrimSpace(userID)
	if normalizedUser == "" {
		return false
	}

	for _, allowed := range c.AllowedUsers {
		if strings.TrimSpace(allowed) == normalizedUser {
			if allowed == "" {
				continue
			}
			return true
		}
	}

	return false
}

func normalizeSlackUsers(raw []string) []string {
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))

	for _, userID := range raw {
		trimmed := strings.TrimSpace(userID)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}

	return out
}
