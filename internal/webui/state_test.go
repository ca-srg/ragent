package webui

import (
	"context"
	"io"
	"log"
	"testing"
	"time"

	"github.com/ca-srg/ragent/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSSEManager() *SSEManager {
	logger := log.New(io.Discard, "", 0)
	manager := NewSSEManager(&SSEConfig{
		HeartbeatInterval: 1 * time.Hour, // Long interval to avoid heartbeat in tests
		BufferSize:        10,
		MaxClients:        10,
	}, logger)
	manager.Start(context.Background())
	return manager
}

func TestNewVectorizeState(t *testing.T) {
	sse := newTestSSEManager()
	defer sse.Stop()

	state := NewVectorizeState(sse)

	assert.NotNil(t, state)
	assert.Equal(t, StatusIdle, state.GetStatus())
	assert.False(t, state.IsRunning())
	assert.Empty(t, state.GetHistory())
	assert.Empty(t, state.GetRecentErrors())
}

func TestVectorizeStateStartRun(t *testing.T) {
	sse := newTestSSEManager()
	defer sse.Stop()

	state := NewVectorizeState(sse)

	runID := state.StartRun(10, false)

	assert.NotEmpty(t, runID)
	assert.Equal(t, StatusRunning, state.GetStatus())
	assert.True(t, state.IsRunning())

	progress := state.GetCurrentProgress()
	assert.Equal(t, StatusRunning, progress.Status)
	assert.Equal(t, 10, progress.TotalFiles)
	assert.Equal(t, 0, progress.ProcessedFiles)
}

func TestVectorizeStateStartRunDryRun(t *testing.T) {
	state := NewVectorizeState(nil)

	runID := state.StartRun(5, true)

	assert.NotEmpty(t, runID)
	assert.Equal(t, StatusRunning, state.GetStatus())

	progress := state.GetCurrentProgress()
	assert.Equal(t, 5, progress.TotalFiles)
}

func TestVectorizeStateUpdateProgress(t *testing.T) {
	state := NewVectorizeState(nil)

	state.StartRun(10, false)
	state.UpdateProgress(5, 4, 1, "test/file.md")

	progress := state.GetCurrentProgress()
	assert.Equal(t, 5, progress.ProcessedFiles)
	assert.Equal(t, 4, progress.SuccessCount)
	assert.Equal(t, 1, progress.FailedCount)
	assert.Equal(t, 50.0, progress.PercentComplete)
}

func TestVectorizeStateUpdateProgressNoRun(t *testing.T) {
	state := NewVectorizeState(nil)

	// Should not panic when no run is active
	state.UpdateProgress(5, 4, 1, "test/file.md")

	progress := state.GetCurrentProgress()
	assert.Equal(t, StatusIdle, progress.Status)
}

func TestVectorizeStateCompleteRun(t *testing.T) {
	state := NewVectorizeState(nil)

	state.StartRun(10, false)
	state.UpdateProgress(10, 8, 2, "")

	result := &types.ProcessingResult{
		ProcessedFiles: 10,
		SuccessCount:   8,
		FailureCount:   2,
		Errors: []types.ProcessingError{
			{
				Timestamp: time.Now(),
				FilePath:  "test/error.md",
				Type:      types.ErrorTypeEmbedding,
				Message:   "embedding failed",
				Retryable: true,
			},
		},
	}

	state.CompleteRun(result)

	assert.Equal(t, StatusIdle, state.GetStatus())
	assert.False(t, state.IsRunning())

	lastRun := state.GetLastRun()
	require.NotNil(t, lastRun)
	assert.Equal(t, 10, lastRun.ProcessedFiles)
	assert.Equal(t, 8, lastRun.SuccessCount)
	assert.Equal(t, 2, lastRun.FailedCount)
	assert.Len(t, lastRun.Errors, 1)

	history := state.GetHistory()
	assert.Len(t, history, 1)
}

func TestVectorizeStateCompleteRunNoRun(t *testing.T) {
	state := NewVectorizeState(nil)

	// Should not panic when no run is active
	result := &types.ProcessingResult{
		ProcessedFiles: 5,
	}
	state.CompleteRun(result)

	assert.Equal(t, StatusIdle, state.GetStatus())
	assert.Nil(t, state.GetLastRun())
}

func TestVectorizeStateFailRun(t *testing.T) {
	state := NewVectorizeState(nil)

	state.StartRun(10, false)
	state.FailRun(assert.AnError)

	assert.Equal(t, StatusError, state.GetStatus())
	assert.False(t, state.IsRunning())

	history := state.GetHistory()
	assert.Len(t, history, 1)
	assert.Equal(t, StatusError, history[0].Status)
}

func TestVectorizeStateFailRunNoRun(t *testing.T) {
	state := NewVectorizeState(nil)

	// Should not panic when no run is active
	state.FailRun(assert.AnError)

	assert.Equal(t, StatusError, state.GetStatus())
}

func TestVectorizeStateSetStopping(t *testing.T) {
	state := NewVectorizeState(nil)

	state.StartRun(10, false)
	state.SetStopping()

	assert.Equal(t, StatusStopping, state.GetStatus())
	assert.True(t, state.IsRunning()) // Still considered running
}

func TestVectorizeStateSetStoppingNotRunning(t *testing.T) {
	state := NewVectorizeState(nil)

	state.SetStopping()

	// Should not change status when not running
	assert.Equal(t, StatusIdle, state.GetStatus())
}

func TestVectorizeStateReset(t *testing.T) {
	state := NewVectorizeState(nil)

	state.StartRun(10, false)
	assert.True(t, state.IsRunning())

	state.Reset()

	assert.Equal(t, StatusIdle, state.GetStatus())
	assert.False(t, state.IsRunning())
}

func TestVectorizeStateAddError(t *testing.T) {
	state := NewVectorizeState(nil)

	errInfo := ErrorInfo{
		Timestamp: time.Now(),
		FilePath:  "test/file.md",
		ErrorType: types.ErrorTypeEmbedding,
		Message:   "test error",
		Retryable: true,
	}

	state.AddError(errInfo)

	errors := state.GetRecentErrors()
	assert.Len(t, errors, 1)
	assert.Equal(t, "test/file.md", errors[0].FilePath)
}

func TestVectorizeStateErrorsMaxSize(t *testing.T) {
	state := NewVectorizeState(nil)

	// Add more than maxErrorsSize errors
	for i := 0; i < 60; i++ {
		state.AddError(ErrorInfo{
			Timestamp: time.Now(),
			FilePath:  "test/file.md",
			Message:   "error",
		})
	}

	errors := state.GetRecentErrors()
	assert.Len(t, errors, maxErrorsSize)
}

func TestVectorizeStateHistoryMaxSize(t *testing.T) {
	state := NewVectorizeState(nil)

	// Add more than maxHistorySize runs
	for i := 0; i < 110; i++ {
		state.StartRun(1, false)
		state.CompleteRun(&types.ProcessingResult{
			ProcessedFiles: 1,
			SuccessCount:   1,
		})
	}

	history := state.GetHistory()
	assert.Len(t, history, maxHistorySize)
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{
			name:     "seconds only",
			duration: 45 * time.Second,
			expected: "45s",
		},
		{
			name:     "minutes and seconds",
			duration: 3*time.Minute + 25*time.Second,
			expected: "3m25s",
		},
		{
			name:     "hours minutes seconds",
			duration: 2*time.Hour + 15*time.Minute + 30*time.Second,
			expected: "2h15m30s",
		},
		{
			name:     "zero duration",
			duration: 0,
			expected: "0s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDuration(tt.duration)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestVectorizeStateProgressPercentCalculation(t *testing.T) {
	state := NewVectorizeState(nil)

	state.StartRun(100, false)

	// Test various progress points
	testCases := []struct {
		processed       int
		expectedPercent float64
	}{
		{0, 0.0},
		{25, 25.0},
		{50, 50.0},
		{75, 75.0},
		{100, 100.0},
	}

	for _, tc := range testCases {
		state.UpdateProgress(tc.processed, tc.processed, 0, "")
		progress := state.GetCurrentProgress()
		assert.Equal(t, tc.expectedPercent, progress.PercentComplete)
	}
}

func TestVectorizeStateConcurrentAccess(t *testing.T) {
	state := NewVectorizeState(nil)
	state.StartRun(1000, false)

	done := make(chan bool)

	// Writer goroutine
	go func() {
		for i := 0; i < 100; i++ {
			state.UpdateProgress(i, i, 0, "file.md")
		}
		done <- true
	}()

	// Reader goroutine
	go func() {
		for i := 0; i < 100; i++ {
			_ = state.GetCurrentProgress()
			_ = state.IsRunning()
			_ = state.GetStatus()
		}
		done <- true
	}()

	<-done
	<-done
}
