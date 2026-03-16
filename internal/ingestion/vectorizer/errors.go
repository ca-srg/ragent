package vectorizer

import (
	"fmt"
	"strings"
	"time"

	pkgconfig "github.com/ca-srg/ragent/internal/pkg/config"
	pkgdomain "github.com/ca-srg/ragent/internal/pkg/domain"
)

// NewProcessingError creates a new ProcessingError with the given parameters
func NewProcessingError(errorType pkgconfig.ErrorType, message, filePath string) *pkgdomain.ProcessingError {
	return &pkgdomain.ProcessingError{
		Type:       errorType,
		Message:    message,
		FilePath:   filePath,
		Timestamp:  time.Now(),
		Retryable:  isRetryableError(errorType),
		RetryCount: 0,
	}
}

// isRetryableError determines if an error type is retryable
func isRetryableError(errorType pkgconfig.ErrorType) bool {
	switch errorType {
	case pkgconfig.ErrorTypeNetworkTimeout, pkgconfig.ErrorTypeRateLimit:
		return true
	case pkgconfig.ErrorTypeFileRead, pkgconfig.ErrorTypeMetadata, pkgconfig.ErrorTypeValidation:
		return false
	case pkgconfig.ErrorTypeEmbedding, pkgconfig.ErrorTypeS3Upload:
		return true // These might be temporary network issues
	default:
		return false
	}
}

// WrapError wraps an existing error with additional context
func WrapError(err error, errorType pkgconfig.ErrorType, filePath string) *pkgdomain.ProcessingError {
	if err == nil {
		return nil
	}

	return &pkgdomain.ProcessingError{
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
func ClassifyError(err error, context string) pkgconfig.ErrorType {
	if err == nil {
		return pkgconfig.ErrorTypeUnknown
	}

	if IsNetworkError(err) {
		return pkgconfig.ErrorTypeNetworkTimeout
	}

	if IsRateLimitError(err) {
		return pkgconfig.ErrorTypeRateLimit
	}

	// Classify based on context
	switch context {
	case "file_read":
		return pkgconfig.ErrorTypeFileRead
	case "metadata":
		return pkgconfig.ErrorTypeMetadata
	case "embedding":
		return pkgconfig.ErrorTypeEmbedding
	case "s3_upload":
		return pkgconfig.ErrorTypeS3Upload
	case "validation":
		return pkgconfig.ErrorTypeValidation
	default:
		return pkgconfig.ErrorTypeUnknown
	}
}

// FormatErrorSummary creates a formatted summary of processing errors
func FormatErrorSummary(errors []pkgdomain.ProcessingError) string {
	if len(errors) == 0 {
		return "No errors occurred during processing."
	}

	errorCounts := make(map[pkgconfig.ErrorType]int)
	for _, err := range errors {
		errorCounts[err.Type]++
	}

	var summary strings.Builder
	fmt.Fprintf(&summary, "Processing completed with %d error(s):\n", len(errors))

	for errorType, count := range errorCounts {
		fmt.Fprintf(&summary, "  - %s: %d error(s)\n", errorType, count)
	}

	// Show first few specific errors for debugging
	summary.WriteString("\nFirst few errors:\n")
	maxShow := 5
	if len(errors) < maxShow {
		maxShow = len(errors)
	}

	for i := 0; i < maxShow; i++ {
		err := errors[i]
		fmt.Fprintf(&summary, "  %d. [%s] %s\n", i+1, err.Type, err.Error())
	}

	if len(errors) > maxShow {
		fmt.Fprintf(&summary, "  ... and %d more error(s)\n", len(errors)-maxShow)
	}

	return summary.String()
}
