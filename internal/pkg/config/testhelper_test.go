package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/joho/godotenv"
)

func loadDotEnvForTest(t *testing.T) {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Logf("warning: failed to get working directory: %v", err)
		return
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			envFile := filepath.Join(dir, ".env")
			if _, err := os.Stat(envFile); err == nil {
				if err := godotenv.Load(envFile); err != nil {
					t.Logf("warning: failed to load .env file: %v", err)
				}
			}
			return
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
}
