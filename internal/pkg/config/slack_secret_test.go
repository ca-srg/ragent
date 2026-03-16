package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadSlackSecretConfig_ValidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "slack-secret.yaml")
	require.NoError(t, os.WriteFile(path, []byte("allowed_users:\n  - U12345\n  - U67890\n"), 0o600))

	cfg, err := LoadSlackSecretConfig(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, []string{"U12345", "U67890"}, cfg.AllowedUsers)
}

func TestLoadSlackSecretConfig_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "missing.yaml")

	cfg, err := LoadSlackSecretConfig(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Empty(t, cfg.AllowedUsers)
}

func TestLoadSlackSecretConfig_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "invalid.yaml")
	require.NoError(t, os.WriteFile(path, []byte("allowed_users: [unclosed"), 0o600))

	cfg, err := LoadSlackSecretConfig(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Empty(t, cfg.AllowedUsers)
}

func TestLoadSlackSecretConfig_DuplicateIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duplicate.yaml")
	require.NoError(t, os.WriteFile(path, []byte("allowed_users:\n  - U12345\n  - U12345\n  - U67890\n"), 0o600))

	cfg, err := LoadSlackSecretConfig(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"U12345", "U67890"}, cfg.AllowedUsers)
}

func TestLoadSlackSecretConfig_WhitespaceIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "whitespace.yaml")
	require.NoError(t, os.WriteFile(path, []byte("allowed_users:\n  - U12345\n  -  \n  -  U67890 \n"), 0o600))

	cfg, err := LoadSlackSecretConfig(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"U12345", "U67890"}, cfg.AllowedUsers)
}

func TestSlackSecretConfig_IsAllowed(t *testing.T) {
	cfg := &SlackSecretConfig{AllowedUsers: []string{"U12345", "U67890"}}
	assert.True(t, cfg.IsAllowed("U12345"))
	assert.False(t, cfg.IsAllowed("U00000"))
	assert.False(t, (&SlackSecretConfig{}).IsAllowed("U12345"))
}

func TestSlackSecretConfig_DefaultPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	expectedSuffix := filepath.Join(".config", "ragent", defaultSlackSecretFile)
	if !strings.HasSuffix(DefaultSlackSecretConfigPath(), expectedSuffix) {
		t.Fatalf("default path %q does not match expected suffix %q", DefaultSlackSecretConfigPath(), expectedSuffix)
	}
}
