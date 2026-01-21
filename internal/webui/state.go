package webui

import (
	"fmt"
	"sync"
	"time"

	"github.com/ca-srg/ragent/internal/types"
)

const (
	maxHistorySize = 100
	maxErrorsSize  = 50
)

// VectorizeState manages the current state of vectorization
type VectorizeState struct {
	mu           sync.RWMutex
	status       VectorizeStatus
	currentRun   *RunInfo
	lastRun      *RunInfo
	history      []RunInfo
	recentErrors []ErrorInfo
	sseManager   *SSEManager
}

// NewVectorizeState creates a new VectorizeState
func NewVectorizeState(sse *SSEManager) *VectorizeState {
	return &VectorizeState{
		status:       StatusIdle,
		history:      make([]RunInfo, 0, maxHistorySize),
		recentErrors: make([]ErrorInfo, 0, maxErrorsSize),
		sseManager:   sse,
	}
}

// StartRun starts a new vectorization run
func (s *VectorizeState) StartRun(totalFiles int, dryRun bool) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	runID := time.Now().Format("20060102-150405")

	s.currentRun = &RunInfo{
		ID:         runID,
		StartTime:  time.Now(),
		Status:     StatusRunning,
		TotalFiles: totalFiles,
		DryRun:     dryRun,
	}
	s.status = StatusRunning

	if s.sseManager != nil {
		s.sseManager.SendEvent(&SSEEvent{
			Event: EventTypeVectorizeStarted,
			Data: map[string]interface{}{
				"run_id":      runID,
				"total_files": totalFiles,
				"dry_run":     dryRun,
			},
		})
	}

	return runID
}

// UpdateProgress updates the progress of the current run
func (s *VectorizeState) UpdateProgress(processed, success, failed int, currentFile string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currentRun == nil {
		return
	}

	s.currentRun.ProcessedFiles = processed
	s.currentRun.SuccessCount = success
	s.currentRun.FailedCount = failed

	event := s.buildProgressEvent(currentFile)

	if s.sseManager != nil {
		s.sseManager.SendEvent(&SSEEvent{
			Event: EventTypeVectorizeProgress,
			Data:  event,
		})
	}
}

// buildProgressEvent builds a progress event from the current state (must be called with lock held)
func (s *VectorizeState) buildProgressEvent(currentFile string) *VectorizeProgressEvent {
	if s.currentRun == nil {
		return &VectorizeProgressEvent{
			Status: s.status,
		}
	}

	percent := 0.0
	if s.currentRun.TotalFiles > 0 {
		percent = float64(s.currentRun.ProcessedFiles) / float64(s.currentRun.TotalFiles) * 100
	}

	elapsed := time.Since(s.currentRun.StartTime)

	event := &VectorizeProgressEvent{
		Status:          s.status,
		TotalFiles:      s.currentRun.TotalFiles,
		ProcessedFiles:  s.currentRun.ProcessedFiles,
		SuccessCount:    s.currentRun.SuccessCount,
		FailedCount:     s.currentRun.FailedCount,
		PercentComplete: percent,
		CurrentFile:     currentFile,
		StartTime:       s.currentRun.StartTime,
		ElapsedTime:     formatDuration(elapsed),
	}

	// Estimate remaining time
	if s.currentRun.ProcessedFiles > 0 {
		avgTime := elapsed / time.Duration(s.currentRun.ProcessedFiles)
		remaining := avgTime * time.Duration(s.currentRun.TotalFiles-s.currentRun.ProcessedFiles)
		event.EstimatedRemain = formatDuration(remaining)
	}

	return event
}

// CompleteRun completes the current run
func (s *VectorizeState) CompleteRun(result *types.ProcessingResult) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currentRun == nil {
		return
	}

	s.currentRun.EndTime = time.Now()
	s.currentRun.Status = StatusIdle
	s.currentRun.ProcessedFiles = result.ProcessedFiles
	s.currentRun.SuccessCount = result.SuccessCount
	s.currentRun.FailedCount = result.FailureCount

	// Convert errors
	for _, err := range result.Errors {
		s.currentRun.Errors = append(s.currentRun.Errors, ErrorInfo{
			Timestamp: err.Timestamp,
			FilePath:  err.FilePath,
			ErrorType: err.Type,
			Message:   err.Message,
			Retryable: err.Retryable,
		})
	}

	// Add to history
	s.history = append([]RunInfo{*s.currentRun}, s.history...)
	if len(s.history) > maxHistorySize {
		s.history = s.history[:maxHistorySize]
	}

	s.lastRun = s.currentRun
	s.status = StatusIdle

	if s.sseManager != nil {
		s.sseManager.SendEvent(&SSEEvent{
			Event: EventTypeVectorizeCompleted,
			Data:  s.currentRun,
		})
	}

	s.currentRun = nil
}

// FailRun marks the current run as failed
func (s *VectorizeState) FailRun(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.status = StatusError

	if s.currentRun != nil {
		s.currentRun.EndTime = time.Now()
		s.currentRun.Status = StatusError

		// Add to history
		s.history = append([]RunInfo{*s.currentRun}, s.history...)
		if len(s.history) > maxHistorySize {
			s.history = s.history[:maxHistorySize]
		}

		s.lastRun = s.currentRun
	}

	if s.sseManager != nil {
		s.sseManager.SendEvent(&SSEEvent{
			Event: EventTypeVectorizeFailed,
			Data: map[string]interface{}{
				"error": err.Error(),
			},
		})
	}

	s.currentRun = nil
}

// SetStopping sets the status to stopping
func (s *VectorizeState) SetStopping() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status == StatusRunning {
		s.status = StatusStopping
	}
}

// Reset resets to idle status
func (s *VectorizeState) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = StatusIdle
	s.currentRun = nil
}

// AddError adds an error to the recent errors list
func (s *VectorizeState) AddError(err ErrorInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.recentErrors = append([]ErrorInfo{err}, s.recentErrors...)
	if len(s.recentErrors) > maxErrorsSize {
		s.recentErrors = s.recentErrors[:maxErrorsSize]
	}

	if s.sseManager != nil {
		s.sseManager.SendEvent(&SSEEvent{
			Event: EventTypeFileError,
			Data:  err,
		})
	}
}

// GetStatus returns the current status
func (s *VectorizeState) GetStatus() VectorizeStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

// IsRunning returns true if vectorization is running
func (s *VectorizeState) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status == StatusRunning || s.status == StatusStopping
}

// GetCurrentProgress returns the current progress
func (s *VectorizeState) GetCurrentProgress() *VectorizeProgressEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.currentRun == nil {
		return &VectorizeProgressEvent{
			Status: s.status,
		}
	}

	return s.buildProgressEvent("")
}

// GetLastRun returns the last completed run
func (s *VectorizeState) GetLastRun() *RunInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.lastRun == nil {
		return nil
	}
	run := *s.lastRun
	return &run
}

// GetHistory returns the run history
func (s *VectorizeState) GetHistory() []RunInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]RunInfo, len(s.history))
	copy(result, s.history)
	return result
}

// GetRecentErrors returns recent errors
func (s *VectorizeState) GetRecentErrors() []ErrorInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]ErrorInfo, len(s.recentErrors))
	copy(result, s.recentErrors)
	return result
}

// formatDuration formats a duration for display
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	sec := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%dh%dm%ds", h, m, sec)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%ds", m, sec)
	}
	return fmt.Sprintf("%ds", sec)
}
