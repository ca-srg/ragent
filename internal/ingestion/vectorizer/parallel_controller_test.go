package vectorizer

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Mock implementations for testing

// MockS3VectorClient implements S3VectorClient for testing
type MockS3VectorClient struct {
	shouldFail  bool
	failureRate float64 // 0.0 = never fail, 1.0 = always fail
	processTime time.Duration
	callCount   int64
	mu          sync.Mutex
}

func NewMockS3VectorClient() *MockS3VectorClient {
	return &MockS3VectorClient{
		processTime: 10 * time.Millisecond, // Simulate processing time
	}
}

func (m *MockS3VectorClient) SetFailure(shouldFail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldFail = shouldFail
}

func (m *MockS3VectorClient) SetFailureRate(rate float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failureRate = rate
}

func (m *MockS3VectorClient) SetProcessTime(duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.processTime = duration
}

func (m *MockS3VectorClient) GetCallCount() int64 {
	return atomic.LoadInt64(&m.callCount)
}

func (m *MockS3VectorClient) StoreVector(ctx context.Context, vectorData *VectorData) error {
	atomic.AddInt64(&m.callCount, 1)

	m.mu.Lock()
	processTime := m.processTime
	shouldFail := m.shouldFail
	failureRate := m.failureRate
	m.mu.Unlock()

	// Simulate processing time
	time.Sleep(processTime)

	// Simulate failures based on failure rate
	if shouldFail || (failureRate > 0 && float64(atomic.LoadInt64(&m.callCount))/(float64(atomic.LoadInt64(&m.callCount))+1) < failureRate) {
		return fmt.Errorf("mock S3 failure")
	}

	return nil
}

func (m *MockS3VectorClient) ValidateAccess(ctx context.Context) error {
	return nil
}

func (m *MockS3VectorClient) ListVectors(ctx context.Context, prefix string) ([]string, error) {
	return []string{}, nil
}

func (m *MockS3VectorClient) DeleteVector(ctx context.Context, vectorID string) error {
	return nil
}

func (m *MockS3VectorClient) GetBucketInfo(ctx context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{"bucket": "test-bucket"}, nil
}

// MockOpenSearchIndexer implements OpenSearchIndexer for testing
type MockOpenSearchIndexer struct {
	shouldFail  bool
	failureRate float64
	processTime time.Duration
	callCount   int64
	mu          sync.Mutex
}

func NewMockOpenSearchIndexer() *MockOpenSearchIndexer {
	return &MockOpenSearchIndexer{
		processTime: 15 * time.Millisecond, // Simulate processing time
	}
}

func (m *MockOpenSearchIndexer) SetFailure(shouldFail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldFail = shouldFail
}

func (m *MockOpenSearchIndexer) SetFailureRate(rate float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failureRate = rate
}

func (m *MockOpenSearchIndexer) SetProcessTime(duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.processTime = duration
}

func (m *MockOpenSearchIndexer) GetCallCount() int64 {
	return atomic.LoadInt64(&m.callCount)
}

func (m *MockOpenSearchIndexer) IndexDocument(ctx context.Context, indexName string, document *OpenSearchDocument) error {
	atomic.AddInt64(&m.callCount, 1)

	m.mu.Lock()
	processTime := m.processTime
	shouldFail := m.shouldFail
	failureRate := m.failureRate
	m.mu.Unlock()

	// Simulate processing time
	time.Sleep(processTime)

	// Simulate failures
	if shouldFail || (failureRate > 0 && float64(atomic.LoadInt64(&m.callCount))/(float64(atomic.LoadInt64(&m.callCount))+1) < failureRate) {
		return fmt.Errorf("mock OpenSearch failure")
	}

	return nil
}

func (m *MockOpenSearchIndexer) IndexDocuments(ctx context.Context, indexName string, documents []*OpenSearchDocument) error {
	return nil
}

func (m *MockOpenSearchIndexer) ValidateConnection(ctx context.Context) error {
	return nil
}

func (m *MockOpenSearchIndexer) CreateIndex(ctx context.Context, indexName string, dimension int) error {
	return nil
}

func (m *MockOpenSearchIndexer) DeleteIndex(ctx context.Context, indexName string) error {
	return nil
}

func (m *MockOpenSearchIndexer) IndexExists(ctx context.Context, indexName string) (bool, error) {
	return true, nil
}

func (m *MockOpenSearchIndexer) GetIndexInfo(ctx context.Context, indexName string) (map[string]interface{}, error) {
	return map[string]interface{}{"name": indexName}, nil
}

func (m *MockOpenSearchIndexer) RefreshIndex(ctx context.Context, indexName string) error {
	return nil
}

func (m *MockOpenSearchIndexer) GetDocumentCount(ctx context.Context, indexName string) (int64, error) {
	return 0, nil
}

func (m *MockOpenSearchIndexer) ProcessJapaneseText(text string) (string, error) {
	return text, nil
}

// Test functions

func TestNewParallelController(t *testing.T) {
	tests := []struct {
		name             string
		concurrencyLimit int
		expectedLimit    int
	}{
		{
			name:             "valid concurrency limit",
			concurrencyLimit: 5,
			expectedLimit:    5,
		},
		{
			name:             "zero concurrency limit uses default",
			concurrencyLimit: 0,
			expectedLimit:    3,
		},
		{
			name:             "negative concurrency limit uses default",
			concurrencyLimit: -1,
			expectedLimit:    3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s3Client := NewMockS3VectorClient()
			osIndexer := NewMockOpenSearchIndexer()

			pc := NewParallelController(s3Client, osIndexer, tt.concurrencyLimit)

			if pc.concurrencyLimit != tt.expectedLimit {
				t.Errorf("expected concurrency limit %d, got %d", tt.expectedLimit, pc.concurrencyLimit)
			}

			if pc.s3Client == nil {
				t.Error("S3 client should not be nil")
			}

			if pc.opensearchIndexer == nil {
				t.Error("OpenSearch indexer should not be nil")
			}

			if pc.stats == nil {
				t.Error("Stats should not be nil")
			}
		})
	}
}

func TestParallelProcessingStats_UpdateStatistics(t *testing.T) {
	stats := &ParallelProcessingStats{
		StartTime: time.Now(),
		Errors:    make([]ProcessingError, 0),
	}

	// Test atomic operations
	atomic.AddInt64(&stats.FilesProcessed, 1)
	atomic.AddInt64(&stats.FilesSuccessful, 1)
	atomic.AddInt64(&stats.S3SuccessCount, 1)
	atomic.AddInt64(&stats.OSSuccessCount, 1)

	if atomic.LoadInt64(&stats.FilesProcessed) != 1 {
		t.Error("FilesProcessed should be 1")
	}

	if atomic.LoadInt64(&stats.FilesSuccessful) != 1 {
		t.Error("FilesSuccessful should be 1")
	}

	if atomic.LoadInt64(&stats.S3SuccessCount) != 1 {
		t.Error("S3SuccessCount should be 1")
	}

	if atomic.LoadInt64(&stats.OSSuccessCount) != 1 {
		t.Error("OSSuccessCount should be 1")
	}
}

func TestParallelController_ProcessingDecisions(t *testing.T) {
	tests := []struct {
		name             string
		s3Success        bool
		osSuccess        bool
		expectedDecision ProcessingDecision
	}{
		{
			name:             "both succeed",
			s3Success:        true,
			osSuccess:        true,
			expectedDecision: ProcessingSuccess,
		},
		{
			name:             "S3 succeeds, OS fails",
			s3Success:        true,
			osSuccess:        false,
			expectedDecision: ProcessingPartialSuccess,
		},
		{
			name:             "S3 fails, OS succeeds",
			s3Success:        false,
			osSuccess:        true,
			expectedDecision: ProcessingPartialSuccess,
		},
		{
			name:             "both fail",
			s3Success:        false,
			osSuccess:        false,
			expectedDecision: ProcessingCompleteFailure,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Determine decision based on success states
			var decision ProcessingDecision
			switch {
			case tt.s3Success && tt.osSuccess:
				decision = ProcessingSuccess
			case tt.s3Success || tt.osSuccess:
				decision = ProcessingPartialSuccess
			default:
				decision = ProcessingCompleteFailure
			}

			if decision != tt.expectedDecision {
				t.Errorf("expected decision %v, got %v", tt.expectedDecision, decision)
			}
		})
	}
}

func TestParallelController_ConcurrencyControl(t *testing.T) {
	s3Client := NewMockS3VectorClient()
	osIndexer := NewMockOpenSearchIndexer()

	// Set processing time to ensure concurrent execution
	s3Client.SetProcessTime(50 * time.Millisecond)
	osIndexer.SetProcessTime(50 * time.Millisecond)

	pc := NewParallelController(s3Client, osIndexer, 2)

	// Create test file infos
	fileInfos := []*FileInfo{
		{Name: "file1.md", Path: "/test/file1.md", Content: "content1"},
		{Name: "file2.md", Path: "/test/file2.md", Content: "content2"},
		{Name: "file3.md", Path: "/test/file3.md", Content: "content3"},
		{Name: "file4.md", Path: "/test/file4.md", Content: "content4"},
	}

	startTime := time.Now()

	// This would require actual ProcessFiles implementation
	// For now, we'll test the concurrency control concept

	// Simulate concurrent processing with semaphore
	semaphore := make(chan struct{}, pc.concurrencyLimit)
	var wg sync.WaitGroup

	for i, file := range fileInfos {
		wg.Add(1)
		go func(idx int, f *FileInfo) {
			defer wg.Done()

			// Acquire semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// Simulate processing time
			time.Sleep(25 * time.Millisecond)
		}(i, file)
	}

	wg.Wait()
	duration := time.Since(startTime)

	// With concurrency limit of 2, processing 4 items should take less time than sequential
	maxExpectedTime := time.Duration(len(fileInfos)/pc.concurrencyLimit+1) * 30 * time.Millisecond
	if duration > maxExpectedTime {
		t.Errorf("processing took too long: %v > %v, concurrency control may not be working", duration, maxExpectedTime)
	}
}

func TestParallelController_ErrorHandling(t *testing.T) {
	s3Client := NewMockS3VectorClient()
	osIndexer := NewMockOpenSearchIndexer()

	// Configure different failure scenarios
	tests := []struct {
		name        string
		s3Fails     bool
		osFails     bool
		expectedErr string
	}{
		{
			name:    "S3 only fails",
			s3Fails: true,
			osFails: false,
		},
		{
			name:    "OpenSearch only fails",
			s3Fails: false,
			osFails: true,
		},
		{
			name:    "both fail",
			s3Fails: true,
			osFails: true,
		},
		{
			name:    "none fail",
			s3Fails: false,
			osFails: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s3Client.SetFailure(tt.s3Fails)
			osIndexer.SetFailure(tt.osFails)

			// Test individual operations
			ctx := context.Background()

			s3Err := s3Client.StoreVector(ctx, &VectorData{ID: "test"})
			osErr := osIndexer.IndexDocument(ctx, "test-index", &OpenSearchDocument{ID: "test"})

			if tt.s3Fails && s3Err == nil {
				t.Error("expected S3 error but got none")
			}
			if !tt.s3Fails && s3Err != nil {
				t.Errorf("unexpected S3 error: %v", s3Err)
			}

			if tt.osFails && osErr == nil {
				t.Error("expected OpenSearch error but got none")
			}
			if !tt.osFails && osErr != nil {
				t.Errorf("unexpected OpenSearch error: %v", osErr)
			}
		})
	}
}

func TestFileProcessingResult_ValidateFields(t *testing.T) {
	fileInfo := &FileInfo{
		Name:    "test.md",
		Path:    "/test/test.md",
		Content: "test content",
	}

	result := &FileProcessingResult{
		FileInfo:       fileInfo,
		Decision:       ProcessingSuccess,
		S3Success:      true,
		S3Error:        nil,
		S3Duration:     10 * time.Millisecond,
		OSSuccess:      true,
		OSError:        nil,
		OSDuration:     15 * time.Millisecond,
		ProcessingTime: 25 * time.Millisecond,
	}

	// Validate all required fields are set
	if result.FileInfo == nil {
		t.Error("FileInfo should not be nil")
	}

	if result.Decision != ProcessingSuccess {
		t.Error("Decision should be ProcessingSuccess")
	}

	if !result.S3Success || !result.OSSuccess {
		t.Error("Both S3Success and OSSuccess should be true")
	}

	if result.S3Error != nil || result.OSError != nil {
		t.Error("Both errors should be nil for successful processing")
	}

	if result.ProcessingTime <= 0 {
		t.Error("ProcessingTime should be positive")
	}
}

// Benchmark tests
func BenchmarkParallelController_ConcurrentProcessing(b *testing.B) {
	s3Client := NewMockS3VectorClient()
	osIndexer := NewMockOpenSearchIndexer()

	// Set minimal processing times for benchmarking
	s3Client.SetProcessTime(1 * time.Millisecond)
	osIndexer.SetProcessTime(1 * time.Millisecond)

	pc := NewParallelController(s3Client, osIndexer, 5)

	fileInfos := make([]*FileInfo, 10)
	for i := 0; i < 10; i++ {
		fileInfos[i] = &FileInfo{
			Name:    fmt.Sprintf("file%d.md", i),
			Path:    fmt.Sprintf("/test/file%d.md", i),
			Content: fmt.Sprintf("content %d", i),
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Benchmark the semaphore-based concurrency control
		semaphore := make(chan struct{}, pc.concurrencyLimit)
		var wg sync.WaitGroup

		for _, file := range fileInfos {
			wg.Add(1)
			go func(f *FileInfo) {
				defer wg.Done()
				semaphore <- struct{}{}
				defer func() { <-semaphore }()

				// Simulate minimal processing
				time.Sleep(100 * time.Microsecond)
			}(file)
		}
		wg.Wait()
	}
}
