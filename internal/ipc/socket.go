package ipc

import (
	"fmt"
	"os"
	"path/filepath"
)

// GetSocketPath returns the path for the Unix socket file.
// Priority:
// 1. $XDG_RUNTIME_DIR/ragent/ragent.sock (Linux standard)
// 2. /tmp/ragent-{uid}/ragent.sock (fallback)
func GetSocketPath() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "ragent", "ragent.sock")
	}

	uid := os.Getuid()
	tmpDir := filepath.Join(os.TempDir(), fmt.Sprintf("ragent-%d", uid))
	return filepath.Join(tmpDir, "ragent.sock")
}

// GetPIDPath returns the path for the PID file.
// The PID file is used for exclusive locking to prevent multiple instances.
func GetPIDPath() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "ragent", "ragent.pid")
	}

	uid := os.Getuid()
	tmpDir := filepath.Join(os.TempDir(), fmt.Sprintf("ragent-%d", uid))
	return filepath.Join(tmpDir, "ragent.pid")
}

// GetSocketDir returns the directory containing the socket file.
func GetSocketDir() string {
	return filepath.Dir(GetSocketPath())
}
