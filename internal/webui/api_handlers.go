package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// handleAPIStatus handles the status API
func (s *Server) handleAPIStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := &APIStatusResponse{
		Status:    s.state.GetStatus(),
		Scheduler: s.scheduler.GetState(),
	}

	// Check for external vectorize process via IPC
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	extStatus, err := s.GetExternalProcessStatus(ctx)
	if err != nil {
		s.logger.Printf("Failed to get external process status: %v", err)
	} else if extStatus != nil && extStatus.Status != nil {
		response.ExternalProcess = &ExternalProcessStatus{
			Running: extStatus.Status.State == "running" || extStatus.Status.State == "waiting",
			State:   string(extStatus.Status.State),
			PID:     extStatus.Status.PID,
		}
		// Include progress if available
		if extStatus.Progress != nil {
			response.ExternalProcess.TotalFiles = extStatus.Progress.TotalFiles
			response.ExternalProcess.Processed = extStatus.Progress.ProcessedFiles
			response.ExternalProcess.Percentage = extStatus.Progress.Percentage
		}
		// If external process is running and webui is idle, show as "running"
		if response.ExternalProcess.Running && response.Status == StatusIdle {
			response.Status = StatusRunning
		}
	}

	s.writeJSON(w, response)
}

// handleVectorizeStart handles the vectorize start API
func (s *Server) handleVectorizeStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.state.IsRunning() {
		http.Error(w, "Vectorization already running", http.StatusConflict)
		return
	}

	// Parse dry-run parameter
	dryRun := r.URL.Query().Get("dry-run") == "true"

	// Run vectorization in background
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		if err := s.runVectorization(ctx, dryRun); err != nil {
			s.logger.Printf("Vectorization error: %v", err)
		}
	}()

	// Return accepted status
	w.WriteHeader(http.StatusAccepted)
	s.writeJSON(w, map[string]interface{}{
		"message": "Vectorization started",
		"dry_run": dryRun,
	})
}

// handleVectorizeStop handles the vectorize stop API
func (s *Server) handleVectorizeStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !s.state.IsRunning() {
		http.Error(w, "No vectorization running", http.StatusBadRequest)
		return
	}

	s.state.SetStopping()

	// Cancel the context if we have a cancel function
	if s.cancelFunc != nil {
		s.cancelFunc()
	}

	s.writeJSON(w, map[string]string{
		"message": "Stop requested",
	})
}

// handleVectorizeProgress handles the vectorize progress API
func (s *Server) handleVectorizeProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := &APIProgressResponse{
		Progress: s.state.GetCurrentProgress(),
		LastRun:  s.state.GetLastRun(),
	}

	// Check for external vectorize process progress via IPC
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	extStatus, err := s.GetExternalProcessStatus(ctx)
	if err != nil {
		s.logger.Printf("Failed to get external process status: %v", err)
	} else if extStatus != nil && extStatus.Status != nil && extStatus.Status.State == "running" {
		// If external process is running, use its progress
		if extStatus.Progress != nil {
			response.Progress = &VectorizeProgressEvent{
				Status:          VectorizeStatus(extStatus.Status.State),
				TotalFiles:      extStatus.Progress.TotalFiles,
				ProcessedFiles:  extStatus.Progress.ProcessedFiles,
				SuccessCount:    extStatus.Progress.SuccessCount,
				FailedCount:     extStatus.Progress.FailedCount,
				PercentComplete: extStatus.Progress.Percentage,
			}
			if extStatus.Status.StartedAt != nil {
				response.Progress.StartTime = *extStatus.Status.StartedAt
				response.Progress.ElapsedTime = time.Since(*extStatus.Status.StartedAt).Truncate(time.Second).String()
			}
		}
	}

	s.writeJSON(w, response)
}

// handleAPIFiles handles the files API
func (s *Server) handleAPIFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	searchQuery := r.URL.Query().Get("search")
	files, err := s.getFileList(searchQuery)
	if err != nil {
		s.logger.Printf("Failed to get file list: %v", err)
		http.Error(w, "Failed to get files", http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, files)
}

// handleAPIHistory handles the history API
func (s *Server) handleAPIHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	history := s.state.GetHistory()
	s.writeJSON(w, history)
}

// handleAPIErrors handles the errors API
func (s *Server) handleAPIErrors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	errors := s.state.GetRecentErrors()
	s.writeJSON(w, errors)
}

// handleSchedulerStatus handles the scheduler status API
func (s *Server) handleSchedulerStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	state := s.scheduler.GetState()
	s.writeJSON(w, state)
}

// handleSchedulerToggle handles the scheduler toggle API
func (s *Server) handleSchedulerToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.scheduler.IsEnabled() {
		s.scheduler.Stop()
		s.writeJSON(w, map[string]interface{}{
			"enabled": false,
			"message": "Scheduler stopped",
		})
	} else {
		ctx := context.Background()
		if err := s.scheduler.Start(ctx); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.writeJSON(w, map[string]interface{}{
			"enabled": true,
			"message": "Scheduler started",
		})
	}
}

// handleSchedulerInterval handles the scheduler interval API
func (s *Server) handleSchedulerInterval(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Interval string `json:"interval"`
	}

	// Try to parse from form or JSON
	intervalStr := r.FormValue("interval")
	if intervalStr == "" {
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			intervalStr = req.Interval
		}
	}

	if intervalStr == "" {
		http.Error(w, "Interval required", http.StatusBadRequest)
		return
	}

	duration, err := time.ParseDuration(intervalStr)
	if err != nil {
		http.Error(w, "Invalid interval format", http.StatusBadRequest)
		return
	}

	if err := s.scheduler.SetInterval(duration); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeJSON(w, map[string]interface{}{
		"interval": duration.String(),
		"message":  "Interval updated",
	})
}

// writeJSON writes a JSON response
func (s *Server) writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Printf("Failed to encode JSON: %v", err)
	}
}
