package ipc

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerClientIntegration(t *testing.T) {
	// Use temp directory for test socket
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")
	pidPath := filepath.Join(tmpDir, "test.pid")

	logger := log.New(os.Stdout, "[test-ipc] ", log.LstdFlags)

	// Start server
	server, err := NewServer(ServerConfig{
		SocketPath: socketPath,
		PIDFile:    pidPath,
	}, logger)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, server.Start(ctx))
	defer func() { _ = server.Shutdown(context.Background()) }()

	// Set some status
	now := time.Now()
	server.UpdateStatus(&StatusResponse{
		State:     StateRunning,
		StartedAt: &now,
		PID:       12345,
		DryRun:    false,
	})

	server.UpdateProgress(&ProgressResponse{
		TotalFiles:     100,
		ProcessedFiles: 50,
		SuccessCount:   48,
		FailedCount:    2,
		Percentage:     50.0,
	})

	// Connect client
	client := NewClient(ClientConfig{SocketPath: socketPath})

	// Test GetStatus
	status, err := client.GetStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, StateRunning, status.State)
	assert.Equal(t, 12345, status.PID)

	// Test GetProgress
	progress, err := client.GetProgress(ctx)
	require.NoError(t, err)
	assert.Equal(t, 100, progress.TotalFiles)
	assert.Equal(t, 50, progress.ProcessedFiles)
	assert.Equal(t, 50.0, progress.Percentage)

	// Test GetFullStatus
	fullStatus, err := client.GetFullStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, StateRunning, fullStatus.Status.State)
	assert.NotNil(t, fullStatus.Progress)
	assert.Equal(t, 50, fullStatus.Progress.ProcessedFiles)
}

func TestStaleSocketCleanup(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "stale.sock")
	pidPath := filepath.Join(tmpDir, "stale.pid")

	// Create a stale socket file (just a regular file, not a real socket)
	if err := os.WriteFile(socketPath, []byte{}, 0600); err != nil {
		t.Fatal(err)
	}

	logger := log.New(os.Stdout, "[test-ipc] ", log.LstdFlags)

	// New server should clean up and start
	server, err := NewServer(ServerConfig{
		SocketPath: socketPath,
		PIDFile:    pidPath,
	}, logger)
	require.NoError(t, err)
	defer func() { _ = server.Shutdown(context.Background()) }()

	require.NoError(t, server.Start(context.Background()))

	// Verify socket works
	client := NewClient(ClientConfig{SocketPath: socketPath})
	status, err := client.GetStatus(context.Background())
	require.NoError(t, err)
	assert.Equal(t, StateIdle, status.State)
}

func TestClientNotRunning(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "nonexistent.sock")

	client := NewClient(ClientConfig{SocketPath: socketPath})
	_, err := client.GetStatus(context.Background())
	assert.ErrorIs(t, err, ErrNotRunning)
}

func TestAnotherInstanceRunning(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")
	pidPath := filepath.Join(tmpDir, "test.pid")

	logger := log.New(os.Stdout, "[test-ipc] ", log.LstdFlags)

	// Start first server
	server1, err := NewServer(ServerConfig{
		SocketPath: socketPath,
		PIDFile:    pidPath,
	}, logger)
	require.NoError(t, err)
	require.NoError(t, server1.Start(context.Background()))
	defer func() { _ = server1.Shutdown(context.Background()) }()

	// Try to start second server - should fail
	_, err = NewServer(ServerConfig{
		SocketPath: socketPath,
		PIDFile:    pidPath,
	}, logger)
	assert.ErrorIs(t, err, ErrAnotherInstanceRunning)
}

func TestControlStop(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")
	pidPath := filepath.Join(tmpDir, "test.pid")

	logger := log.New(os.Stdout, "[test-ipc] ", log.LstdFlags)

	server, err := NewServer(ServerConfig{
		SocketPath: socketPath,
		PIDFile:    pidPath,
	}, logger)
	require.NoError(t, err)
	require.NoError(t, server.Start(context.Background()))
	defer func() { _ = server.Shutdown(context.Background()) }()

	// Request stop
	client := NewClient(ClientConfig{SocketPath: socketPath})
	resp, err := client.RequestStop(context.Background(), false)
	require.NoError(t, err)
	assert.True(t, resp.Acknowledged)

	// Verify stop channel is closed
	select {
	case <-server.StopChan():
		// Expected
	default:
		t.Error("stop channel should be closed")
	}

	// Second stop request should indicate already stopping
	resp, err = client.RequestStop(context.Background(), false)
	require.NoError(t, err)
	assert.False(t, resp.Acknowledged)
	assert.Contains(t, resp.Message, "already stopping")
}

func TestMultipleConnections(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")
	pidPath := filepath.Join(tmpDir, "test.pid")

	logger := log.New(os.Stdout, "[test-ipc] ", log.LstdFlags)

	server, err := NewServer(ServerConfig{
		SocketPath: socketPath,
		PIDFile:    pidPath,
	}, logger)
	require.NoError(t, err)
	require.NoError(t, server.Start(context.Background()))
	defer func() { _ = server.Shutdown(context.Background()) }()

	server.SetState(StateRunning)

	// Multiple clients connecting simultaneously
	ctx := context.Background()
	done := make(chan error, 5)

	for i := 0; i < 5; i++ {
		go func() {
			client := NewClient(ClientConfig{SocketPath: socketPath})
			status, err := client.GetStatus(ctx)
			if err != nil {
				done <- err
				return
			}
			if status.State != StateRunning {
				done <- assert.AnError
				return
			}
			done <- nil
		}()
	}

	for i := 0; i < 5; i++ {
		err := <-done
		assert.NoError(t, err)
	}
}
