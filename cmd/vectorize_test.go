package cmd

import (
	"bytes"
	"context"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ca-srg/ragent/internal/types"
)

func resetFollowModeState() {
	followMode = false
	followInterval = defaultFollowInterval
	followIntervalDuration = 0
	dryRun = false
	clearVectors = false
	followModeProcessing.Store(false)

	if flag := vectorizeCmd.Flags().Lookup("interval"); flag != nil {
		_ = flag.Value.Set(defaultFollowInterval)
		flag.Changed = false
	}

	vectorizationRunner = executeVectorizationOnce
}

func TestValidateFollowModeFlags(t *testing.T) {
	resetFollowModeState()
	t.Cleanup(resetFollowModeState)

	tests := []struct {
		name         string
		setup        func()
		wantErr      bool
		errContains  string
		wantDuration time.Duration
	}{
		{
			name: "valid custom interval",
			setup: func() {
				resetFollowModeState()
				followMode = true
				flag := vectorizeCmd.Flags().Lookup("interval")
				_ = flag.Value.Set("45m")
				flag.Changed = true
			},
			wantDuration: 45 * time.Minute,
		},
		{
			name: "valid default interval",
			setup: func() {
				resetFollowModeState()
				followMode = true
			},
			wantDuration: 30 * time.Minute,
		},
		{
			name: "dry-run incompatible",
			setup: func() {
				resetFollowModeState()
				followMode = true
				dryRun = true
			},
			wantErr:     true,
			errContains: "--follow cannot be used with --dry-run",
		},
		{
			name: "clear incompatible",
			setup: func() {
				resetFollowModeState()
				followMode = true
				clearVectors = true
			},
			wantErr:     true,
			errContains: "--follow cannot be used with --clear",
		},
		{
			name: "interval too short",
			setup: func() {
				resetFollowModeState()
				followMode = true
				flag := vectorizeCmd.Flags().Lookup("interval")
				_ = flag.Value.Set("4m")
				flag.Changed = true
			},
			wantErr:     true,
			errContains: "at least",
		},
		{
			name: "invalid interval format",
			setup: func() {
				resetFollowModeState()
				followMode = true
				flag := vectorizeCmd.Flags().Lookup("interval")
				_ = flag.Value.Set("abc")
				flag.Changed = true
			},
			wantErr:     true,
			errContains: "invalid interval",
		},
		{
			name: "interval without follow",
			setup: func() {
				resetFollowModeState()
				flag := vectorizeCmd.Flags().Lookup("interval")
				_ = flag.Value.Set("15m")
				flag.Changed = true
			},
			wantErr:     true,
			errContains: "--interval flag requires --follow",
		},
		{
			name: "follow disabled resets duration",
			setup: func() {
				resetFollowModeState()
				followIntervalDuration = time.Hour
			},
			wantDuration: 0,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tc.setup()
			err := validateFollowModeFlags(vectorizeCmd)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tc.errContains) {
					t.Fatalf("expected error to contain %q, got %q", tc.errContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tc.wantDuration != 0 {
				if followMode && followIntervalDuration != tc.wantDuration {
					t.Fatalf("expected duration %v, got %v", tc.wantDuration, followIntervalDuration)
				}
				if !followMode && followIntervalDuration != 0 {
					t.Fatalf("expected duration reset to 0, got %v", followIntervalDuration)
				}
			}
		})
	}
}

func TestRunFollowMode_BasicExecution(t *testing.T) {
	resetFollowModeState()
	t.Cleanup(resetFollowModeState)

	var mu sync.Mutex
	callCount := 0
	vectorizationRunner = func(ctx context.Context, cfg *types.Config) (*types.ProcessingResult, error) {
		mu.Lock()
		callCount++
		mu.Unlock()
		return &types.ProcessingResult{ProcessedFiles: 3}, nil
	}

	followIntervalDuration = 20 * time.Millisecond

	var buf bytes.Buffer
	originalWriter := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() {
		log.SetOutput(originalWriter)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- runFollowMode(ctx, &types.Config{})
	}()

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		mu.Lock()
		c := callCount
		mu.Unlock()
		if c >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	calls := callCount
	mu.Unlock()
	if calls < 2 {
		cancel()
		select {
		case err := <-errCh:
			if err != nil {
				t.Logf("runFollowMode returned error during failure path: %v", err)
			}
		case <-time.After(time.Second):
		}
		t.Fatalf("expected at least two vectorization cycles, got %d\nlogs: %s", calls, buf.String())
	}

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("runFollowMode returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for runFollowMode to exit")
	}

	if followModeProcessing.Load() {
		t.Fatalf("expected processing flag to be cleared")
	}

	logs := buf.String()
	for _, expected := range []string{
		"Follow mode enabled. Interval:",
		"[Follow Mode] Starting vectorization cycle...",
		"[Follow Mode] Completed. Processed 3 files.",
		"[Follow Mode] Shutdown complete.",
	} {
		if !strings.Contains(logs, expected) {
			t.Fatalf("expected logs to contain %q\nlogs: %s", expected, logs)
		}
	}
}

func TestRunFollowCycle_SkipWhenProcessing(t *testing.T) {
	resetFollowModeState()
	t.Cleanup(resetFollowModeState)

	vectorizationRunner = func(ctx context.Context, cfg *types.Config) (*types.ProcessingResult, error) {
		t.Fatalf("vectorization runner should not be called when processing flag is set")
		return nil, nil
	}

	followModeProcessing.Store(true)

	var buf bytes.Buffer
	originalWriter := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() {
		log.SetOutput(originalWriter)
	})

	result, err := runFollowCycle(context.Background(), &types.Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result when skipping, got %#v", result)
	}

	if !strings.Contains(buf.String(), "Skipping this cycle") {
		t.Fatalf("expected skip log, got %q", buf.String())
	}
}
