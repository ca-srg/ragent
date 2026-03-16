package slackbot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHybridSearchAdapter_shouldExcludeSecret_AllowsAllowlistedUser(t *testing.T) {
	tmpHome := t.TempDir()
	configDir := filepath.Join(tmpHome, ".config", "ragent")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	slackSecretPath := filepath.Join(configDir, "slack-secret.yaml")
	if err := os.WriteFile(slackSecretPath, []byte("allowed_users:\n  - U12345\n"), 0o600); err != nil {
		t.Fatalf("failed to write slack secret file: %v", err)
	}
	t.Setenv("HOME", tmpHome)

	adapter := &HybridSearchAdapter{}
	if got := adapter.shouldExcludeSecret(SearchOptions{UserID: "U12345"}); got {
		t.Fatalf("expected allowlisted user to include secret docs")
	}
}

func TestHybridSearchAdapter_shouldExcludeSecret_DeniesNonAllowlistedUser(t *testing.T) {
	tmpHome := t.TempDir()
	configDir := filepath.Join(tmpHome, ".config", "ragent")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	slackSecretPath := filepath.Join(configDir, "slack-secret.yaml")
	if err := os.WriteFile(slackSecretPath, []byte("allowed_users:\n  - U12345\n"), 0o600); err != nil {
		t.Fatalf("failed to write slack secret file: %v", err)
	}
	t.Setenv("HOME", tmpHome)

	adapter := &HybridSearchAdapter{}
	if got := adapter.shouldExcludeSecret(SearchOptions{UserID: "U99999"}); !got {
		t.Fatalf("expected non-allowlisted user to be denied secret docs")
	}
}

func TestHybridSearchAdapter_shouldExcludeSecret_DeniesWhenConfigMissing(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	adapter := &HybridSearchAdapter{}
	if got := adapter.shouldExcludeSecret(SearchOptions{UserID: "U12345"}); !got {
		t.Fatalf("expected missing slack-secret.yaml to deny secret docs")
	}
}
