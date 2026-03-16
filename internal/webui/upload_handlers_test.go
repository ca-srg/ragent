package webui

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	domain "github.com/ca-srg/ragent/internal/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testUploadFile struct {
	Name    string
	Content []byte
}

type mockFileScanner struct{}

func (m *mockFileScanner) ScanDirectory(_ string) ([]*domain.FileInfo, error) {
	return nil, nil
}

type mockVectorizer struct{}

func (m *mockVectorizer) VectorizeFiles(_ context.Context, _ []*domain.FileInfo, _ bool) (*domain.ProcessingResult, error) {
	return &domain.ProcessingResult{}, nil
}

type recordingVectorizer struct {
	mu    sync.Mutex
	calls int
}

func (m *recordingVectorizer) VectorizeFiles(_ context.Context, _ []*domain.FileInfo, _ bool) (*domain.ProcessingResult, error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()

	return &domain.ProcessingResult{}, nil
}

func (m *recordingVectorizer) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.calls
}

func newUploadTestServer(t *testing.T, dir string) *Server {
	t.Helper()

	cfg := DefaultServerConfig()
	cfg.Directory = dir

	deps := &Dependencies{
		FileScanner: &mockFileScanner{},
		Vectorizer:  &mockVectorizer{},
	}

	srv, err := NewServer(cfg, deps, nil)
	require.NoError(t, err)

	return srv
}

func createMultipartRequest(t *testing.T, files []testUploadFile) *http.Request {
	t.Helper()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	for _, f := range files {
		part, err := writer.CreateFormFile("files", f.Name)
		require.NoError(t, err)

		_, err = part.Write(f.Content)
		require.NoError(t, err)
	}

	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	return req
}

func TestUploadTypes(t *testing.T) {
	result := UploadResult{
		FileName: "test.md",
		Status:   "saved",
		Message:  "ok",
		Size:     100,
	}
	assert.Equal(t, "test.md", result.FileName)
	assert.Equal(t, int64(100), result.Size)

	response := UploadResponse{
		Results:            []UploadResult{result},
		SavedCount:         1,
		RejectedCount:      0,
		VectorizeTriggered: true,
	}
	assert.Len(t, response.Results, 1)
	assert.Equal(t, 1, response.SavedCount)
	assert.True(t, response.VectorizeTriggered)
	assert.Equal(t, int64(50*1024*1024), MaxUploadSize)
	assert.Equal(t, []string{".md", ".csv", ".pdf"}, AllowedExtensions)
}

func TestHandleUpload_MethodNotAllowed(t *testing.T) {
	srv := newUploadTestServer(t, t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/upload", nil)
	w := httptest.NewRecorder()

	srv.handleUpload(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestHandleUpload_NoFiles(t *testing.T) {
	srv := newUploadTestServer(t, t.TempDir())
	req := createMultipartRequest(t, nil)
	w := httptest.NewRecorder()

	srv.handleUpload(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestHandleUpload_InvalidExtension(t *testing.T) {
	srv := newUploadTestServer(t, t.TempDir())
	req := createMultipartRequest(t, []testUploadFile{{
		Name:    "malware.exe",
		Content: []byte("binary"),
	}})
	w := httptest.NewRecorder()

	srv.handleUpload(w, req)

	resp := w.Result()
	var result UploadResponse
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Greater(t, result.RejectedCount, 0)
}

func TestHandleUpload_OversizeFile(t *testing.T) {
	srv := newUploadTestServer(t, t.TempDir())
	oversizeContent := make([]byte, MaxUploadSize+1)
	req := createMultipartRequest(t, []testUploadFile{{
		Name:    "large.pdf",
		Content: oversizeContent,
	}})
	w := httptest.NewRecorder()

	srv.handleUpload(w, req)

	resp := w.Result()
	var result UploadResponse
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Greater(t, result.RejectedCount, 0)
}

func TestHandleUpload_PathTraversal(t *testing.T) {
	srv := newUploadTestServer(t, t.TempDir())
	req := createMultipartRequest(t, []testUploadFile{{
		Name:    "../escape.md",
		Content: []byte("# escaped"),
	}})
	w := httptest.NewRecorder()

	srv.handleUpload(w, req)

	resp := w.Result()
	var result UploadResponse
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)

	if result.RejectedCount == 0 {
		require.NotEmpty(t, result.Results)
		for _, item := range result.Results {
			assert.NotContains(t, item.FileName, "..")
			assert.False(t, strings.ContainsAny(item.FileName, `/\\`))
		}
	} else {
		assert.Greater(t, result.RejectedCount, 0)
	}
}

func TestHandleUpload_EmptyFile(t *testing.T) {
	srv := newUploadTestServer(t, t.TempDir())
	req := createMultipartRequest(t, []testUploadFile{{
		Name:    "empty.md",
		Content: []byte{},
	}})
	w := httptest.NewRecorder()

	srv.handleUpload(w, req)

	resp := w.Result()
	var result UploadResponse
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Greater(t, result.RejectedCount, 0)
}

func TestHandleUpload_CaseInsensitiveExtension(t *testing.T) {
	srv := newUploadTestServer(t, t.TempDir())
	req := createMultipartRequest(t, []testUploadFile{{
		Name:    "report.PDF",
		Content: []byte("pdf-bytes"),
	}})
	w := httptest.NewRecorder()

	srv.handleUpload(w, req)

	resp := w.Result()
	var result UploadResponse
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Greater(t, result.SavedCount, 0)
}

func TestHandleUpload_SingleFileSuccess(t *testing.T) {
	dir := t.TempDir()
	vectorizer := &recordingVectorizer{}

	cfg := DefaultServerConfig()
	cfg.Directory = dir

	srv, err := NewServer(cfg, &Dependencies{
		FileScanner: &mockFileScanner{},
		Vectorizer:  vectorizer,
	}, nil)
	require.NoError(t, err)

	req := createMultipartRequest(t, []testUploadFile{{
		Name:    "single.md",
		Content: []byte("# single"),
	}})
	w := httptest.NewRecorder()

	srv.handleUpload(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result UploadResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, 1, result.SavedCount)
	assert.True(t, result.VectorizeTriggered)
	assert.Eventually(t, func() bool {
		return vectorizer.CallCount() == 1
	}, time.Second, 10*time.Millisecond)
}

func TestHandleUpload_MultipleFilesSuccess(t *testing.T) {
	srv := newUploadTestServer(t, t.TempDir())
	req := createMultipartRequest(t, []testUploadFile{
		{Name: "first.md", Content: []byte("# first")},
		{Name: "second.csv", Content: []byte("a,b\n1,2")},
		{Name: "third.pdf", Content: []byte("%PDF-1.4")},
	})
	w := httptest.NewRecorder()

	srv.handleUpload(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result UploadResponse
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, 3, result.SavedCount)
}

func TestHandleUpload_PartialSuccess(t *testing.T) {
	srv := newUploadTestServer(t, t.TempDir())
	req := createMultipartRequest(t, []testUploadFile{
		{Name: "good.md", Content: []byte("# good")},
		{Name: "table.csv", Content: []byte("a,b\n1,2")},
		{Name: "bad.exe", Content: []byte("boom")},
	})
	w := httptest.NewRecorder()

	srv.handleUpload(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result UploadResponse
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, 2, result.SavedCount)
	assert.Equal(t, 1, result.RejectedCount)
}

func TestHandleUpload_VectorizeConflict(t *testing.T) {
	dir := t.TempDir()
	vectorizer := &recordingVectorizer{}

	cfg := DefaultServerConfig()
	cfg.Directory = dir

	srv, err := NewServer(cfg, &Dependencies{
		FileScanner: &mockFileScanner{},
		Vectorizer:  vectorizer,
	}, nil)
	require.NoError(t, err)

	srv.state.StartRun(1, false)

	req := createMultipartRequest(t, []testUploadFile{{
		Name:    "test.md",
		Content: []byte("# test"),
	}})
	w := httptest.NewRecorder()

	srv.handleUpload(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result UploadResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Greater(t, result.SavedCount, 0)
	assert.False(t, result.VectorizeTriggered)
	assert.Equal(t, 0, vectorizer.CallCount())
}

func TestHandleUpload_DuplicateBasename(t *testing.T) {
	srv := newUploadTestServer(t, t.TempDir())
	req := createMultipartRequest(t, []testUploadFile{
		{Name: "duplicate.md", Content: []byte("# first")},
		{Name: "duplicate.md", Content: []byte("# second")},
	})
	w := httptest.NewRecorder()

	srv.handleUpload(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result UploadResponse
	err := json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Greater(t, result.SavedCount, 0)
	assert.LessOrEqual(t, result.SavedCount, 2)
}

func TestHandleUpload_FileSavedToCorrectPath(t *testing.T) {
	dir := t.TempDir()
	srv := newUploadTestServer(t, dir)

	req := createMultipartRequest(t, []testUploadFile{{
		Name:    "saved-file.md",
		Content: []byte("# saved"),
	}})
	w := httptest.NewRecorder()

	srv.handleUpload(w, req)

	expectedPath := filepath.Join(dir, "saved-file.md")
	_, err := os.Stat(expectedPath)
	assert.NoError(t, err, "ファイルが保存されていない: %s", expectedPath)
}

func TestUploadIntegration_FullFlow(t *testing.T) {
	dir := t.TempDir()
	vectorizer := &recordingVectorizer{}

	cfg := DefaultServerConfig()
	cfg.Directory = dir

	srv, err := NewServer(cfg, &Dependencies{
		FileScanner: &mockFileScanner{},
		Vectorizer:  vectorizer,
	}, nil)
	require.NoError(t, err)

	ts := httptest.NewServer(srv.setupRoutes())
	defer ts.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("files", "integration.md")
	require.NoError(t, err)

	_, err = part.Write([]byte("# integration test"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/upload", body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result UploadResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, 1, result.SavedCount)
	assert.Equal(t, 0, result.RejectedCount)
	assert.True(t, result.VectorizeTriggered)
	assert.Len(t, result.Results, 1)
	assert.Equal(t, "integration.md", result.Results[0].FileName)
	assert.Equal(t, "saved", result.Results[0].Status)

	expectedPath := filepath.Join(dir, "integration.md")
	content, err := os.ReadFile(expectedPath)
	require.NoError(t, err)
	assert.Equal(t, "# integration test", string(content))
	assert.Eventually(t, func() bool {
		return vectorizer.CallCount() == 1
	}, time.Second, 10*time.Millisecond)
}

func TestUploadIntegration_RouteRegistered(t *testing.T) {
	srv := newUploadTestServer(t, t.TempDir())

	ts := httptest.NewServer(srv.setupRoutes())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/upload")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}
