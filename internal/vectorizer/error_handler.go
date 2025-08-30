package vectorizer

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// DualBackendErrorHandler handles errors from both S3 Vector and OpenSearch backends
type DualBackendErrorHandler struct {
	mu sync.RWMutex

	// Error classification settings
	maxRetryAttempts  int
	retryDelay        time.Duration
	backoffMultiplier float64

	// Error statistics
	stats *ErrorHandlerStats
}

// ErrorHandlerStats tracks error handling statistics
type ErrorHandlerStats struct {
	// Overall error statistics
	TotalErrors        int64
	RetryableErrors    int64
	NonRetryableErrors int64
	RetriesAttempted   int64
	RetriesSuccessful  int64
	RetriesFailed      int64

	// S3 Vector specific errors
	S3ConnectionErrors int64
	S3UploadErrors     int64
	S3ValidationErrors int64
	S3RetryableErrors  int64

	// OpenSearch specific errors
	OSConnectionErrors int64
	OSIndexingErrors   int64
	OSMappingErrors    int64
	OSBulkIndexErrors  int64
	OSQueryErrors      int64
	OSRetryableErrors  int64

	// Error classification counts
	NetworkErrors        int64
	TimeoutErrors        int64
	RateLimitErrors      int64
	AuthenticationErrors int64
	ValidationErrors     int64
	UnknownErrors        int64
}

// BackendErrorContext provides context for error handling decisions
type BackendErrorContext struct {
	BackendType   BackendType
	OperationType OperationType
	FilePath      string
	DocumentID    string
	AttemptNumber int
	LastAttemptAt time.Time
	TotalDuration time.Duration
	OriginalError error
}

// BackendType identifies the backend that generated the error
type BackendType int

const (
	BackendS3Vector BackendType = iota
	BackendOpenSearch
	BackendBoth // For operations that affect both backends
)

// OperationType identifies the type of operation that failed
type OperationType int

const (
	OperationIndexDocument OperationType = iota
	OperationIndexDocuments
	OperationValidateConnection
	OperationCreateIndex
	OperationDeleteIndex
	OperationStoreVector
	OperationListVectors
	OperationDeleteVector
)

// ErrorHandlingDecision represents the recommended action for handling an error
type ErrorHandlingDecision struct {
	ShouldRetry        bool
	RetryDelay         time.Duration
	MaxRetries         int
	ProcessingDecision ProcessingDecision
	UserMessage        string
	TechnicalDetails   string
	SuggestedActions   []string
}

// NewDualBackendErrorHandler creates a new error handler
func NewDualBackendErrorHandler(maxRetryAttempts int, retryDelay time.Duration) *DualBackendErrorHandler {
	if maxRetryAttempts <= 0 {
		maxRetryAttempts = 3
	}
	if retryDelay <= 0 {
		retryDelay = 2 * time.Second
	}

	return &DualBackendErrorHandler{
		maxRetryAttempts:  maxRetryAttempts,
		retryDelay:        retryDelay,
		backoffMultiplier: 2.0,
		stats:             &ErrorHandlerStats{},
	}
}

// HandleError analyzes an error and provides a handling decision
func (eh *DualBackendErrorHandler) HandleError(ctx *BackendErrorContext) *ErrorHandlingDecision {
	eh.mu.Lock()
	defer eh.mu.Unlock()

	if ctx == nil || ctx.OriginalError == nil {
		return eh.createDecision(false, 0, ProcessingCompleteFailure, "No error to handle", "", nil)
	}

	// Update statistics
	eh.updateErrorStatistics(ctx)

	// Classify the error
	errorClass := eh.classifyError(ctx.OriginalError, ctx.BackendType)

	// Determine if the error is retryable
	isRetryable := eh.isRetryableError(ctx.OriginalError, ctx.BackendType, errorClass)

	// Calculate retry delay with exponential backoff
	retryDelay := eh.calculateRetryDelay(ctx.AttemptNumber)

	// Determine processing decision
	decision := eh.determineProcessingDecision(ctx, errorClass, isRetryable)

	// Create user-friendly message and suggestions
	userMessage, technicalDetails, suggestions := eh.createErrorMessages(ctx, errorClass)

	// Check if we should retry
	shouldRetry := isRetryable &&
		ctx.AttemptNumber < eh.maxRetryAttempts &&
		decision != ProcessingCompleteFailure

	log.Printf("Error handling decision for %s %s: retry=%v, decision=%v, attempt=%d/%d",
		eh.getBackendName(ctx.BackendType), eh.getOperationName(ctx.OperationType),
		shouldRetry, decision, ctx.AttemptNumber, eh.maxRetryAttempts)

	return eh.createDecision(shouldRetry, retryDelay, decision, userMessage, technicalDetails, suggestions)
}

// HandleDualBackendErrors handles errors from both S3 and OpenSearch operations
func (eh *DualBackendErrorHandler) HandleDualBackendErrors(
	s3Error error,
	s3FilePath string,
	osError error,
	osFilePath string,
	documentID string,
) *ErrorHandlingDecision {

	// If both succeeded, no error handling needed
	if s3Error == nil && osError == nil {
		return eh.createDecision(false, 0, ProcessingSuccess, "Both backends successful", "", nil)
	}

	// Handle individual errors
	var s3Decision, osDecision *ErrorHandlingDecision

	if s3Error != nil {
		s3Ctx := &BackendErrorContext{
			BackendType:   BackendS3Vector,
			OperationType: OperationStoreVector,
			FilePath:      s3FilePath,
			DocumentID:    documentID,
			AttemptNumber: 1,
			LastAttemptAt: time.Now(),
			OriginalError: s3Error,
		}
		s3Decision = eh.HandleError(s3Ctx)
	}

	if osError != nil {
		osCtx := &BackendErrorContext{
			BackendType:   BackendOpenSearch,
			OperationType: OperationIndexDocument,
			FilePath:      osFilePath,
			DocumentID:    documentID,
			AttemptNumber: 1,
			LastAttemptAt: time.Now(),
			OriginalError: osError,
		}
		osDecision = eh.HandleError(osCtx)
	}

	// Combine decisions
	return eh.combineDualBackendDecisions(s3Decision, osDecision, s3Error, osError)
}

// classifyError classifies an error into specific categories
func (eh *DualBackendErrorHandler) classifyError(err error, backendType BackendType) ErrorType {
	if err == nil {
		return ErrorTypeUnknown
	}

	errStr := strings.ToLower(err.Error())

	// Timeout errors - specific classification
	if strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "deadline") ||
		strings.Contains(errStr, "context deadline exceeded") {
		return ErrorTypeTimeout
	}

	// Network and connection errors
	if strings.Contains(errStr, "connection") ||
		strings.Contains(errStr, "dial") ||
		strings.Contains(errStr, "network") ||
		strings.Contains(errStr, "host") {
		if backendType == BackendOpenSearch {
			return ErrorTypeOpenSearchConnection
		}
		return ErrorTypeNetworkTimeout
	}

	// Authentication and authorization errors
	if strings.Contains(errStr, "access denied") ||
		strings.Contains(errStr, "unauthorized") ||
		strings.Contains(errStr, "authentication") ||
		strings.Contains(errStr, "auth") {
		return ErrorTypeAuthentication
	}

	// Rate limiting errors
	if strings.Contains(errStr, "rate limit") ||
		strings.Contains(errStr, "too many requests") ||
		strings.Contains(errStr, "throttle") {
		return ErrorTypeRateLimit
	}

	// Backend specific errors
	switch backendType {
	case BackendS3Vector:
		if strings.Contains(errStr, "bucket") || strings.Contains(errStr, "key") {
			return ErrorTypeS3Upload
		}

	case BackendOpenSearch:
		if strings.Contains(errStr, "mapping") || strings.Contains(errStr, "field") {
			return ErrorTypeOpenSearchMapping
		}
		if strings.Contains(errStr, "index") && strings.Contains(errStr, "not found") {
			return ErrorTypeOpenSearchIndex
		}
		if strings.Contains(errStr, "bulk") || strings.Contains(errStr, "batch") {
			return ErrorTypeOpenSearchBulkIndex
		}
		if strings.Contains(errStr, "query") || strings.Contains(errStr, "search") {
			return ErrorTypeOpenSearchQuery
		}
		return ErrorTypeOpenSearchIndexing
	}

	// Validation errors
	if strings.Contains(errStr, "validation") ||
		strings.Contains(errStr, "invalid") ||
		strings.Contains(errStr, "malformed") {
		return ErrorTypeValidation
	}

	return ErrorTypeUnknown
}

// isRetryableError determines if an error should be retried
func (eh *DualBackendErrorHandler) isRetryableError(err error, backendType BackendType, errorType ErrorType) bool {
	if err == nil {
		return false
	}

	// Always retryable error types
	retryableTypes := map[ErrorType]bool{
		ErrorTypeNetworkTimeout:       true,
		ErrorTypeTimeout:              true, // New: timeout errors are retryable
		ErrorTypeRateLimit:            true,
		ErrorTypeOpenSearchConnection: true,
		ErrorTypeAuthentication:       false, // New: authentication errors are not retryable
		ErrorTypeS3Upload:             false, // Usually not retryable without changes
	}

	if retryable, exists := retryableTypes[errorType]; exists {
		return retryable
	}

	// Check error message for specific retryable conditions
	errStr := strings.ToLower(err.Error())

	// Temporary conditions that are retryable
	retryableConditions := []string{
		"temporary",
		"temporarily",
		"service unavailable",
		"server error",
		"internal error",
		"503",
		"502",
		"504",
	}

	for _, condition := range retryableConditions {
		if strings.Contains(errStr, condition) {
			return true
		}
	}

	// Non-retryable conditions
	nonRetryableConditions := []string{
		"access denied",
		"unauthorized",
		"forbidden",
		"not found",
		"invalid",
		"malformed",
		"bad request",
		"400",
		"401",
		"403",
		"404",
	}

	for _, condition := range nonRetryableConditions {
		if strings.Contains(errStr, condition) {
			return false
		}
	}

	// Default to not retryable for unknown errors
	return false
}

// determineProcessingDecision determines the overall processing decision
func (eh *DualBackendErrorHandler) determineProcessingDecision(
	ctx *BackendErrorContext,
	errorType ErrorType,
	isRetryable bool,
) ProcessingDecision {

	// If this is the final attempt and still failing
	if ctx.AttemptNumber >= eh.maxRetryAttempts {
		// For single backend errors, it's partial success if other backend might work
		if ctx.BackendType != BackendBoth {
			return ProcessingPartialSuccess
		}
		return ProcessingCompleteFailure
	}

	// If retryable, we can potentially succeed
	if isRetryable {
		return ProcessingPartialSuccess
	}

	// For validation errors or other non-retryable errors
	if errorType == ErrorTypeValidation ||
		errorType == ErrorTypeOpenSearchMapping ||
		errorType == ErrorTypeOpenSearchIndex {
		return ProcessingPartialSuccess // Other backend might still work
	}

	return ProcessingCompleteFailure
}

// combineDualBackendDecisions combines decisions from both backends
func (eh *DualBackendErrorHandler) combineDualBackendDecisions(
	s3Decision, osDecision *ErrorHandlingDecision,
	s3Error, osError error,
) *ErrorHandlingDecision {

	// If only one backend failed
	if s3Error == nil && osError != nil {
		// S3 succeeded, OpenSearch failed - partial success
		decision := *osDecision
		decision.ProcessingDecision = ProcessingPartialSuccess
		decision.UserMessage = fmt.Sprintf("S3 Vector succeeded, OpenSearch failed: %s", osDecision.UserMessage)
		return &decision
	}

	if osError == nil && s3Error != nil {
		// OpenSearch succeeded, S3 failed - partial success
		decision := *s3Decision
		decision.ProcessingDecision = ProcessingPartialSuccess
		decision.UserMessage = fmt.Sprintf("OpenSearch succeeded, S3 Vector failed: %s", s3Decision.UserMessage)
		return &decision
	}

	// Both backends failed
	shouldRetry := (s3Decision != nil && s3Decision.ShouldRetry) ||
		(osDecision != nil && osDecision.ShouldRetry)

	var retryDelay time.Duration
	if s3Decision != nil && osDecision != nil {
		// Use the longer retry delay
		if s3Decision.RetryDelay > osDecision.RetryDelay {
			retryDelay = s3Decision.RetryDelay
		} else {
			retryDelay = osDecision.RetryDelay
		}
	} else if s3Decision != nil {
		retryDelay = s3Decision.RetryDelay
	} else if osDecision != nil {
		retryDelay = osDecision.RetryDelay
	}

	// Combine messages
	var userMessage, technicalDetails string
	var suggestions []string

	if s3Decision != nil && osDecision != nil {
		userMessage = fmt.Sprintf("Both backends failed - S3: %s, OpenSearch: %s",
			s3Decision.UserMessage, osDecision.UserMessage)
		technicalDetails = fmt.Sprintf("S3: %s; OpenSearch: %s",
			s3Decision.TechnicalDetails, osDecision.TechnicalDetails)
		suggestions = append(s3Decision.SuggestedActions, osDecision.SuggestedActions...)
	}

	return eh.createDecision(shouldRetry, retryDelay, ProcessingCompleteFailure,
		userMessage, technicalDetails, suggestions)
}

// calculateRetryDelay calculates retry delay with exponential backoff
func (eh *DualBackendErrorHandler) calculateRetryDelay(attemptNumber int) time.Duration {
	if attemptNumber <= 1 {
		return eh.retryDelay
	}

	// Exponential backoff with jitter
	multiplier := float64(attemptNumber - 1)
	backoffDuration := time.Duration(float64(eh.retryDelay) * (eh.backoffMultiplier * multiplier))

	// Cap the maximum delay at 30 seconds
	maxDelay := 30 * time.Second
	if backoffDuration > maxDelay {
		backoffDuration = maxDelay
	}

	return backoffDuration
}

// createErrorMessages creates user-friendly error messages and suggestions
func (eh *DualBackendErrorHandler) createErrorMessages(
	ctx *BackendErrorContext,
	errorType ErrorType,
) (userMessage, technicalDetails string, suggestions []string) {

	backendName := eh.getBackendName(ctx.BackendType)
	operationName := eh.getOperationName(ctx.OperationType)

	switch errorType {
	case ErrorTypeNetworkTimeout:
		userMessage = fmt.Sprintf("%s operation timed out", backendName)
		technicalDetails = fmt.Sprintf("Network timeout during %s %s operation", backendName, operationName)
		suggestions = []string{
			"Check network connectivity",
			"Verify service endpoints are accessible",
			"Consider increasing timeout values",
		}

	case ErrorTypeTimeout:
		userMessage = fmt.Sprintf("%s operation timeout", backendName)
		technicalDetails = fmt.Sprintf("Operation timeout during %s %s operation", backendName, operationName)
		suggestions = []string{
			"Increase request timeout settings",
			"Check system load and performance",
			"Retry with smaller batch sizes",
		}

	case ErrorTypeRateLimit:
		userMessage = fmt.Sprintf("%s rate limit exceeded", backendName)
		technicalDetails = fmt.Sprintf("Rate limiting applied during %s %s operation", backendName, operationName)
		suggestions = []string{
			"Reduce concurrency settings",
			"Implement exponential backoff",
			"Check rate limit quotas",
		}

	case ErrorTypeValidation:
		userMessage = fmt.Sprintf("%s validation error", backendName)
		technicalDetails = fmt.Sprintf("Data validation failed during %s %s operation", backendName, operationName)
		suggestions = []string{
			"Check input data format",
			"Verify required fields are present",
			"Review validation rules",
		}

	case ErrorTypeAuthentication:
		userMessage = fmt.Sprintf("%s authentication failed", backendName)
		technicalDetails = fmt.Sprintf("Authentication error during %s %s operation", backendName, operationName)
		suggestions = []string{
			"Verify authentication credentials",
			"Check API keys and tokens",
			"Ensure proper permissions are configured",
		}

	case ErrorTypeOpenSearchConnection:
		userMessage = "OpenSearch connection failed"
		technicalDetails = fmt.Sprintf("Connection error during OpenSearch %s operation", operationName)
		suggestions = []string{
			"Check OpenSearch endpoint configuration",
			"Verify authentication credentials",
			"Test network connectivity to OpenSearch",
		}

	case ErrorTypeOpenSearchMapping:
		userMessage = "OpenSearch mapping error"
		technicalDetails = fmt.Sprintf("Index mapping issue during OpenSearch %s operation", operationName)
		suggestions = []string{
			"Check index mapping configuration",
			"Verify field types match data",
			"Consider recreating the index",
		}

	case ErrorTypeS3Upload:
		userMessage = "S3 Vector storage failed"
		technicalDetails = fmt.Sprintf("S3 error during %s operation", operationName)
		suggestions = []string{
			"Check S3 bucket permissions",
			"Verify AWS credentials",
			"Check S3 bucket configuration",
		}

	default:
		userMessage = fmt.Sprintf("%s operation failed", backendName)
		technicalDetails = fmt.Sprintf("Unknown error during %s %s operation: %v",
			backendName, operationName, ctx.OriginalError)
		suggestions = []string{
			"Check service logs for details",
			"Retry the operation",
			"Contact support if issue persists",
		}
	}

	return userMessage, technicalDetails, suggestions
}

// updateErrorStatistics updates error handling statistics
func (eh *DualBackendErrorHandler) updateErrorStatistics(ctx *BackendErrorContext) {
	eh.stats.TotalErrors++

	// Update backend-specific statistics
	switch ctx.BackendType {
	case BackendS3Vector:
		switch eh.classifyError(ctx.OriginalError, ctx.BackendType) {
		case ErrorTypeNetworkTimeout:
			eh.stats.S3ConnectionErrors++
		case ErrorTypeTimeout:
			eh.stats.S3ConnectionErrors++
		case ErrorTypeS3Upload:
			eh.stats.S3UploadErrors++
		case ErrorTypeValidation:
			eh.stats.S3ValidationErrors++
		case ErrorTypeAuthentication:
			eh.stats.S3ValidationErrors++
		}
		if eh.isRetryableError(ctx.OriginalError, ctx.BackendType, eh.classifyError(ctx.OriginalError, ctx.BackendType)) {
			eh.stats.S3RetryableErrors++
		}

	case BackendOpenSearch:
		switch eh.classifyError(ctx.OriginalError, ctx.BackendType) {
		case ErrorTypeOpenSearchConnection:
			eh.stats.OSConnectionErrors++
		case ErrorTypeOpenSearchIndexing:
			eh.stats.OSIndexingErrors++
		case ErrorTypeOpenSearchMapping:
			eh.stats.OSMappingErrors++
		case ErrorTypeOpenSearchBulkIndex:
			eh.stats.OSBulkIndexErrors++
		case ErrorTypeOpenSearchQuery:
			eh.stats.OSQueryErrors++
		}
		if eh.isRetryableError(ctx.OriginalError, ctx.BackendType, eh.classifyError(ctx.OriginalError, ctx.BackendType)) {
			eh.stats.OSRetryableErrors++
		}
	}

	// Update general error type statistics
	errorType := eh.classifyError(ctx.OriginalError, ctx.BackendType)
	if eh.isRetryableError(ctx.OriginalError, ctx.BackendType, errorType) {
		eh.stats.RetryableErrors++
	} else {
		eh.stats.NonRetryableErrors++
	}

	// Update error classification counts
	switch errorType {
	case ErrorTypeNetworkTimeout:
		eh.stats.TimeoutErrors++
	case ErrorTypeTimeout:
		eh.stats.TimeoutErrors++
	case ErrorTypeRateLimit:
		eh.stats.RateLimitErrors++
	case ErrorTypeValidation:
		eh.stats.ValidationErrors++
	case ErrorTypeAuthentication:
		eh.stats.ValidationErrors++
	default:
		eh.stats.UnknownErrors++
	}
}

// Helper methods

func (eh *DualBackendErrorHandler) createDecision(
	shouldRetry bool,
	retryDelay time.Duration,
	decision ProcessingDecision,
	userMessage, technicalDetails string,
	suggestions []string,
) *ErrorHandlingDecision {
	return &ErrorHandlingDecision{
		ShouldRetry:        shouldRetry,
		RetryDelay:         retryDelay,
		MaxRetries:         eh.maxRetryAttempts,
		ProcessingDecision: decision,
		UserMessage:        userMessage,
		TechnicalDetails:   technicalDetails,
		SuggestedActions:   suggestions,
	}
}

func (eh *DualBackendErrorHandler) getBackendName(backendType BackendType) string {
	switch backendType {
	case BackendS3Vector:
		return "S3 Vector"
	case BackendOpenSearch:
		return "OpenSearch"
	case BackendBoth:
		return "Both Backends"
	default:
		return "Unknown Backend"
	}
}

func (eh *DualBackendErrorHandler) getOperationName(operationType OperationType) string {
	switch operationType {
	case OperationIndexDocument:
		return "IndexDocument"
	case OperationIndexDocuments:
		return "IndexDocuments"
	case OperationValidateConnection:
		return "ValidateConnection"
	case OperationCreateIndex:
		return "CreateIndex"
	case OperationDeleteIndex:
		return "DeleteIndex"
	case OperationStoreVector:
		return "StoreVector"
	case OperationListVectors:
		return "ListVectors"
	case OperationDeleteVector:
		return "DeleteVector"
	default:
		return "Unknown Operation"
	}
}

// GetStatistics returns current error handling statistics
func (eh *DualBackendErrorHandler) GetStatistics() *ErrorHandlerStats {
	eh.mu.RLock()
	defer eh.mu.RUnlock()

	// Return a copy to avoid race conditions
	statsCopy := *eh.stats
	return &statsCopy
}

// ResetStatistics resets all error handling statistics
func (eh *DualBackendErrorHandler) ResetStatistics() {
	eh.mu.Lock()
	defer eh.mu.Unlock()

	eh.stats = &ErrorHandlerStats{}
}
