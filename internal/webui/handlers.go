package webui

import (
	"context"
	"net/http"
	"time"
)

// handleDashboard handles the dashboard page
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	state := s.state.GetCurrentProgress()

	// Check for external vectorize process via IPC
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	extStatus, err := s.GetExternalProcessStatus(ctx)
	if err != nil {
		s.logger.Printf("Failed to get external process status: %v", err)
	} else if extStatus != nil && extStatus.Status != nil {
		isExtRunning := extStatus.Status.State == "running" || extStatus.Status.State == "waiting"
		// If external process is active and webui is idle, update state display
		if isExtRunning && state.Status == StatusIdle {
			state.Status = VectorizeStatus(extStatus.Status.State)
			if extStatus.Progress != nil {
				state.TotalFiles = extStatus.Progress.TotalFiles
				state.ProcessedFiles = extStatus.Progress.ProcessedFiles
				state.SuccessCount = extStatus.Progress.SuccessCount
				state.FailedCount = extStatus.Progress.FailedCount
				state.PercentComplete = extStatus.Progress.Percentage
			}
			if extStatus.Status.StartedAt != nil {
				state.StartTime = *extStatus.Status.StartedAt
				state.ElapsedTime = time.Since(*extStatus.Status.StartedAt).Truncate(time.Second).String()
			}
		}
	}

	data := &DashboardData{
		ActivePage:   "dashboard",
		State:        state,
		Scheduler:    s.scheduler.GetState(),
		RecentErrors: s.state.GetRecentErrors(),
		LastRun:      s.state.GetLastRun(),
	}

	if err := s.templates.Render(w, "index.html", data); err != nil {
		s.logger.Printf("Failed to render dashboard: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// handleFilesPage handles the files browser page
func (s *Server) handleFilesPage(w http.ResponseWriter, r *http.Request) {
	searchQuery := r.URL.Query().Get("search")

	files, err := s.getFileList(searchQuery)
	if err != nil {
		s.logger.Printf("Failed to get file list: %v", err)
		http.Error(w, "Failed to get files", http.StatusInternalServerError)
		return
	}

	stats := s.getFileStats(files)

	data := &FilesPageData{
		ActivePage:    "files",
		Files:         files,
		TotalFiles:    stats.TotalFiles,
		MarkdownCount: stats.MarkdownCount,
		CSVCount:      stats.CSVCount,
		SearchQuery:   searchQuery,
	}

	if err := s.templates.Render(w, "files.html", data); err != nil {
		s.logger.Printf("Failed to render files page: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// handleHistoryPage handles the history page
func (s *Server) handleHistoryPage(w http.ResponseWriter, r *http.Request) {
	data := &HistoryPageData{
		ActivePage: "history",
		History:    s.state.GetHistory(),
	}

	if err := s.templates.Render(w, "history.html", data); err != nil {
		s.logger.Printf("Failed to render history page: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// handlePartialProgress handles the progress partial for HTMX
func (s *Server) handlePartialProgress(w http.ResponseWriter, r *http.Request) {
	progress := s.state.GetCurrentProgress()

	// Check for external vectorize process via IPC
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	extStatus, err := s.GetExternalProcessStatus(ctx)
	if err != nil {
		s.logger.Printf("Failed to get external process status: %v", err)
	} else if extStatus != nil && extStatus.Status != nil {
		isExtRunning := extStatus.Status.State == "running" || extStatus.Status.State == "waiting"
		// If external process is active and webui is idle, update progress display
		if isExtRunning && progress.Status == StatusIdle {
			progress.Status = VectorizeStatus(extStatus.Status.State)
			if extStatus.Progress != nil {
				progress.TotalFiles = extStatus.Progress.TotalFiles
				progress.ProcessedFiles = extStatus.Progress.ProcessedFiles
				progress.SuccessCount = extStatus.Progress.SuccessCount
				progress.FailedCount = extStatus.Progress.FailedCount
				progress.PercentComplete = extStatus.Progress.Percentage
			}
			if extStatus.Status.StartedAt != nil {
				progress.StartTime = *extStatus.Status.StartedAt
				progress.ElapsedTime = time.Since(*extStatus.Status.StartedAt).Truncate(time.Second).String()
			}
		}
	}

	if err := s.templates.Render(w, "progress.html", progress); err != nil {
		s.logger.Printf("Failed to render progress partial: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// handlePartialStats handles the stats partial for HTMX
func (s *Server) handlePartialStats(w http.ResponseWriter, r *http.Request) {
	progress := s.state.GetCurrentProgress()

	// Check for external vectorize process via IPC
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	extStatus, err := s.GetExternalProcessStatus(ctx)
	if err != nil {
		s.logger.Printf("Failed to get external process status: %v", err)
	} else if extStatus != nil && extStatus.Status != nil {
		isExtRunning := extStatus.Status.State == "running" || extStatus.Status.State == "waiting"
		// If external process is active and webui is idle, update progress display
		if isExtRunning && progress.Status == StatusIdle {
			progress.Status = VectorizeStatus(extStatus.Status.State)
			if extStatus.Progress != nil {
				progress.TotalFiles = extStatus.Progress.TotalFiles
				progress.ProcessedFiles = extStatus.Progress.ProcessedFiles
				progress.SuccessCount = extStatus.Progress.SuccessCount
				progress.FailedCount = extStatus.Progress.FailedCount
				progress.PercentComplete = extStatus.Progress.Percentage
			}
			if extStatus.Status.StartedAt != nil {
				progress.StartTime = *extStatus.Status.StartedAt
				progress.ElapsedTime = time.Since(*extStatus.Status.StartedAt).Truncate(time.Second).String()
			}
		}
	}

	if err := s.templates.Render(w, "stats.html", progress); err != nil {
		s.logger.Printf("Failed to render stats partial: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// handlePartialFileList handles the file list partial for HTMX
func (s *Server) handlePartialFileList(w http.ResponseWriter, r *http.Request) {
	searchQuery := r.URL.Query().Get("search")
	files, err := s.getFileList(searchQuery)
	if err != nil {
		s.logger.Printf("Failed to get file list: %v", err)
		http.Error(w, "Failed to get files", http.StatusInternalServerError)
		return
	}

	if err := s.templates.Render(w, "file_list.html", files); err != nil {
		s.logger.Printf("Failed to render file list partial: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// handlePartialErrorList handles the error list partial for HTMX
func (s *Server) handlePartialErrorList(w http.ResponseWriter, r *http.Request) {
	errors := s.state.GetRecentErrors()
	if err := s.templates.Render(w, "error_list.html", errors); err != nil {
		s.logger.Printf("Failed to render error list partial: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// getFileList returns the list of files, optionally filtered by search query
func (s *Server) getFileList(searchQuery string) ([]FileListItem, error) {
	files, err := s.fileScanner.ScanDirectory(s.config.Directory)
	if err != nil {
		return nil, err
	}

	var result []FileListItem
	for _, f := range files {
		// Apply search filter
		if searchQuery != "" {
			if !containsIgnoreCase(f.Name, searchQuery) && !containsIgnoreCase(f.Path, searchQuery) {
				continue
			}
		}

		result = append(result, FileListItem{
			Path:       f.Path,
			Name:       f.Name,
			Size:       f.Size,
			ModTime:    f.ModTime,
			IsMarkdown: f.IsMarkdown,
			IsCSV:      f.IsCSV,
			Processed:  false, // TODO: Track processed status
		})
	}

	return result, nil
}

// getFileStats calculates file statistics
func (s *Server) getFileStats(files []FileListItem) *FileStats {
	stats := &FileStats{
		TotalFiles: len(files),
	}

	for _, f := range files {
		if f.IsMarkdown {
			stats.MarkdownCount++
		}
		if f.IsCSV {
			stats.CSVCount++
		}
	}

	return stats
}

// containsIgnoreCase checks if s contains substr (case-insensitive)
func containsIgnoreCase(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			len(substr) == 0 ||
			(len(s) > 0 && containsIgnoreCaseImpl(s, substr)))
}

func containsIgnoreCaseImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if equalFoldAt(s, i, substr) {
			return true
		}
	}
	return false
}

func equalFoldAt(s string, start int, substr string) bool {
	for j := 0; j < len(substr); j++ {
		c1 := s[start+j]
		c2 := substr[j]
		if c1 != c2 {
			// Simple ASCII case-insensitive comparison
			if c1 >= 'A' && c1 <= 'Z' {
				c1 += 'a' - 'A'
			}
			if c2 >= 'A' && c2 <= 'Z' {
				c2 += 'a' - 'A'
			}
			if c1 != c2 {
				return false
			}
		}
	}
	return true
}
