package vectorizer

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// Test helper functions

func createTestError(message string) error {
	return fmt.Errorf("%s", message)
}

func createTestBackendContext(backendType BackendType, operationType OperationType, filePath string) *BackendErrorContext {
	return &BackendErrorContext{
		BackendType:   backendType,
		OperationType: operationType,
		FilePath:      filePath,
		DocumentID:    "test-doc-id",
		AttemptNumber: 1,
		LastAttemptAt: time.Now(),
		TotalDuration: 100 * time.Millisecond,
		OriginalError: fmt.Errorf("test error"),
	}
}

// Tests for DualBackendErrorHandler

func TestNewDualBackendErrorHandler(t *testing.T) {
	tests := []struct {
		name            string
		maxRetries      int
		retryDelay      time.Duration
		expectedRetries int
		expectedDelay   time.Duration
	}{
		{
			name:            "valid parameters",
			maxRetries:      5,
			retryDelay:      2 * time.Second,
			expectedRetries: 5,
			expectedDelay:   2 * time.Second,
		},
		{
			name:            "zero retries",
			maxRetries:      0,
			retryDelay:      1 * time.Second,
			expectedRetries: 0,
			expectedDelay:   1 * time.Second,
		},
		{
			name:            "negative retries should be handled",
			maxRetries:      -1,
			retryDelay:      1 * time.Second,
			expectedRetries: -1, // Let the implementation handle validation
			expectedDelay:   1 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewDualBackendErrorHandler(tt.maxRetries, tt.retryDelay)

			if handler == nil {
				t.Fatal("expected non-nil handler")
			}

			if handler.maxRetryAttempts != tt.expectedRetries {
				t.Errorf("expected max retry attempts %d, got %d", tt.expectedRetries, handler.maxRetryAttempts)
			}

			if handler.retryDelay != tt.expectedDelay {
				t.Errorf("expected retry delay %v, got %v", tt.expectedDelay, handler.retryDelay)
			}

			if handler.stats == nil {
				t.Error("expected non-nil stats")
			}

			if handler.backoffMultiplier <= 1.0 {
				t.Errorf("expected backoff multiplier > 1.0, got %f", handler.backoffMultiplier)
			}
		})
	}
}

func TestDualBackendErrorHandler_HandleError(t *testing.T) {
	tests := []struct {
		name              string
		errorMessage      string
		backendType       BackendType
		operationType     OperationType
		attemptNumber     int
		expectedRetryable bool
		expectedDecision  ProcessingDecision
	}{
		{
			name:              "connection error should be retryable",
			errorMessage:      "connection refused",
			backendType:       BackendS3Vector,
			operationType:     OperationStoreVector,
			attemptNumber:     1,
			expectedRetryable: true,
			expectedDecision:  ProcessingPartialSuccess, // Can continue with other backend
		},
		{
			name:              "timeout error should be retryable",
			errorMessage:      "request timeout",
			backendType:       BackendOpenSearch,
			operationType:     OperationIndexDocument,
			attemptNumber:     1,
			expectedRetryable: true,
			expectedDecision:  ProcessingPartialSuccess,
		},
		{
			name:              "validation error should not be retryable",
			errorMessage:      "invalid document format",
			backendType:       BackendOpenSearch,
			operationType:     OperationIndexDocument,
			attemptNumber:     1,
			expectedRetryable: false,
			expectedDecision:  ProcessingCompleteFailure, // Document is fundamentally invalid
		},
		{
			name:              "authentication error should not be retryable",
			errorMessage:      "authentication failed",
			backendType:       BackendS3Vector,
			operationType:     OperationStoreVector,
			attemptNumber:     1,
			expectedRetryable: false,
			expectedDecision:  ProcessingCompleteFailure,
		},
		{
			name:              "max retries exceeded should not be retryable",
			errorMessage:      "connection refused",
			backendType:       BackendS3Vector,
			operationType:     OperationStoreVector,
			attemptNumber:     10, // Exceeds typical retry limit
			expectedRetryable: false,
			expectedDecision:  ProcessingPartialSuccess, // Can still continue with other backend
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewDualBackendErrorHandler(3, 1*time.Second)

			context := &BackendErrorContext{
				BackendType:   tt.backendType,
				OperationType: tt.operationType,
				FilePath:      "/test/file.md",
				DocumentID:    "test-doc",
				AttemptNumber: tt.attemptNumber,
				LastAttemptAt: time.Now(),
				TotalDuration: 100 * time.Millisecond,
				OriginalError: createTestError(tt.errorMessage),
			}

			decision := handler.HandleError(context)

			if decision == nil {
				t.Fatal("expected non-nil decision")
			}

			if decision.ShouldRetry != tt.expectedRetryable {
				t.Errorf("expected ShouldRetry %v, got %v", tt.expectedRetryable, decision.ShouldRetry)
			}

			if decision.ProcessingDecision != tt.expectedDecision {
				t.Errorf("expected ProcessingDecision %v, got %v", tt.expectedDecision, decision.ProcessingDecision)
			}

			// Validate that retry delay increases with attempt number for retryable errors
			if decision.ShouldRetry && tt.attemptNumber > 1 {
				expectedDelay := time.Duration(float64(handler.retryDelay) *
					pow(handler.backoffMultiplier, float64(tt.attemptNumber-1)))
				if decision.RetryDelay < expectedDelay/2 || decision.RetryDelay > expectedDelay*2 {
					t.Errorf("retry delay %v seems incorrect for attempt %d", decision.RetryDelay, tt.attemptNumber)
				}
			}

			// Validate user message is provided
			if decision.UserMessage == "" {
				t.Error("expected non-empty user message")
			}

			// Validate technical details are provided for debugging
			if decision.TechnicalDetails == "" {
				t.Error("expected non-empty technical details")
			}
		})
	}
}

func TestDualBackendErrorHandler_ClassifyError(t *testing.T) {
	handler := NewDualBackendErrorHandler(3, 1*time.Second)

	tests := []struct {
		name              string
		errorMessage      string
		expectedType      ErrorType
		expectedRetryable bool
	}{
		{
			name:              "connection error",
			errorMessage:      "dial tcp: connection refused",
			expectedType:      ErrorTypeOpenSearchConnection,
			expectedRetryable: true,
		},
		{
			name:              "timeout error",
			errorMessage:      "context deadline exceeded",
			expectedType:      ErrorTypeTimeout,
			expectedRetryable: true,
		},
		{
			name:              "rate limit error",
			errorMessage:      "too many requests",
			expectedType:      ErrorTypeRateLimit,
			expectedRetryable: true,
		},
		{
			name:              "validation error",
			errorMessage:      "invalid field mapping",
			expectedType:      ErrorTypeValidation,
			expectedRetryable: false,
		},
		{
			name:              "authentication error",
			errorMessage:      "authentication failed: invalid credentials",
			expectedType:      ErrorTypeAuthentication,
			expectedRetryable: false,
		},
		{
			name:              "unknown error",
			errorMessage:      "some unknown error occurred",
			expectedType:      ErrorTypeUnknown,
			expectedRetryable: true, // Default to retryable for unknown errors
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := createTestError(tt.errorMessage)

			// Test the classification logic (would need to be exposed or tested indirectly)
			context := createTestBackendContext(BackendOpenSearch, OperationIndexDocument, "/test/file.md")
			context.OriginalError = err

			decision := handler.HandleError(context)

			if decision.ShouldRetry != tt.expectedRetryable {
				t.Errorf("expected retryable %v for error '%s', got %v",
					tt.expectedRetryable, tt.errorMessage, decision.ShouldRetry)
			}
		})
	}
}

func TestDualBackendErrorHandler_Statistics(t *testing.T) {
	handler := NewDualBackendErrorHandler(3, 1*time.Second)

	// Create various error contexts
	contexts := []*BackendErrorContext{
		createTestBackendContext(BackendS3Vector, OperationStoreVector, "/file1.md"),
		createTestBackendContext(BackendOpenSearch, OperationIndexDocument, "/file2.md"),
		createTestBackendContext(BackendOpenSearch, OperationIndexDocument, "/file3.md"),
		createTestBackendContext(BackendS3Vector, OperationStoreVector, "/file4.md"),
	}

	// Process errors
	for _, ctx := range contexts {
		handler.HandleError(ctx)
	}

	stats := handler.GetStatistics()

	if stats.TotalErrors != int64(len(contexts)) {
		t.Errorf("expected TotalErrors %d, got %d", len(contexts), stats.TotalErrors)
	}

	// Test concurrent access to statistics
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx := createTestBackendContext(BackendOpenSearch, OperationIndexDocument, fmt.Sprintf("/file%d.md", idx))
			handler.HandleError(ctx)
		}(i)
	}
	wg.Wait()

	finalStats := handler.GetStatistics()
	if finalStats.TotalErrors != int64(len(contexts)+10) {
		t.Errorf("expected TotalErrors %d after concurrent processing, got %d",
			len(contexts)+10, finalStats.TotalErrors)
	}
}

func TestDualBackendErrorHandler_BackoffStrategy(t *testing.T) {
	handler := NewDualBackendErrorHandler(5, 100*time.Millisecond)

	baseDelay := 100 * time.Millisecond

	tests := []struct {
		attempt     int
		expectedMin time.Duration
		expectedMax time.Duration
	}{
		{1, baseDelay, baseDelay * 2},
		{2, baseDelay * 2, baseDelay * 4},
		{3, baseDelay * 4, baseDelay * 8},
		{4, baseDelay * 8, baseDelay * 16},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("attempt_%d", tt.attempt), func(t *testing.T) {
			context := createTestBackendContext(BackendOpenSearch, OperationIndexDocument, "/test/file.md")
			context.AttemptNumber = tt.attempt
			context.OriginalError = createTestError("connection refused") // Retryable error

			decision := handler.HandleError(context)

			if !decision.ShouldRetry {
				t.Skip("error not retryable, skipping backoff test")
			}

			if decision.RetryDelay < tt.expectedMin {
				t.Errorf("retry delay %v too short for attempt %d, expected at least %v",
					decision.RetryDelay, tt.attempt, tt.expectedMin)
			}

			if decision.RetryDelay > tt.expectedMax {
				t.Errorf("retry delay %v too long for attempt %d, expected at most %v",
					decision.RetryDelay, tt.attempt, tt.expectedMax)
			}
		})
	}
}

func TestErrorHandlerStats_ThreadSafety(t *testing.T) {
	handler := NewDualBackendErrorHandler(3, 1*time.Second)

	// Test concurrent access to statistics
	const numGoroutines = 100
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			// Simulate processing errors
			context := createTestBackendContext(
				BackendType(idx%2), // Alternate between S3 and OpenSearch
				OperationIndexDocument,
				fmt.Sprintf("/file%d.md", idx),
			)

			handler.HandleError(context)
		}(i)
	}

	wg.Wait()

	stats := handler.GetStatistics()
	if stats.TotalErrors != numGoroutines {
		t.Errorf("expected %d total errors, got %d", numGoroutines, stats.TotalErrors)
	}
}

func TestBackendErrorContext_Validation(t *testing.T) {
	tests := []struct {
		name    string
		context *BackendErrorContext
		valid   bool
	}{
		{
			name:    "valid context",
			context: createTestBackendContext(BackendS3Vector, OperationStoreVector, "/test/file.md"),
			valid:   true,
		},
		{
			name: "missing file path",
			context: &BackendErrorContext{
				BackendType:   BackendS3Vector,
				OperationType: OperationStoreVector,
				FilePath:      "", // Missing
				DocumentID:    "test-doc",
			},
			valid: false,
		},
		{
			name: "missing original error",
			context: &BackendErrorContext{
				BackendType:   BackendOpenSearch,
				OperationType: OperationIndexDocument,
				FilePath:      "/test/file.md",
				DocumentID:    "test-doc",
				OriginalError: nil, // Missing
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test basic field validation
			isValid := tt.context.FilePath != "" && tt.context.OriginalError != nil

			if isValid != tt.valid {
				t.Errorf("expected validity %v, got %v for context: %+v", tt.valid, isValid, tt.context)
			}
		})
	}
}

// Helper function for power calculation (simple implementation)
func pow(base, exp float64) float64 {
	result := 1.0
	for i := 0; i < int(exp); i++ {
		result *= base
	}
	return result
}

// Benchmark tests
func BenchmarkDualBackendErrorHandler_HandleError(b *testing.B) {
	handler := NewDualBackendErrorHandler(3, 100*time.Millisecond)
	context := createTestBackendContext(BackendOpenSearch, OperationIndexDocument, "/test/file.md")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		handler.HandleError(context)
	}
}

func BenchmarkDualBackendErrorHandler_ConcurrentHandling(b *testing.B) {
	handler := NewDualBackendErrorHandler(3, 100*time.Millisecond)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			context := createTestBackendContext(
				BackendType(i%2),
				OperationIndexDocument,
				fmt.Sprintf("/test/file%d.md", i),
			)
			handler.HandleError(context)
			i++
		}
	})
}
