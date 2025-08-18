package vectorizer

import (
	"fmt"
	"strings"
	"time"
)

// NewProcessingError creates a new ProcessingError with the given parameters
func NewProcessingError(errorType ErrorType, message, filePath string) *ProcessingError {
	return &ProcessingError{
		Type:       errorType,
		Message:    message,
		FilePath:   filePath,
		Timestamp:  time.Now(),
		Retryable:  isRetryableError(errorType),
		RetryCount: 0,
	}
}

// isRetryableError determines if an error type is retryable
func isRetryableError(errorType ErrorType) bool {
	switch errorType {
	case ErrorTypeNetworkTimeout, ErrorTypeRateLimit:
		return true
	case ErrorTypeFileRead, ErrorTypeMetadata, ErrorTypeValidation:
		return false
	case ErrorTypeEmbedding, ErrorTypeS3Upload:
		return true // These might be temporary network issues
	default:
		return false
	}
}

// WrapError wraps an existing error with additional context
func WrapError(err error, errorType ErrorType, filePath string) *ProcessingError {
	if err == nil {
		return nil
	}

	return &ProcessingError{
		Type:       errorType,
		Message:    err.Error(),
		FilePath:   filePath,
		Timestamp:  time.Now(),
		Retryable:  isRetryableError(errorType),
		RetryCount: 0,
	}
}

// IsNetworkError checks if the error is network-related
func IsNetworkError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())
	networkKeywords := []string{
		"timeout", "connection", "network", "refused", "unreachable",
		"dns", "no route", "broken pipe", "reset by peer",
	}

	for _, keyword := range networkKeywords {
		if strings.Contains(message, keyword) {
			return true
		}
	}

	return false
}

// IsRateLimitError checks if the error is rate limit related
func IsRateLimitError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())
	rateLimitKeywords := []string{
		"rate limit", "too many requests", "429", "quota exceeded",
		"throttled", "rate exceeded",
	}

	for _, keyword := range rateLimitKeywords {
		if strings.Contains(message, keyword) {
			return true
		}
	}

	return false
}

// ClassifyError determines the error type based on the error message
func ClassifyError(err error, context string) ErrorType {
	if err == nil {
		return ErrorTypeUnknown
	}

	if IsNetworkError(err) {
		return ErrorTypeNetworkTimeout
	}

	if IsRateLimitError(err) {
		return ErrorTypeRateLimit
	}

	// Classify based on context
	switch context {
	case "file_read":
		return ErrorTypeFileRead
	case "metadata":
		return ErrorTypeMetadata
	case "embedding":
		return ErrorTypeEmbedding
	case "s3_upload":
		return ErrorTypeS3Upload
	case "validation":
		return ErrorTypeValidation
	default:
		return ErrorTypeUnknown
	}
}

// FormatErrorSummary creates a formatted summary of processing errors
func FormatErrorSummary(errors []ProcessingError) string {
	if len(errors) == 0 {
		return "No errors occurred during processing."
	}

	errorCounts := make(map[ErrorType]int)
	for _, err := range errors {
		errorCounts[err.Type]++
	}

	var summary strings.Builder
	summary.WriteString(fmt.Sprintf("Processing completed with %d error(s):\n", len(errors)))

	for errorType, count := range errorCounts {
		summary.WriteString(fmt.Sprintf("  - %s: %d error(s)\n", errorType, count))
	}

	// Show first few specific errors for debugging
	summary.WriteString("\nFirst few errors:\n")
	maxShow := 5
	if len(errors) < maxShow {
		maxShow = len(errors)
	}

	for i := 0; i < maxShow; i++ {
		err := errors[i]
		summary.WriteString(fmt.Sprintf("  %d. [%s] %s\n", i+1, err.Type, err.Error()))
	}

	if len(errors) > maxShow {
		summary.WriteString(fmt.Sprintf("  ... and %d more error(s)\n", len(errors)-maxShow))
	}

	return summary.String()
}
