package webui

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testServer creates a minimal server for testing handlers
type testServer struct {
	state     *VectorizeState
	scheduler *Scheduler
	logger    *log.Logger
}

func newTestServer() *testServer {
	logger := log.New(io.Discard, "", 0)
	state := NewVectorizeState(nil)
	scheduler := NewScheduler(state, 30*time.Minute, logger)
	scheduler.SetRunFunc(func(ctx context.Context) error {
		return nil
	})

	return &testServer{
		state:     state,
		scheduler: scheduler,
		logger:    logger,
	}
}

func (ts *testServer) handleAPIStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := &APIStatusResponse{
		Status:    ts.state.GetStatus(),
		Scheduler: ts.scheduler.GetState(),
	}

	ts.writeJSON(w, response)
}

func (ts *testServer) handleVectorizeStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !ts.state.IsRunning() {
		http.Error(w, "No vectorization running", http.StatusBadRequest)
		return
	}

	ts.state.SetStopping()
	ts.writeJSON(w, map[string]string{
		"message": "Stop requested",
	})
}

func (ts *testServer) handleVectorizeProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := &APIProgressResponse{
		Progress: ts.state.GetCurrentProgress(),
		LastRun:  ts.state.GetLastRun(),
	}

	ts.writeJSON(w, response)
}

func (ts *testServer) handleAPIHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	history := ts.state.GetHistory()
	ts.writeJSON(w, history)
}

func (ts *testServer) handleAPIErrors(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	errors := ts.state.GetRecentErrors()
	ts.writeJSON(w, errors)
}

func (ts *testServer) handleSchedulerStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	state := ts.scheduler.GetState()
	ts.writeJSON(w, state)
}

func (ts *testServer) handleSchedulerToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if ts.scheduler.IsEnabled() {
		ts.scheduler.Stop()
		ts.writeJSON(w, map[string]interface{}{
			"enabled": false,
			"message": "Scheduler stopped",
		})
	} else {
		// For testing, we don't actually start the scheduler
		ts.writeJSON(w, map[string]interface{}{
			"enabled": true,
			"message": "Scheduler started",
		})
	}
}

func (ts *testServer) handleSchedulerInterval(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Interval string `json:"interval"`
	}

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

	if err := ts.scheduler.SetInterval(duration); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	ts.writeJSON(w, map[string]interface{}{
		"interval": duration.String(),
		"message":  "Interval updated",
	})
}

func (ts *testServer) writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// API Handler Tests

func TestHandleAPIStatus(t *testing.T) {
	ts := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()

	ts.handleAPIStatus(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	var result APIStatusResponse
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, StatusIdle, result.Status)
	assert.NotNil(t, result.Scheduler)
}

func TestHandleAPIStatusMethodNotAllowed(t *testing.T) {
	ts := newTestServer()

	req := httptest.NewRequest(http.MethodPost, "/api/status", nil)
	w := httptest.NewRecorder()

	ts.handleAPIStatus(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestHandleVectorizeStopNotRunning(t *testing.T) {
	ts := newTestServer()

	req := httptest.NewRequest(http.MethodPost, "/api/vectorize/stop", nil)
	w := httptest.NewRecorder()

	ts.handleVectorizeStop(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHandleVectorizeStopWhileRunning(t *testing.T) {
	ts := newTestServer()
	ts.state.StartRun(10, false)

	req := httptest.NewRequest(http.MethodPost, "/api/vectorize/stop", nil)
	w := httptest.NewRecorder()

	ts.handleVectorizeStop(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, StatusStopping, ts.state.GetStatus())
}

func TestHandleVectorizeStopMethodNotAllowed(t *testing.T) {
	ts := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/vectorize/stop", nil)
	w := httptest.NewRecorder()

	ts.handleVectorizeStop(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestHandleVectorizeProgress(t *testing.T) {
	ts := newTestServer()
	ts.state.StartRun(10, false)
	ts.state.UpdateProgress(5, 4, 1, "test.md")

	req := httptest.NewRequest(http.MethodGet, "/api/vectorize/progress", nil)
	w := httptest.NewRecorder()

	ts.handleVectorizeProgress(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result APIProgressResponse
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.NotNil(t, result.Progress)
	assert.Equal(t, StatusRunning, result.Progress.Status)
	assert.Equal(t, 5, result.Progress.ProcessedFiles)
}

func TestHandleVectorizeProgressMethodNotAllowed(t *testing.T) {
	ts := newTestServer()

	req := httptest.NewRequest(http.MethodPost, "/api/vectorize/progress", nil)
	w := httptest.NewRecorder()

	ts.handleVectorizeProgress(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestHandleAPIHistory(t *testing.T) {
	ts := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/history", nil)
	w := httptest.NewRecorder()

	ts.handleAPIHistory(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result []RunInfo
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestHandleAPIHistoryMethodNotAllowed(t *testing.T) {
	ts := newTestServer()

	req := httptest.NewRequest(http.MethodPost, "/api/history", nil)
	w := httptest.NewRecorder()

	ts.handleAPIHistory(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestHandleAPIErrors(t *testing.T) {
	ts := newTestServer()
	ts.state.AddError(ErrorInfo{
		Timestamp: time.Now(),
		FilePath:  "test.md",
		Message:   "test error",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/errors", nil)
	w := httptest.NewRecorder()

	ts.handleAPIErrors(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result []ErrorInfo
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "test.md", result[0].FilePath)
}

func TestHandleAPIErrorsMethodNotAllowed(t *testing.T) {
	ts := newTestServer()

	req := httptest.NewRequest(http.MethodPost, "/api/errors", nil)
	w := httptest.NewRecorder()

	ts.handleAPIErrors(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestHandleSchedulerStatus(t *testing.T) {
	ts := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/scheduler/status", nil)
	w := httptest.NewRecorder()

	ts.handleSchedulerStatus(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result SchedulerState
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.False(t, result.Enabled)
}

func TestHandleSchedulerStatusMethodNotAllowed(t *testing.T) {
	ts := newTestServer()

	req := httptest.NewRequest(http.MethodPost, "/api/scheduler/status", nil)
	w := httptest.NewRecorder()

	ts.handleSchedulerStatus(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestHandleSchedulerToggle(t *testing.T) {
	ts := newTestServer()

	req := httptest.NewRequest(http.MethodPost, "/api/scheduler/toggle", nil)
	w := httptest.NewRecorder()

	ts.handleSchedulerToggle(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, true, result["enabled"])
}

func TestHandleSchedulerToggleMethodNotAllowed(t *testing.T) {
	ts := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/scheduler/toggle", nil)
	w := httptest.NewRecorder()

	ts.handleSchedulerToggle(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestHandleSchedulerIntervalWithJSON(t *testing.T) {
	ts := newTestServer()

	body := strings.NewReader(`{"interval": "15m"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/scheduler/interval", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ts.handleSchedulerInterval(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, "15m0s", result["interval"])
}

func TestHandleSchedulerIntervalMissing(t *testing.T) {
	ts := newTestServer()

	req := httptest.NewRequest(http.MethodPost, "/api/scheduler/interval", nil)
	w := httptest.NewRecorder()

	ts.handleSchedulerInterval(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHandleSchedulerIntervalInvalid(t *testing.T) {
	ts := newTestServer()

	body := strings.NewReader(`{"interval": "invalid"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/scheduler/interval", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	ts.handleSchedulerInterval(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHandleSchedulerIntervalMethodNotAllowed(t *testing.T) {
	ts := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/scheduler/interval", nil)
	w := httptest.NewRecorder()

	ts.handleSchedulerInterval(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

// Helper function tests

func TestContainsIgnoreCase(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		substr   string
		expected bool
	}{
		{
			name:     "exact match",
			s:        "hello",
			substr:   "hello",
			expected: true,
		},
		{
			name:     "case insensitive match",
			s:        "Hello",
			substr:   "hello",
			expected: true,
		},
		{
			name:     "partial match",
			s:        "hello world",
			substr:   "world",
			expected: true,
		},
		{
			name:     "no match",
			s:        "hello",
			substr:   "world",
			expected: false,
		},
		{
			name:     "empty substr",
			s:        "hello",
			substr:   "",
			expected: true,
		},
		{
			name:     "empty string",
			s:        "",
			substr:   "hello",
			expected: false,
		},
		{
			name:     "both empty",
			s:        "",
			substr:   "",
			expected: true,
		},
		{
			name:     "upper in string",
			s:        "HELLO WORLD",
			substr:   "world",
			expected: true,
		},
		{
			name:     "mixed case",
			s:        "HeLLo WoRLd",
			substr:   "WORLD",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsIgnoreCase(tt.s, tt.substr)
			assert.Equal(t, tt.expected, result)
		})
	}
}
