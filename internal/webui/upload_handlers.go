package webui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	domain "github.com/ca-srg/ragent/internal/pkg/domain"
)

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseMultipartForm(MaxRequestSize); err != nil {
		http.Error(w, "Invalid multipart form", http.StatusBadRequest)
		return
	}

	secret, err := parseUploadSecretFlag(r.FormValue("secret"))
	if err != nil {
		http.Error(w, "Invalid secret flag", http.StatusBadRequest)
		return
	}

	if r.MultipartForm != nil {
		defer func() {
			if err := r.MultipartForm.RemoveAll(); err != nil {
				s.logger.Printf("Failed to cleanup multipart form: %v", err)
			}
		}()
	}

	fileHeaders := r.MultipartForm.File["files"]
	if len(fileHeaders) == 0 {
		http.Error(w, "No files uploaded", http.StatusBadRequest)
		return
	}

	if err := os.MkdirAll(s.config.Directory, 0o755); err != nil {
		http.Error(w, "Failed to prepare upload directory", http.StatusInternalServerError)
		return
	}

	response := UploadResponse{
		Results: make([]UploadResult, 0, len(fileHeaders)),
	}
	fileInfos := make([]*domain.FileInfo, 0, len(fileHeaders))

	for _, fileHeader := range fileHeaders {
		sanitizedName, err := sanitizeFilename(fileHeader.Filename)
		if err != nil {
			response.Results = append(response.Results, UploadResult{
				FileName: fileHeader.Filename,
				Status:   "rejected",
				Message:  err.Error(),
				Size:     fileHeader.Size,
			})
			response.RejectedCount++
			continue
		}

		if !isAllowedExtension(sanitizedName) {
			response.Results = append(response.Results, UploadResult{
				FileName: sanitizedName,
				Status:   "rejected",
				Message:  fmt.Sprintf("unsupported file extension: %s", filepath.Ext(sanitizedName)),
				Size:     fileHeader.Size,
			})
			response.RejectedCount++
			continue
		}

		if fileHeader.Size > MaxUploadSize {
			response.Results = append(response.Results, UploadResult{
				FileName: sanitizedName,
				Status:   "rejected",
				Message:  "file exceeds maximum upload size",
				Size:     fileHeader.Size,
			})
			response.RejectedCount++
			continue
		}

		if fileHeader.Size == 0 {
			response.Results = append(response.Results, UploadResult{
				FileName: sanitizedName,
				Status:   "rejected",
				Message:  "empty files are not allowed",
				Size:     fileHeader.Size,
			})
			response.RejectedCount++
			continue
		}

		src, err := fileHeader.Open()
		if err != nil {
			response.Results = append(response.Results, UploadResult{
				FileName: sanitizedName,
				Status:   "error",
				Message:  "failed to open uploaded file",
				Size:     fileHeader.Size,
			})
			continue
		}

		contentBytes, readErr := io.ReadAll(src)
		closeErr := src.Close()
		if readErr != nil {
			response.Results = append(response.Results, UploadResult{
				FileName: sanitizedName,
				Status:   "error",
				Message:  "failed to read uploaded file",
				Size:     fileHeader.Size,
			})
			continue
		}
		if closeErr != nil {
			s.logger.Printf("Failed to close uploaded file %q: %v", sanitizedName, closeErr)
		}

		destinationPath := filepath.Join(s.config.Directory, sanitizedName)
		dst, err := os.Create(destinationPath)
		if err != nil {
			response.Results = append(response.Results, UploadResult{
				FileName: sanitizedName,
				Status:   "error",
				Message:  "failed to create destination file",
				Size:     fileHeader.Size,
			})
			continue
		}

		_, copyErr := io.Copy(dst, bytes.NewReader(contentBytes))
		closeDstErr := dst.Close()
		if copyErr != nil {
			response.Results = append(response.Results, UploadResult{
				FileName: sanitizedName,
				Status:   "error",
				Message:  "failed to save uploaded file",
				Size:     fileHeader.Size,
			})
			_ = os.Remove(destinationPath)
			continue
		}
		if closeDstErr != nil {
			response.Results = append(response.Results, UploadResult{
				FileName: sanitizedName,
				Status:   "error",
				Message:  "failed to finalize uploaded file",
				Size:     fileHeader.Size,
			})
			_ = os.Remove(destinationPath)
			continue
		}

		ext := strings.ToLower(filepath.Ext(sanitizedName))
		fileInfo := &domain.FileInfo{
			Path:       destinationPath,
			Name:       sanitizedName,
			Size:       fileHeader.Size,
			Content:    string(contentBytes),
			IsMarkdown: ext == ".md",
			IsCSV:      ext == ".csv",
			IsPDF:      ext == ".pdf",
			SourceType: "upload",
			Metadata: domain.DocumentMetadata{
				Secret: secret,
			},
		}
		if ext == ".pdf" {
			fileInfo.RawBytes = contentBytes
			fileInfo.Content = ""
		}

		fileInfos = append(fileInfos, fileInfo)
		response.Results = append(response.Results, UploadResult{
			FileName: sanitizedName,
			Status:   "saved",
			Message:  "file uploaded successfully",
			Size:     fileHeader.Size,
		})
		response.SavedCount++
	}

	if len(fileInfos) > 0 && !s.state.IsRunning() {
		response.VectorizeTriggered = true
		go func(uploaded []*domain.FileInfo) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			s.vectorizeUploadedFiles(ctx, uploaded)
		}(fileInfos)
	}

	s.writeJSON(w, response)
}

func sanitizeFilename(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", fmt.Errorf("filename is required")
	}

	base := filepath.Base(trimmed)
	if base == "." || base == "" {
		return "", fmt.Errorf("invalid filename")
	}
	if strings.Contains(base, "..") {
		return "", fmt.Errorf("invalid filename")
	}
	if strings.ContainsAny(base, `/\\`) {
		return "", fmt.Errorf("invalid filename")
	}

	return base, nil
}

func isAllowedExtension(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	for _, allowedExt := range AllowedExtensions {
		if ext == allowedExt {
			return true
		}
	}
	return false
}

func (s *Server) vectorizeUploadedFiles(ctx context.Context, fileInfos []*domain.FileInfo) {
	if len(fileInfos) == 0 {
		return
	}
	if s.state.IsRunning() {
		return
	}

	runID := s.state.StartRun(len(fileInfos), false)
	s.logger.Printf("Starting upload vectorization run %s with %d files", runID, len(fileInfos))

	result, err := s.vectorizer.VectorizeFiles(ctx, fileInfos, false)
	if err != nil {
		s.state.FailRun(err)
		s.logger.Printf("Upload vectorization failed: %v", err)
		return
	}

	s.state.CompleteRun(result)
	s.logger.Printf("Upload vectorization completed: %d processed, %d success, %d failed",
		result.ProcessedFiles, result.SuccessCount, result.FailureCount)
}

func parseUploadSecretFlag(rawValue string) (bool, error) {
	trimmed := strings.TrimSpace(rawValue)
	if trimmed == "" {
		return false, nil
	}

	secret, err := strconv.ParseBool(trimmed)
	if err != nil {
		return false, err
	}

	return secret, nil
}
