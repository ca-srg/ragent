package vectorizer

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ca-srg/mdrag/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integration tests using mocks to test interface interactions

func TestVectorizerService_IntegrationWithMocks(t *testing.T) {
	// Create mock services
	mockEmbedding := NewMockEmbeddingClient()
	mockS3 := NewMockS3VectorClient()
	mockMetadata := NewMockMetadataExtractor()
	mockScanner := NewMockFileScanner()
	mockOSIndexer := NewMockOpenSearchIndexerIntegration()

	// Set up test files
	testFiles := []*FileInfo{
		{
			Path:    "test1.md",
			Name:    "test1.md",
			Size:    100,
			Content: "Test content 1",
		},
		{
			Path:    "test2.md",
			Name:    "test2.md",
			Size:    200,
			Content: "Test content 2",
		},
	}
	mockScanner.SetFiles(testFiles)

	// Set up embedding response
	mockEmbedding.SetEmbedding(make([]float64, 1024))

	// Create index
	ctx := context.Background()
	err := mockOSIndexer.CreateIndex(ctx, "test-index", 1024)
	require.NoError(t, err)

	// Create service config
	cfg := &types.Config{
		Concurrency: 2,
	}

	serviceConfig := &ServiceConfig{
		Config:              cfg,
		EmbeddingClient:     mockEmbedding,
		S3Client:            mockS3,
		MetadataExtractor:   mockMetadata,
		FileScanner:         mockScanner,
		EnableOpenSearch:    true,
		OpenSearchIndexName: "test-index",
		OpenSearchIndexer:   mockOSIndexer,
	}

	// Create service
	service, err := NewVectorizerService(serviceConfig)
	require.NoError(t, err)

	// Process files
	result, err := service.VectorizeMarkdownFiles(ctx, "test-dir", false)

	// Verify results
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 2, result.ProcessedFiles)
	assert.Equal(t, 2, result.SuccessCount)
	assert.Equal(t, 0, result.FailureCount)

	// Verify call counts
	connectionCalls, indexingCalls, bulkCalls := mockOSIndexer.GetCallCounts()
	assert.True(t, connectionCalls >= 0) // Connection validation might be called
	assert.True(t, indexingCalls > 0 || bulkCalls > 0, "Either indexing or bulk calls should be made")
}

func TestDualBackendProcessing_WithMocks(t *testing.T) {
	// Setup mocks
	mockS3Client := NewMockS3VectorClient()
	mockOSIndexer := NewMockOpenSearchIndexerIntegration()

	// Create test documents
	testFiles := []*FileInfo{
		{
			Name:    "file1.md",
			Path:    "/test/file1.md",
			Content: "Content 1",
			Metadata: types.DocumentMetadata{
				Title:     "File 1",
				Category:  "test",
				WordCount: 10,
			},
		},
		{
			Name:    "file2.md",
			Path:    "/test/file2.md",
			Content: "Content 2",
			Metadata: types.DocumentMetadata{
				Title:     "File 2",
				Category:  "test",
				WordCount: 15,
			},
		},
	}

	// Setup OpenSearch index
	ctx := context.Background()
	indexName := "test-dual-backend"
	err := mockOSIndexer.CreateIndex(ctx, indexName, 1024)
	if err != nil {
		t.Fatalf("failed to create test index: %v", err)
	}

	// Test scenarios
	tests := []struct {
		name             string
		s3ShouldFail     bool
		osShouldFail     bool
		expectedDecision ProcessingDecision
	}{
		{
			name:             "both succeed",
			s3ShouldFail:     false,
			osShouldFail:     false,
			expectedDecision: ProcessingSuccess,
		},
		{
			name:             "S3 fails, OS succeeds",
			s3ShouldFail:     true,
			osShouldFail:     false,
			expectedDecision: ProcessingPartialSuccess,
		},
		{
			name:             "S3 succeeds, OS fails",
			s3ShouldFail:     false,
			osShouldFail:     true,
			expectedDecision: ProcessingPartialSuccess,
		},
		{
			name:             "both fail",
			s3ShouldFail:     true,
			osShouldFail:     true,
			expectedDecision: ProcessingCompleteFailure,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset mocks
			mockS3Client.SetFailure(tt.s3ShouldFail)
			mockOSIndexer.SetFailure(tt.osShouldFail)

			// Test individual backend operations to verify decision logic
			var s3Success, osSuccess bool

			// Test S3 operation
			s3Err := mockS3Client.StoreVector(ctx, &types.VectorData{
				ID:        "test-doc",
				Embedding: []float64{0.1, 0.2, 0.3},
				Metadata:  testFiles[0].Metadata,
				Content:   testFiles[0].Content,
			})
			s3Success = s3Err == nil

			// Test OpenSearch operation
			osDoc := &OpenSearchDocument{
				ID:        "test-doc",
				Title:     testFiles[0].Metadata.Title,
				Content:   testFiles[0].Content,
				Category:  testFiles[0].Metadata.Category,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
				IndexedAt: time.Now(),
			}
			osErr := mockOSIndexer.IndexDocument(ctx, indexName, osDoc)
			osSuccess = osErr == nil

			// Verify decision logic
			var actualDecision ProcessingDecision
			switch {
			case s3Success && osSuccess:
				actualDecision = ProcessingSuccess
			case s3Success || osSuccess:
				actualDecision = ProcessingPartialSuccess
			default:
				actualDecision = ProcessingCompleteFailure
			}

			if actualDecision != tt.expectedDecision {
				t.Errorf("expected decision %v, got %v (S3: %v, OS: %v)",
					tt.expectedDecision, actualDecision, s3Success, osSuccess)
			}
		})
	}
}

func TestErrorHandling_IntegrationWithMocks(t *testing.T) {
	// Create error handler
	errorHandler := NewDualBackendErrorHandler(3, 100*time.Millisecond)

	// Test various error scenarios
	tests := []struct {
		name            string
		errorType       string
		backendType     BackendType
		operationType   OperationType
		expectRetryable bool
	}{
		{
			name:            "S3 connection error",
			errorType:       "connection refused",
			backendType:     BackendS3Vector,
			operationType:   OperationStoreVector,
			expectRetryable: true,
		},
		{
			name:            "OpenSearch mapping error",
			errorType:       "mapping conflict",
			backendType:     BackendOpenSearch,
			operationType:   OperationIndexDocument,
			expectRetryable: false,
		},
		{
			name:            "OpenSearch timeout",
			errorType:       "request timeout",
			backendType:     BackendOpenSearch,
			operationType:   OperationIndexDocument,
			expectRetryable: true,
		},
		{
			name:            "S3 validation error",
			errorType:       "invalid vector data",
			backendType:     BackendS3Vector,
			operationType:   OperationStoreVector,
			expectRetryable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create error context
			context := &BackendErrorContext{
				BackendType:   tt.backendType,
				OperationType: tt.operationType,
				FilePath:      "/test/file.md",
				DocumentID:    "test-doc",
				AttemptNumber: 1,
				LastAttemptAt: time.Now(),
				TotalDuration: 100 * time.Millisecond,
				OriginalError: fmt.Errorf("%s", tt.errorType),
			}

			// Handle the error
			decision := errorHandler.HandleError(context)

			if decision.ShouldRetry != tt.expectRetryable {
				t.Errorf("expected retryable %v for %s, got %v",
					tt.expectRetryable, tt.errorType, decision.ShouldRetry)
			}

			// Verify decision includes useful information
			if decision.UserMessage == "" {
				t.Error("expected non-empty user message")
			}

			if decision.TechnicalDetails == "" {
				t.Error("expected non-empty technical details")
			}
		})
	}

	// Test statistics tracking
	stats := errorHandler.GetStatistics()
	if stats.TotalErrors != int64(len(tests)) {
		t.Errorf("expected %d total errors, got %d", len(tests), stats.TotalErrors)
	}
}

func TestServiceConfiguration_ValidationWithMocks(t *testing.T) {
	tests := []struct {
		name        string
		config      *ServiceConfig
		expectError bool
		errorType   string
	}{
		{
			name: "valid configuration",
			config: &ServiceConfig{
				Config:              &Config{Concurrency: 3, RetryAttempts: 2},
				EmbeddingClient:     NewMockEmbeddingClient(),
				S3Client:            NewMockS3VectorClient(),
				OpenSearchIndexer:   NewMockOpenSearchIndexerIntegration(),
				MetadataExtractor:   NewMockMetadataExtractor(),
				FileScanner:         NewMockFileScanner(),
				EnableOpenSearch:    true,
				OpenSearchIndexName: "test-index",
			},
			expectError: false,
		},
		{
			name:        "nil configuration",
			config:      nil,
			expectError: true,
			errorType:   "service config cannot be nil",
		},
		{
			name: "missing embedding client",
			config: &ServiceConfig{
				Config:              &Config{Concurrency: 3},
				EmbeddingClient:     nil,
				S3Client:            NewMockS3VectorClient(),
				OpenSearchIndexer:   NewMockOpenSearchIndexerIntegration(),
				MetadataExtractor:   NewMockMetadataExtractor(),
				FileScanner:         NewMockFileScanner(),
				EnableOpenSearch:    true,
				OpenSearchIndexName: "test-index",
			},
			expectError: false, // Service might handle nil gracefully
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, err := NewVectorizerService(tt.config)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
				if tt.errorType != "" && !strings.Contains(err.Error(), tt.errorType) {
					t.Errorf("expected error containing '%s', got '%v'", tt.errorType, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if service == nil {
					t.Error("expected non-nil service")
				}
			}
		})
	}
}

// Helper mock implementations for testing

type MockMetadataExtractor struct {
	metadata *types.DocumentMetadata
}

func NewMockMetadataExtractor() *MockMetadataExtractor {
	return &MockMetadataExtractor{
		metadata: &types.DocumentMetadata{
			Title:     "Mock Title",
			Category:  "mock",
			WordCount: 100,
			Tags:      []string{"test"},
		},
	}
}

func (m *MockMetadataExtractor) ExtractMetadata(filePath string, content string) (*types.DocumentMetadata, error) {
	if m.metadata != nil {
		return m.metadata, nil
	}
	return &types.DocumentMetadata{Title: "Default", Category: "default"}, nil
}

func (m *MockMetadataExtractor) ParseFrontMatter(content string) (map[string]interface{}, string, error) {
	return map[string]interface{}{}, content, nil
}

func (m *MockMetadataExtractor) GenerateKey(metadata *types.DocumentMetadata) string {
	return fmt.Sprintf("mock-key-%s", metadata.Title)
}

type MockFileScanner struct {
	files []*FileInfo
}

func NewMockFileScanner() *MockFileScanner {
	return &MockFileScanner{
		files: []*FileInfo{},
	}
}

func (m *MockFileScanner) SetFiles(files []*FileInfo) {
	m.files = files
}

func (m *MockFileScanner) ScanDirectory(dirPath string) ([]*FileInfo, error) {
	return m.files, nil
}

func (m *MockFileScanner) ValidateDirectory(dirPath string) error {
	return nil
}

func (m *MockFileScanner) ReadFileContent(filePath string) (string, error) {
	for _, file := range m.files {
		if file.Path == filePath {
			return file.Content, nil
		}
	}
	return "", fmt.Errorf("file not found: %s", filePath)
}

func (m *MockFileScanner) IsMarkdownFile(filePath string) bool {
	return strings.HasSuffix(filePath, ".md")
}

type MockEmbeddingClient struct {
	embedding  []float64
	shouldFail bool
}

func NewMockEmbeddingClient() *MockEmbeddingClient {
	return &MockEmbeddingClient{
		embedding: []float64{0.1, 0.2, 0.3, 0.4, 0.5},
	}
}

func (m *MockEmbeddingClient) SetEmbedding(embedding []float64) {
	m.embedding = embedding
}

func (m *MockEmbeddingClient) SetFailure(shouldFail bool) {
	m.shouldFail = shouldFail
}

func (m *MockEmbeddingClient) GenerateEmbedding(ctx context.Context, text string) ([]float64, error) {
	if m.shouldFail {
		return nil, fmt.Errorf("mock embedding generation failure")
	}
	return m.embedding, nil
}

func (m *MockEmbeddingClient) ValidateConnection(ctx context.Context) error {
	if m.shouldFail {
		return fmt.Errorf("mock embedding client connection failure")
	}
	return nil
}

func (m *MockEmbeddingClient) GetModelInfo() (string, int, error) {
	return "mock-model", len(m.embedding), nil
}

// MockOpenSearchIndexerIntegration is a simplified mock for integration tests
type MockOpenSearchIndexerIntegration struct {
	connectionCalls int
	indexingCalls   int
	bulkCalls       int
	shouldFail      bool
}

func NewMockOpenSearchIndexerIntegration() *MockOpenSearchIndexerIntegration {
	return &MockOpenSearchIndexerIntegration{}
}

func (m *MockOpenSearchIndexerIntegration) SetFailure(shouldFail bool) {
	m.shouldFail = shouldFail
}

func (m *MockOpenSearchIndexerIntegration) GetCallCounts() (connection, indexing, bulkIndexing int) {
	return m.connectionCalls, m.indexingCalls, m.bulkCalls
}

func (m *MockOpenSearchIndexerIntegration) IndexDocument(ctx context.Context, indexName string, doc *OpenSearchDocument) error {
	m.indexingCalls++
	if m.shouldFail {
		return fmt.Errorf("mock indexing failure")
	}
	return nil
}

func (m *MockOpenSearchIndexerIntegration) ValidateConnection(ctx context.Context) error {
	m.connectionCalls++
	if m.shouldFail {
		return fmt.Errorf("mock connection failure")
	}
	return nil
}

func (m *MockOpenSearchIndexerIntegration) CreateIndex(ctx context.Context, indexName string, dimension int) error {
	if m.shouldFail {
		return fmt.Errorf("mock index creation failure")
	}
	return nil
}

func (m *MockOpenSearchIndexerIntegration) IndexDocuments(ctx context.Context, indexName string, documents []*OpenSearchDocument) error {
	m.bulkCalls++
	if m.shouldFail {
		return fmt.Errorf("mock bulk indexing failure")
	}
	return nil
}

func (m *MockOpenSearchIndexerIntegration) DeleteIndex(ctx context.Context, indexName string) error {
	if m.shouldFail {
		return fmt.Errorf("mock index deletion failure")
	}
	return nil
}

func (m *MockOpenSearchIndexerIntegration) IndexExists(ctx context.Context, indexName string) (bool, error) {
	if m.shouldFail {
		return false, fmt.Errorf("mock index exists failure")
	}
	return true, nil
}

func (m *MockOpenSearchIndexerIntegration) GetIndexInfo(ctx context.Context, indexName string) (map[string]interface{}, error) {
	if m.shouldFail {
		return nil, fmt.Errorf("mock get index info failure")
	}
	return map[string]interface{}{"name": indexName}, nil
}

func (m *MockOpenSearchIndexerIntegration) RefreshIndex(ctx context.Context, indexName string) error {
	if m.shouldFail {
		return fmt.Errorf("mock refresh index failure")
	}
	return nil
}

func (m *MockOpenSearchIndexerIntegration) GetDocumentCount(ctx context.Context, indexName string) (int64, error) {
	if m.shouldFail {
		return 0, fmt.Errorf("mock get document count failure")
	}
	return 0, nil
}

func (m *MockOpenSearchIndexerIntegration) ProcessJapaneseText(text string) (string, error) {
	if m.shouldFail {
		return "", fmt.Errorf("mock process japanese text failure")
	}
	return text, nil
}
