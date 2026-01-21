package webui

import (
	"time"

	"github.com/ca-srg/ragent/internal/types"
)

// VectorizeStatus represents the current state of vectorization
type VectorizeStatus string

const (
	StatusIdle     VectorizeStatus = "idle"
	StatusRunning  VectorizeStatus = "running"
	StatusError    VectorizeStatus = "error"
	StatusStopping VectorizeStatus = "stopping"
)

// SSE Event types
const (
	EventTypeVectorizeStarted   = "vectorize_started"
	EventTypeVectorizeProgress  = "vectorize_progress"
	EventTypeVectorizeCompleted = "vectorize_completed"
	EventTypeVectorizeFailed    = "vectorize_failed"
	EventTypeFileProcessed      = "file_processed"
	EventTypeFileError          = "file_error"
	EventTypeSchedulerTick      = "scheduler_tick"
	EventTypeHeartbeat          = "heartbeat"
)

// VectorizeProgressEvent represents progress information for SSE
type VectorizeProgressEvent struct {
	Status          VectorizeStatus `json:"status"`
	TotalFiles      int             `json:"total_files"`
	ProcessedFiles  int             `json:"processed_files"`
	SuccessCount    int             `json:"success_count"`
	FailedCount     int             `json:"failed_count"`
	PercentComplete float64         `json:"percent_complete"`
	CurrentFile     string          `json:"current_file,omitempty"`
	StartTime       time.Time       `json:"start_time"`
	ElapsedTime     string          `json:"elapsed_time"`
	EstimatedRemain string          `json:"estimated_remain,omitempty"`
}

// RunInfo represents information about a vectorization run
type RunInfo struct {
	ID             string          `json:"id"`
	StartTime      time.Time       `json:"start_time"`
	EndTime        time.Time       `json:"end_time,omitempty"`
	Status         VectorizeStatus `json:"status"`
	TotalFiles     int             `json:"total_files"`
	ProcessedFiles int             `json:"processed_files"`
	SuccessCount   int             `json:"success_count"`
	FailedCount    int             `json:"failed_count"`
	Errors         []ErrorInfo     `json:"errors,omitempty"`
	DryRun         bool            `json:"dry_run"`
}

// ErrorInfo represents error information
type ErrorInfo struct {
	Timestamp time.Time       `json:"timestamp"`
	FilePath  string          `json:"file_path"`
	ErrorType types.ErrorType `json:"error_type"`
	Message   string          `json:"message"`
	Retryable bool            `json:"retryable"`
}

// SchedulerState represents the state of the follow mode scheduler
type SchedulerState struct {
	Enabled   bool          `json:"enabled"`
	Interval  time.Duration `json:"interval"`
	NextRunAt time.Time     `json:"next_run_at,omitempty"`
	LastRunAt time.Time     `json:"last_run_at,omitempty"`
}

// SSEEvent represents a Server-Sent Event
type SSEEvent struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data"`
}

// FileStats represents file statistics
type FileStats struct {
	TotalFiles    int `json:"total_files"`
	MarkdownCount int `json:"markdown_count"`
	CSVCount      int `json:"csv_count"`
}

// FileListItem represents a file item for display
type FileListItem struct {
	Path       string    `json:"path"`
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	ModTime    time.Time `json:"mod_time"`
	IsMarkdown bool      `json:"is_markdown"`
	IsCSV      bool      `json:"is_csv"`
	Processed  bool      `json:"processed"`
}

// DashboardData represents data for the dashboard page
type DashboardData struct {
	ActivePage   string
	State        *VectorizeProgressEvent
	Scheduler    *SchedulerState
	RecentErrors []ErrorInfo
	LastRun      *RunInfo
}

// FilesPageData represents data for the files page
type FilesPageData struct {
	ActivePage    string
	Files         []FileListItem
	TotalFiles    int
	MarkdownCount int
	CSVCount      int
	SearchQuery   string
}

// HistoryPageData represents data for the history page
type HistoryPageData struct {
	ActivePage string
	History    []RunInfo
}

// APIStatusResponse represents the status API response
type APIStatusResponse struct {
	Status          VectorizeStatus        `json:"status"`
	Scheduler       *SchedulerState        `json:"scheduler"`
	ExternalProcess *ExternalProcessStatus `json:"external_process,omitempty"`
}

// ExternalProcessStatus represents the status of an external vectorize process
type ExternalProcessStatus struct {
	Running    bool    `json:"running"`
	State      string  `json:"state"`
	PID        int     `json:"pid,omitempty"`
	TotalFiles int     `json:"total_files,omitempty"`
	Processed  int     `json:"processed,omitempty"`
	Percentage float64 `json:"percentage,omitempty"`
}

// APIProgressResponse represents the progress API response
type APIProgressResponse struct {
	Progress *VectorizeProgressEvent `json:"progress,omitempty"`
	LastRun  *RunInfo                `json:"last_run,omitempty"`
}
