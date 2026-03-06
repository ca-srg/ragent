// Package domain defines the core domain types for the ingestion slice.
// These types are shared by the ingestion package and all its sub-packages
// to avoid circular imports.
package domain

import (
	"fmt"
	"time"

	pkgconfig "github.com/ca-srg/ragent/internal/pkg/config"
)

// DocumentMetadata represents metadata extracted from markdown files
type DocumentMetadata struct {
	Title        string                 `json:"title"`
	Category     string                 `json:"category"`
	Tags         []string               `json:"tags"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
	Author       string                 `json:"author"`
	Reference    string                 `json:"reference"`
	Source       string                 `json:"source"`
	FilePath     string                 `json:"file_path"`
	WordCount    int                    `json:"word_count"`
	CustomFields map[string]interface{} `json:"custom_fields"`
}

// FileInfo represents information about a file to be processed (markdown, CSV, or PDF)
type FileInfo struct {
	Path         string           `json:"path"`
	Name         string           `json:"name"`
	Size         int64            `json:"size"`
	ModTime      time.Time        `json:"mod_time"`
	IsMarkdown   bool             `json:"is_markdown"`
	IsCSV        bool             `json:"is_csv"`
	CSVRowIndex  int              `json:"csv_row_index,omitempty"` // Row index for CSV files (1-based, excluding header)
	IsPDF        bool             `json:"is_pdf"`
	PDFPageIndex int              `json:"pdf_page_index,omitempty"` // Page index for PDF files (1-based)
	Content      string           `json:"content"`
	Metadata     DocumentMetadata `json:"metadata"`
	ContentHash  string           `json:"content_hash,omitempty"` // MD5 hash of content (hex format)
	SourceType   string           `json:"source_type,omitempty"`  // "local" or "s3"
	RawBytes     []byte           `json:"-"`                      // Raw bytes for binary files (PDFs from S3/GitHub)
}

// VectorData represents the embedding vector data for a document
type VectorData struct {
	ID        string           `json:"id"`
	Embedding []float64        `json:"embedding"`
	Metadata  DocumentMetadata `json:"metadata"`
	Content   string           `json:"content"`
	CreatedAt time.Time        `json:"created_at"`
}

// ProcessingResult represents the result of processing markdown files
type ProcessingResult struct {
	ProcessedFiles int               `json:"processed_files"`
	SuccessCount   int               `json:"success_count"`
	FailureCount   int               `json:"failure_count"`
	Errors         []ProcessingError `json:"errors"`
	StartTime      time.Time         `json:"start_time"`
	EndTime        time.Time         `json:"end_time"`
	Duration       time.Duration     `json:"duration"`
	// OpenSearch specific statistics
	OpenSearchEnabled        bool          `json:"opensearch_enabled"`
	OpenSearchSuccessCount   int           `json:"opensearch_success_count"`
	OpenSearchFailureCount   int           `json:"opensearch_failure_count"`
	OpenSearchIndexedCount   int           `json:"opensearch_indexed_count"`
	OpenSearchSkippedCount   int           `json:"opensearch_skipped_count"`
	OpenSearchRetryCount     int           `json:"opensearch_retry_count"`
	OpenSearchProcessingTime time.Duration `json:"opensearch_processing_time"`
}

// ProcessingError represents an error that occurred during processing
type ProcessingError struct {
	Type       pkgconfig.ErrorType `json:"type"`
	Message    string              `json:"message"`
	FilePath   string              `json:"file_path"`
	Timestamp  time.Time           `json:"timestamp"`
	Retryable  bool                `json:"retryable"`
	RetryCount int                 `json:"retry_count"`
}

// Error implements the error interface for ProcessingError
func (pe *ProcessingError) Error() string {
	return fmt.Sprintf("[%s] %s (file: %s)", pe.Type, pe.Message, pe.FilePath)
}

// IsRetryable returns whether this error type should be retried
func (pe *ProcessingError) IsRetryable() bool {
	return pe.Retryable && pe.RetryCount < 3 // Maximum 3 retries
}

// IncrementRetry increments the retry count
func (pe *ProcessingError) IncrementRetry() {
	pe.RetryCount++
}
