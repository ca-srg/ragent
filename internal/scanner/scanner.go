package scanner

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ca-srg/ragent/internal/types"
)

// Type alias for FileInfo
type FileInfo = types.FileInfo

// FileScanner implements the FileScanner interface for scanning markdown files
type FileScanner struct{}

// NewFileScanner creates a new FileScanner instance
func NewFileScanner() *FileScanner {
	return &FileScanner{}
}

// ScanDirectory scans a directory for supported files (markdown and CSV)
func (s *FileScanner) ScanDirectory(dirPath string) ([]*FileInfo, error) {
	if err := s.ValidateDirectory(dirPath); err != nil {
		return nil, fmt.Errorf("directory validation failed: %w", err)
	}

	var files []*FileInfo

	err := filepath.WalkDir(dirPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Skip files with permission errors but continue processing
			return nil
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Check if it's a supported file type
		if !s.IsSupportedFile(path) {
			return nil
		}

		// Get file info
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("failed to get file info for %s: %w", path, err)
		}

		// Create FileInfo struct
		fileInfo := &FileInfo{
			Path:       path,
			Name:       d.Name(),
			Size:       info.Size(),
			ModTime:    info.ModTime(),
			IsMarkdown: s.IsMarkdownFile(path),
			IsCSV:      s.IsCSVFile(path),
		}

		files = append(files, fileInfo)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to scan directory %s: %w", dirPath, err)
	}

	return files, nil
}

// ValidateDirectory checks if the directory exists and is readable
func (s *FileScanner) ValidateDirectory(dirPath string) error {
	info, err := os.Stat(dirPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("directory does not exist: %s", dirPath)
		}
		return fmt.Errorf("cannot access directory %s: %w", dirPath, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", dirPath)
	}

	// Test if directory is readable by trying to open it
	file, err := os.Open(dirPath)
	if err != nil {
		return fmt.Errorf("directory is not readable: %s (%w)", dirPath, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to close directory: %w", err)
	}

	return nil
}

// ReadFileContent reads and returns the content of a file
func (s *FileScanner) ReadFileContent(filePath string) (string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	return string(content), nil
}

// IsMarkdownFile checks if a file is a markdown file
func (s *FileScanner) IsMarkdownFile(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	return ext == ".md" || ext == ".markdown"
}

// IsCSVFile checks if a file is a CSV file
func (s *FileScanner) IsCSVFile(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	return ext == ".csv"
}

// IsSupportedFile checks if a file is a supported file type (markdown or CSV)
func (s *FileScanner) IsSupportedFile(filePath string) bool {
	return s.IsMarkdownFile(filePath) || s.IsCSVFile(filePath)
}

// LoadFileWithContent loads file info and reads its content
func (s *FileScanner) LoadFileWithContent(fileInfo *FileInfo) error {
	content, err := s.ReadFileContent(fileInfo.Path)
	if err != nil {
		return err
	}

	fileInfo.Content = content
	return nil
}

// FilterFilesBySize filters files by size constraints
func (s *FileScanner) FilterFilesBySize(files []*FileInfo, minSize, maxSize int64) []*FileInfo {
	var filtered []*FileInfo

	for _, file := range files {
		if file.Size >= minSize && file.Size <= maxSize {
			filtered = append(filtered, file)
		}
	}

	return filtered
}

// FilterFilesByPattern filters files by name pattern
func (s *FileScanner) FilterFilesByPattern(files []*FileInfo, pattern string) ([]*FileInfo, error) {
	var filtered []*FileInfo

	for _, file := range files {
		matched, err := filepath.Match(pattern, file.Name)
		if err != nil {
			return nil, fmt.Errorf("invalid pattern %s: %w", pattern, err)
		}

		if matched {
			filtered = append(filtered, file)
		}
	}

	return filtered, nil
}

// GetFileStats returns statistics about scanned files
func (s *FileScanner) GetFileStats(files []*FileInfo) map[string]interface{} {
	stats := make(map[string]interface{})

	if len(files) == 0 {
		stats["count"] = 0
		return stats
	}

	var totalSize int64
	for _, file := range files {
		totalSize += file.Size
	}

	stats["count"] = len(files)
	stats["total_size"] = totalSize
	stats["average_size"] = totalSize / int64(len(files))

	return stats
}

// ScanDirectoryWithStats scans directory and returns files with statistics
func (s *FileScanner) ScanDirectoryWithStats(dirPath string) ([]*FileInfo, map[string]interface{}, error) {
	files, err := s.ScanDirectory(dirPath)
	if err != nil {
		return nil, nil, err
	}

	stats := s.GetFileStats(files)
	return files, stats, nil
}
