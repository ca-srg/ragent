package ipc

import "time"

// ProcessState represents the current state of the vectorize process
type ProcessState string

const (
	StateIdle     ProcessState = "idle"
	StateRunning  ProcessState = "running"
	StateWaiting  ProcessState = "waiting" // follow mode: waiting for next run
	StateStopping ProcessState = "stopping"
	StateError    ProcessState = "error"
)

// Method names for JSON-RPC
const (
	MethodStatusGet   = "status.get"
	MethodProgressGet = "progress.get"
	MethodControlStop = "control.stop"
)

// StatusResponse is the response for "status.get" method
type StatusResponse struct {
	State     ProcessState `json:"state"`
	StartedAt *time.Time   `json:"started_at,omitempty"`
	Error     string       `json:"error,omitempty"`
	PID       int          `json:"pid"`
	Version   string       `json:"version,omitempty"`
	DryRun    bool         `json:"dry_run,omitempty"`
}

// ProgressResponse is the response for "progress.get" method
type ProgressResponse struct {
	TotalFiles     int     `json:"total_files"`
	ProcessedFiles int     `json:"processed_files"`
	SuccessCount   int     `json:"success_count"`
	FailedCount    int     `json:"failed_count"`
	CurrentFile    string  `json:"current_file,omitempty"`
	Percentage     float64 `json:"percentage"`
	FilesPerSecond float64 `json:"files_per_second,omitempty"`
	ETASeconds     float64 `json:"eta_seconds,omitempty"`
	ElapsedSeconds float64 `json:"elapsed_seconds,omitempty"`
}

// StopParams is the params for "control.stop" method
type StopParams struct {
	Force   bool `json:"force"`   // Force immediate stop
	Timeout int  `json:"timeout"` // Graceful shutdown timeout in seconds
}

// StopResponse is the response for "control.stop" method
type StopResponse struct {
	Acknowledged bool   `json:"acknowledged"`
	Message      string `json:"message"`
}

// FullStatusResponse combines status and progress information
type FullStatusResponse struct {
	Status   *StatusResponse   `json:"status"`
	Progress *ProgressResponse `json:"progress,omitempty"`
}
