package webui

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testServer struct {
	state  *VectorizeState
	logger *log.Logger
}

func newTestServer() *testServer {
	logger := log.New(io.Discard, "", 0)
	state := NewVectorizeState(nil)

	return &testServer{
		state:  state,
		logger: logger,
	}
}

func (ts *testServer) handleAPIStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := &APIStatusResponse{
		Status: ts.state.GetStatus(),
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

func (ts *testServer) writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}

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
}

func TestHandleAPIStatusMethodNotAllowed(t *testing.T) {
	ts := newTestServer()

	req := httptest.NewRequest(http.MethodPost, "/api/status", nil)
	w := httptest.NewRecorder()

	ts.handleAPIStatus(w, req)

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
