package mocks

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ca-srg/ragent/internal/vectorizer"
)

// OpenSearchIndexerMock is a mock implementation of the OpenSearchIndexer interface
type OpenSearchIndexerMock struct {
	mu sync.RWMutex

	// Mock behavior settings
	ShouldFailConnection    bool
	ShouldFailIndexing      bool
	ShouldFailBulkIndexing  bool
	ShouldFailIndexCreation bool
	ShouldFailIndexDeletion bool
	ConnectionLatency       time.Duration
	IndexingLatency         time.Duration

	// Mock state tracking
	IndexedDocuments      []*vectorizer.OpenSearchDocument
	CreatedIndices        []string
	DeletedIndices        []string
	ConnectionCallCount   int
	IndexingCallCount     int
	BulkIndexingCallCount int

	// Mock data storage
	Indices   map[string]IndexInfo
	Documents map[string]map[string]*vectorizer.OpenSearchDocument
}

// IndexInfo represents information about a mock index
type IndexInfo struct {
	Name          string
	Dimension     int
	CreatedAt     time.Time
	DocumentCount int64
	Mappings      map[string]interface{}
	Settings      map[string]interface{}
}

// NewOpenSearchIndexerMock creates a new mock instance
func NewOpenSearchIndexerMock() *OpenSearchIndexerMock {
	return &OpenSearchIndexerMock{
		Indices:   make(map[string]IndexInfo),
		Documents: make(map[string]map[string]*vectorizer.OpenSearchDocument),
	}
}

// IndexDocument mocks indexing a single document
func (m *OpenSearchIndexerMock) IndexDocument(ctx context.Context, indexName string, document *vectorizer.OpenSearchDocument) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.IndexingCallCount++

	if m.ShouldFailIndexing {
		return fmt.Errorf("mock indexing failure for document %s", document.ID)
	}

	if m.IndexingLatency > 0 {
		time.Sleep(m.IndexingLatency)
	}

	// Check if index exists
	if _, exists := m.Indices[indexName]; !exists {
		return fmt.Errorf("index %s does not exist", indexName)
	}

	// Store the document
	if m.Documents[indexName] == nil {
		m.Documents[indexName] = make(map[string]*vectorizer.OpenSearchDocument)
	}

	m.Documents[indexName][document.ID] = document.Clone()
	m.IndexedDocuments = append(m.IndexedDocuments, document.Clone())

	// Update document count
	indexInfo := m.Indices[indexName]
	indexInfo.DocumentCount = int64(len(m.Documents[indexName]))
	m.Indices[indexName] = indexInfo

	return nil
}

// IndexDocuments mocks bulk indexing of documents
func (m *OpenSearchIndexerMock) IndexDocuments(ctx context.Context, indexName string, documents []*vectorizer.OpenSearchDocument) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.BulkIndexingCallCount++

	if m.ShouldFailBulkIndexing {
		return fmt.Errorf("mock bulk indexing failure for %d documents", len(documents))
	}

	if m.IndexingLatency > 0 {
		time.Sleep(m.IndexingLatency * time.Duration(len(documents)/10+1))
	}

	// Check if index exists
	if _, exists := m.Indices[indexName]; !exists {
		return fmt.Errorf("index %s does not exist", indexName)
	}

	// Store all documents
	if m.Documents[indexName] == nil {
		m.Documents[indexName] = make(map[string]*vectorizer.OpenSearchDocument)
	}

	for _, doc := range documents {
		m.Documents[indexName][doc.ID] = doc.Clone()
		m.IndexedDocuments = append(m.IndexedDocuments, doc.Clone())
	}

	// Update document count
	indexInfo := m.Indices[indexName]
	indexInfo.DocumentCount = int64(len(m.Documents[indexName]))
	m.Indices[indexName] = indexInfo

	return nil
}

// ValidateConnection mocks connection validation
func (m *OpenSearchIndexerMock) ValidateConnection(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ConnectionCallCount++

	if m.ShouldFailConnection {
		return fmt.Errorf("mock connection validation failure")
	}

	if m.ConnectionLatency > 0 {
		time.Sleep(m.ConnectionLatency)
	}

	return nil
}

// CreateIndex mocks index creation
func (m *OpenSearchIndexerMock) CreateIndex(ctx context.Context, indexName string, dimension int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ShouldFailIndexCreation {
		return fmt.Errorf("mock index creation failure for %s", indexName)
	}

	if _, exists := m.Indices[indexName]; exists {
		return fmt.Errorf("index %s already exists", indexName)
	}

	indexInfo := IndexInfo{
		Name:          indexName,
		Dimension:     dimension,
		CreatedAt:     time.Now(),
		DocumentCount: 0,
		Mappings: map[string]interface{}{
			"properties": map[string]interface{}{
				"title": map[string]interface{}{
					"type":     "text",
					"analyzer": "kuromoji",
				},
				"content": map[string]interface{}{
					"type":     "text",
					"analyzer": "kuromoji",
				},
				"embedding": map[string]interface{}{
					"type":      "knn_vector",
					"dimension": dimension,
				},
			},
		},
		Settings: map[string]interface{}{
			"index": map[string]interface{}{
				"knn": true,
			},
		},
	}

	m.Indices[indexName] = indexInfo
	m.CreatedIndices = append(m.CreatedIndices, indexName)

	return nil
}

// DeleteIndex mocks index deletion
func (m *OpenSearchIndexerMock) DeleteIndex(ctx context.Context, indexName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ShouldFailIndexDeletion {
		return fmt.Errorf("mock index deletion failure for %s", indexName)
	}

	if _, exists := m.Indices[indexName]; !exists {
		return fmt.Errorf("index %s does not exist", indexName)
	}

	delete(m.Indices, indexName)
	delete(m.Documents, indexName)
	m.DeletedIndices = append(m.DeletedIndices, indexName)

	return nil
}

// IndexExists mocks checking if an index exists
func (m *OpenSearchIndexerMock) IndexExists(ctx context.Context, indexName string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.Indices[indexName]
	return exists, nil
}

// GetIndexInfo mocks retrieving index information
func (m *OpenSearchIndexerMock) GetIndexInfo(ctx context.Context, indexName string) (map[string]interface{}, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	indexInfo, exists := m.Indices[indexName]
	if !exists {
		return nil, fmt.Errorf("index %s does not exist", indexName)
	}

	return map[string]interface{}{
		"name":           indexInfo.Name,
		"dimension":      indexInfo.Dimension,
		"created_at":     indexInfo.CreatedAt,
		"document_count": indexInfo.DocumentCount,
		"mappings":       indexInfo.Mappings,
		"settings":       indexInfo.Settings,
	}, nil
}

// RefreshIndex mocks index refresh operation
func (m *OpenSearchIndexerMock) RefreshIndex(ctx context.Context, indexName string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, exists := m.Indices[indexName]; !exists {
		return fmt.Errorf("index %s does not exist", indexName)
	}

	// Mock refresh - no actual operation needed
	return nil
}

// GetDocumentCount mocks getting document count
func (m *OpenSearchIndexerMock) GetDocumentCount(ctx context.Context, indexName string) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	indexInfo, exists := m.Indices[indexName]
	if !exists {
		return 0, fmt.Errorf("index %s does not exist", indexName)
	}

	return indexInfo.DocumentCount, nil
}

// ProcessJapaneseText mocks Japanese text processing
func (m *OpenSearchIndexerMock) ProcessJapaneseText(text string) (string, error) {
	// Simple mock - just return the original text with a marker
	return fmt.Sprintf("processed_%s", text), nil
}

// Helper methods for testing

// Reset clears all mock state
func (m *OpenSearchIndexerMock) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ShouldFailConnection = false
	m.ShouldFailIndexing = false
	m.ShouldFailBulkIndexing = false
	m.ShouldFailIndexCreation = false
	m.ShouldFailIndexDeletion = false
	m.ConnectionLatency = 0
	m.IndexingLatency = 0

	m.IndexedDocuments = nil
	m.CreatedIndices = nil
	m.DeletedIndices = nil
	m.ConnectionCallCount = 0
	m.IndexingCallCount = 0
	m.BulkIndexingCallCount = 0

	m.Indices = make(map[string]IndexInfo)
	m.Documents = make(map[string]map[string]*vectorizer.OpenSearchDocument)
}

// GetIndexedDocumentCount returns the number of documents indexed
func (m *OpenSearchIndexerMock) GetIndexedDocumentCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.IndexedDocuments)
}

// GetIndexedDocumentsByIndex returns documents indexed for a specific index
func (m *OpenSearchIndexerMock) GetIndexedDocumentsByIndex(indexName string) []*vectorizer.OpenSearchDocument {
	m.mu.RLock()
	defer m.mu.RUnlock()

	documents, exists := m.Documents[indexName]
	if !exists {
		return nil
	}

	var result []*vectorizer.OpenSearchDocument
	for _, doc := range documents {
		result = append(result, doc.Clone())
	}

	return result
}

// GetAllIndices returns all created indices
func (m *OpenSearchIndexerMock) GetAllIndices() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var indices []string
	for indexName := range m.Indices {
		indices = append(indices, indexName)
	}

	return indices
}

// SetLatencies sets the mock latencies for operations
func (m *OpenSearchIndexerMock) SetLatencies(connection, indexing time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ConnectionLatency = connection
	m.IndexingLatency = indexing
}

// SetFailureModes configures which operations should fail
func (m *OpenSearchIndexerMock) SetFailureModes(connection, indexing, bulkIndexing, indexCreation, indexDeletion bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ShouldFailConnection = connection
	m.ShouldFailIndexing = indexing
	m.ShouldFailBulkIndexing = bulkIndexing
	m.ShouldFailIndexCreation = indexCreation
	m.ShouldFailIndexDeletion = indexDeletion
}

// GetCallCounts returns the number of times each operation was called
func (m *OpenSearchIndexerMock) GetCallCounts() (connection, indexing, bulkIndexing int) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.ConnectionCallCount, m.IndexingCallCount, m.BulkIndexingCallCount
}
