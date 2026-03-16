// Package domain defines the shared domain types used across vertical slices.
package domain

import (
	"fmt"
	"time"

	pkgconfig "github.com/ca-srg/ragent/internal/pkg/config"
)

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

type FileInfo struct {
	Path         string           `json:"path"`
	Name         string           `json:"name"`
	Size         int64            `json:"size"`
	ModTime      time.Time        `json:"mod_time"`
	IsMarkdown   bool             `json:"is_markdown"`
	IsCSV        bool             `json:"is_csv"`
	CSVRowIndex  int              `json:"csv_row_index,omitempty"`
	IsPDF        bool             `json:"is_pdf"`
	PDFPageIndex int              `json:"pdf_page_index,omitempty"`
	Content      string           `json:"content"`
	Metadata     DocumentMetadata `json:"metadata"`
	ContentHash  string           `json:"content_hash,omitempty"`
	SourceType   string           `json:"source_type,omitempty"`
	RawBytes     []byte           `json:"-"`
}

type VectorData struct {
	ID        string           `json:"id"`
	Embedding []float64        `json:"embedding"`
	Metadata  DocumentMetadata `json:"metadata"`
	Content   string           `json:"content"`
	CreatedAt time.Time        `json:"created_at"`
}

type ProcessingResult struct {
	ProcessedFiles           int               `json:"processed_files"`
	SuccessCount             int               `json:"success_count"`
	FailureCount             int               `json:"failure_count"`
	Errors                   []ProcessingError `json:"errors"`
	StartTime                time.Time         `json:"start_time"`
	EndTime                  time.Time         `json:"end_time"`
	Duration                 time.Duration     `json:"duration"`
	OpenSearchEnabled        bool              `json:"opensearch_enabled"`
	OpenSearchSuccessCount   int               `json:"opensearch_success_count"`
	OpenSearchFailureCount   int               `json:"opensearch_failure_count"`
	OpenSearchIndexedCount   int               `json:"opensearch_indexed_count"`
	OpenSearchSkippedCount   int               `json:"opensearch_skipped_count"`
	OpenSearchRetryCount     int               `json:"opensearch_retry_count"`
	OpenSearchProcessingTime time.Duration     `json:"opensearch_processing_time"`
}

type ProcessingError struct {
	Type       pkgconfig.ErrorType `json:"type"`
	Message    string              `json:"message"`
	FilePath   string              `json:"file_path"`
	Timestamp  time.Time           `json:"timestamp"`
	Retryable  bool                `json:"retryable"`
	RetryCount int                 `json:"retry_count"`
}

func (pe *ProcessingError) Error() string {
	return fmt.Sprintf("[%s] %s (file: %s)", pe.Type, pe.Message, pe.FilePath)
}

func (pe *ProcessingError) IsRetryable() bool {
	return pe.Retryable && pe.RetryCount < 3
}

func (pe *ProcessingError) IncrementRetry() {
	pe.RetryCount++
}
