package ingestion

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	appconfig "github.com/ca-srg/ragent/internal/pkg/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ca-srg/ragent/internal/ingestion/scanner"
)

// newTestVectorizeCmd creates a minimal cobra.Command with the --interval flag
// bound to the package-level followInterval var, for use in validateFollowModeFlags tests.
func newTestVectorizeCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "vectorize"}
	cmd.Flags().StringVar(&followInterval, "interval", DefaultFollowInterval, "test interval flag")
	return cmd
}

func resetFollowModeState() {
	followMode = false
	followInterval = DefaultFollowInterval
	followIntervalDuration = 0
	dryRun = false
	clearVectors = false
	followModeProcessing.Store(false)

	vectorizationRunner = executeVectorizationOnce
}

func TestValidateFollowModeFlags(t *testing.T) {
	resetFollowModeState()
	t.Cleanup(resetFollowModeState)

	tests := []struct {
		name         string
		setup        func(*cobra.Command)
		wantErr      bool
		errContains  string
		wantDuration time.Duration
	}{
		{
			name: "valid custom interval",
			setup: func(cmd *cobra.Command) {
				followMode = true
				flag := cmd.Flags().Lookup("interval")
				_ = flag.Value.Set("45m")
				flag.Changed = true
			},
			wantDuration: 45 * time.Minute,
		},
		{
			name: "valid default interval",
			setup: func(cmd *cobra.Command) {
				followMode = true
			},
			wantDuration: 30 * time.Minute,
		},
		{
			name: "dry-run incompatible",
			setup: func(cmd *cobra.Command) {
				followMode = true
				dryRun = true
			},
			wantErr:     true,
			errContains: "--follow cannot be used with --dry-run",
		},
		{
			name: "clear incompatible",
			setup: func(cmd *cobra.Command) {
				followMode = true
				clearVectors = true
			},
			wantErr:     true,
			errContains: "--follow cannot be used with --clear",
		},
		{
			name: "interval too short",
			setup: func(cmd *cobra.Command) {
				followMode = true
				flag := cmd.Flags().Lookup("interval")
				_ = flag.Value.Set("4m")
				flag.Changed = true
			},
			wantErr:     true,
			errContains: "at least",
		},
		{
			name: "invalid interval format",
			setup: func(cmd *cobra.Command) {
				followMode = true
				flag := cmd.Flags().Lookup("interval")
				_ = flag.Value.Set("abc")
				flag.Changed = true
			},
			wantErr:     true,
			errContains: "invalid interval",
		},
		{
			name: "interval without follow",
			setup: func(cmd *cobra.Command) {
				flag := cmd.Flags().Lookup("interval")
				_ = flag.Value.Set("15m")
				flag.Changed = true
			},
			wantErr:     true,
			errContains: "--interval flag requires --follow",
		},
		{
			name: "follow disabled resets duration",
			setup: func(cmd *cobra.Command) {
				followIntervalDuration = time.Hour
			},
			wantDuration: 0,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			resetFollowModeState()
			cmd := newTestVectorizeCmd()
			if tc.setup != nil {
				tc.setup(cmd)
			}
			err := validateFollowModeFlags(cmd)

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
	vectorizationRunner = func(ctx context.Context, cfg *appconfig.Config) (*ProcessingResult, error) {
		mu.Lock()
		callCount++
		mu.Unlock()
		return &ProcessingResult{ProcessedFiles: 3}, nil
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
		errCh <- runFollowMode(ctx, &appconfig.Config{})
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

	vectorizationRunner = func(ctx context.Context, cfg *appconfig.Config) (*ProcessingResult, error) {
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

	result, err := runFollowCycleWithIPC(context.Background(), &appconfig.Config{}, nil)
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

func TestScannerDetectsPDFFiles(t *testing.T) {
	s := scanner.NewFileScanner()

	assert.True(t, s.IsPDFFile("document.pdf"))
	assert.True(t, s.IsPDFFile("path/to/file.PDF"))
	assert.True(t, s.IsPDFFile("/absolute/path/report.pdf"))
	assert.False(t, s.IsPDFFile("document.md"))
	assert.False(t, s.IsPDFFile("data.csv"))
	assert.False(t, s.IsPDFFile("file.txt"))
}

func TestScannerSupportsPDFFiles(t *testing.T) {
	s := scanner.NewFileScanner()

	assert.True(t, s.IsSupportedFile("document.pdf"))
	assert.True(t, s.IsSupportedFile("document.md"))
	assert.True(t, s.IsSupportedFile("data.csv"))
	assert.False(t, s.IsSupportedFile("image.png"))
	assert.False(t, s.IsSupportedFile("archive.zip"))
}

func TestScanDirectoryDetectsPDFFiles(t *testing.T) {
	s := scanner.NewFileScanner()

	// Create a temp PDF file
	tmpDir := t.TempDir()
	pdfPath := filepath.Join(tmpDir, "test.pdf")
	err := os.WriteFile(pdfPath, []byte("fake pdf content"), 0644)
	require.NoError(t, err)

	// Scan the directory
	files, err := s.ScanDirectory(tmpDir)
	require.NoError(t, err)

	// Find the PDF file
	var pdfFile *scanner.FileInfo
	for _, f := range files {
		if f.IsPDF {
			pdfFile = f
			break
		}
	}

	require.NotNil(t, pdfFile, "PDF file should be detected")
	assert.True(t, pdfFile.IsPDF)
	assert.Equal(t, pdfPath, pdfFile.Path)
}
