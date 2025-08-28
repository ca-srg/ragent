package types

import (
	"fmt"
	"time"
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

// FileInfo represents information about a markdown file to be processed
type FileInfo struct {
	Path       string           `json:"path"`
	Name       string           `json:"name"`
	Size       int64            `json:"size"`
	ModTime    time.Time        `json:"mod_time"`
	IsMarkdown bool             `json:"is_markdown"`
	Content    string           `json:"content"`
	Metadata   DocumentMetadata `json:"metadata"`
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
}

// ProcessingError represents an error that occurred during processing
type ProcessingError struct {
	Type       ErrorType `json:"type"`
	Message    string    `json:"message"`
	FilePath   string    `json:"file_path"`
	Timestamp  time.Time `json:"timestamp"`
	Retryable  bool      `json:"retryable"`
	RetryCount int       `json:"retry_count"`
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

// ErrorType represents the type of error that occurred
type ErrorType string

const (
	ErrorTypeFileRead       ErrorType = "file_read"
	ErrorTypeMetadata       ErrorType = "metadata_extraction"
	ErrorTypeEmbedding      ErrorType = "embedding_generation"
	ErrorTypeS3Upload       ErrorType = "s3_upload"
	ErrorTypeNetworkTimeout ErrorType = "network_timeout"
	ErrorTypeRateLimit      ErrorType = "rate_limit"
	ErrorTypeValidation     ErrorType = "validation"
	ErrorTypeUnknown        ErrorType = "unknown"
)

// Config represents the vectorizer configuration
type Config struct {
	AWSS3VectorBucket string        `json:"aws_s3_vector_bucket"`
	AWSS3VectorIndex  string        `json:"aws_s3_vector_index"`
	AWSS3Region       string        `json:"aws_s3_region"`
	ChatModel         string        `json:"chat_model"`
	Concurrency       int           `json:"concurrency"`
	RetryAttempts     int           `json:"retry_attempts"`
	RetryDelay        time.Duration `json:"retry_delay"`
	ExcludeCategories []string      `json:"exclude_categories"`
}

// QueryResult represents a single result from a vector query
type QueryResult struct {
	Key      string                 `json:"key"`
	Distance float64                `json:"distance"`
	Metadata map[string]interface{} `json:"metadata"`
	Content  string                 `json:"content,omitempty"`
}

// QueryVectorsResult represents the complete result from a vector query
type QueryVectorsResult struct {
	Results    []QueryResult `json:"results"`
	TotalCount int           `json:"total_count"`
	Query      string        `json:"query"`
	TopK       int           `json:"top_k"`
}
