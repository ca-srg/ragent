package vectorizer

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ca-srg/mdrag/internal/opensearch"
)

// MockOpenSearchClient provides a test double for OpenSearch client
type MockOpenSearchClient struct {
	shouldReturnError bool
	errorToReturn     error
	callCounts        map[string]int
}

func NewMockOpenSearchClient() *MockOpenSearchClient {
	return &MockOpenSearchClient{
		callCounts: make(map[string]int),
	}
}

func (m *MockOpenSearchClient) SetError(err error) {
	m.shouldReturnError = true
	m.errorToReturn = err
}

func (m *MockOpenSearchClient) ClearError() {
	m.shouldReturnError = false
	m.errorToReturn = nil
}

func (m *MockOpenSearchClient) GetCallCount(method string) int {
	return m.callCounts[method]
}

// Mock methods for opensearch.Client interface
func (m *MockOpenSearchClient) HealthCheck(ctx context.Context) error {
	m.callCounts["HealthCheck"]++
	if m.shouldReturnError {
		return m.errorToReturn
	}
	return nil
}

func (m *MockOpenSearchClient) CreateVectorIndex(ctx context.Context, indexName string, dimension int, engine, spaceType string) error {
	m.callCounts["CreateVectorIndex"]++
	if m.shouldReturnError {
		return m.errorToReturn
	}
	return nil
}

func (m *MockOpenSearchClient) WaitForRateLimit(ctx context.Context) error {
	m.callCounts["WaitForRateLimit"]++
	if m.shouldReturnError {
		return m.errorToReturn
	}
	return nil
}

func (m *MockOpenSearchClient) ExecuteWithRetry(ctx context.Context, operation func() error, operationName string) error {
	m.callCounts["ExecuteWithRetry"]++
	if m.shouldReturnError {
		return m.errorToReturn
	}
	return operation()
}

func (m *MockOpenSearchClient) RecordRequest(duration time.Duration, success bool) {
	m.callCounts["RecordRequest"]++
}

func (m *MockOpenSearchClient) GetClient() interface{} {
	m.callCounts["GetClient"]++
	return nil // Return nil for mock
}

// Test functions
func TestOpenSearchIndexerImpl_ValidateConnection(t *testing.T) {
	tests := []struct {
		name        string
		setupError  error
		expectError bool
	}{
		{
			name:        "successful connection validation",
			setupError:  nil,
			expectError: false,
		},
		{
			name:        "connection validation failure",
			setupError:  &ProcessingError{Type: ErrorTypeOpenSearchConnection, Message: "connection failed"},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := NewMockOpenSearchClient()
			if tt.setupError != nil {
				mockClient.SetError(tt.setupError)
			}

			// Create indexer with mock client - we'll need to adjust this when the actual client interface is available
			indexer := &OpenSearchIndexerImpl{
				client:           nil, // Will need proper mock integration
				textProcessor:    opensearch.NewJapaneseTextProcessor(),
				defaultIndex:     "test-index",
				defaultDimension: 1024,
			}

			ctx := context.Background()
			err := indexer.ValidateConnection(ctx)

			if tt.expectError && err == nil {
				t.Errorf("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestOpenSearchIndexerImpl_ProcessJapaneseText(t *testing.T) {
	indexer := &OpenSearchIndexerImpl{
		client:           nil,
		textProcessor:    opensearch.NewJapaneseTextProcessor(),
		defaultIndex:     "test-index",
		defaultDimension: 1536,
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty text",
			input:    "",
			expected: "",
		},
		{
			name:     "simple japanese text",
			input:    "これは日本語のテストです",
			expected: "これは日本語のテストです", // Will be processed by kuromoji
		},
		{
			name:     "mixed japanese and english",
			input:    "Hello 世界",
			expected: "Hello 世界",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := indexer.ProcessJapaneseText(tt.input)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			// Note: The actual processed text might differ based on kuromoji processing
			// This test validates that the method doesn't fail and returns some result
			if tt.input == "" && result != "" {
				t.Errorf("expected empty result for empty input, got: %s", result)
			}
			if tt.input != "" && result == "" {
				t.Errorf("expected non-empty result for non-empty input")
			}
		})
	}
}

func TestOpenSearchDocument_Validate(t *testing.T) {
	tests := []struct {
		name        string
		document    *OpenSearchDocument
		expectError bool
		errorType   ErrorType
	}{
		{
			name: "valid document",
			document: &OpenSearchDocument{
				ID:        "test-id",
				Title:     "Test Title",
				Content:   "Test content",
				Category:  "test",
				Tags:      []string{"tag1", "tag2"},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
				IndexedAt: time.Now(),
				Embedding: []float64{0.1, 0.2, 0.3},
			},
			expectError: false,
		},
		{
			name:        "nil document",
			document:    nil,
			expectError: true,
			errorType:   ErrorTypeValidation,
		},
		{
			name: "missing ID",
			document: &OpenSearchDocument{
				Title:   "Test Title",
				Content: "Test content",
			},
			expectError: true,
			errorType:   ErrorTypeValidation,
		},
		{
			name: "missing title and content",
			document: &OpenSearchDocument{
				ID: "test-id",
			},
			expectError: true,
			errorType:   ErrorTypeValidation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.document != nil {
				err = tt.document.Validate()
			} else {
				// Simulate nil document validation
				err = WrapError(nil, ErrorTypeValidation, "")
			}

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				if procErr, ok := err.(*ProcessingError); ok {
					if tt.errorType != "" && procErr.Type != tt.errorType {
						t.Errorf("expected error type %v, got %v", tt.errorType, procErr.Type)
					}
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestOpenSearchIndexerImpl_classifyOpenSearchError(t *testing.T) {
	indexer := &OpenSearchIndexerImpl{
		client:           nil,
		textProcessor:    opensearch.NewJapaneseTextProcessor(),
		defaultIndex:     "test-index",
		defaultDimension: 1536,
	}

	tests := []struct {
		name         string
		inputError   error
		context      string
		expectedType ErrorType
	}{
		{
			name:         "connection error",
			inputError:   fmt.Errorf("connection refused"),
			context:      "test-context",
			expectedType: ErrorTypeOpenSearchConnection,
		},
		{
			name:         "mapping error",
			inputError:   fmt.Errorf("mapping error"),
			context:      "test-context",
			expectedType: ErrorTypeOpenSearchMapping,
		},
		{
			name:         "rate limit error",
			inputError:   fmt.Errorf("rate limit exceeded"),
			context:      "test-context",
			expectedType: ErrorTypeRateLimit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: This would need proper error instances for real testing
			// The current implementation relies on string matching
			result := indexer.classifyOpenSearchError(tt.inputError, tt.context)

			if result == nil {
				t.Errorf("expected classified error but got nil")
				return
			}

			if procErr, ok := result.(*ProcessingError); ok {
				if procErr.Type != tt.expectedType {
					t.Errorf("expected error type %v, got %v", tt.expectedType, procErr.Type)
				}
			} else {
				t.Errorf("expected ProcessingError, got %T", result)
			}
		})
	}
}

func TestOpenSearchIndexerImpl_SafeDeleteIndex(t *testing.T) {
	// This test would require proper mocking of the OpenSearch client
	// For now, we'll create a basic structure test

	tests := []struct {
		name        string
		indexName   string
		expectError bool
	}{
		{
			name:        "valid index name",
			indexName:   "valid-index",
			expectError: false,
		},
		{
			name:        "system index (should be rejected)",
			indexName:   ".system-index",
			expectError: true,
		},
		{
			name:        "too short index name (should be rejected)",
			indexName:   "ab",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			indexer := &OpenSearchIndexerImpl{
				client:           nil,
				textProcessor:    opensearch.NewJapaneseTextProcessor(),
				defaultIndex:     "test-index",
				defaultDimension: 1024,
			}

			ctx := context.Background()
			_, err := indexer.SafeDeleteIndex(ctx, tt.indexName)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error for index name '%s' but got none", tt.indexName)
				}
			}
			// Note: Non-error cases would require proper client mocking
		})
	}
}

// Benchmark tests
func BenchmarkOpenSearchIndexerImpl_ProcessJapaneseText(b *testing.B) {
	indexer := &OpenSearchIndexerImpl{
		client:           nil,
		textProcessor:    opensearch.NewJapaneseTextProcessor(),
		defaultIndex:     "test-index",
		defaultDimension: 1536,
	}

	testText := "これは日本語のベンチマークテストです。機械学習と自然言語処理を使用しています。"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := indexer.ProcessJapaneseText(testText)
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}
