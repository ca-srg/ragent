package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

func (s *Server) handleAPIStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := &APIStatusResponse{
		Status: s.state.GetStatus(),
	}

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
		if extStatus.Progress != nil {
			response.ExternalProcess.TotalFiles = extStatus.Progress.TotalFiles
			response.ExternalProcess.Processed = extStatus.Progress.ProcessedFiles
			response.ExternalProcess.Percentage = extStatus.Progress.Percentage
		}
		if response.ExternalProcess.Running && response.Status == StatusIdle {
			response.Status = StatusRunning
		}
	}

	s.writeJSON(w, response)
}

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

func (s *Server) handleAPIHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	history := s.state.GetHistory()
	s.writeJSON(w, history)
}

func (s *Server) handleAPIErrors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	errors := s.state.GetRecentErrors()
	s.writeJSON(w, errors)
}

func (s *Server) writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Printf("Failed to encode JSON: %v", err)
	}
}
