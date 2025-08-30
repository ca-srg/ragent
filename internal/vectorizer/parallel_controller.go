package vectorizer

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// ParallelController manages concurrent processing of files for both S3 Vector and OpenSearch
type ParallelController struct {
	s3Client          S3VectorClient
	opensearchIndexer OpenSearchIndexer
	concurrencyLimit  int

	// Statistics tracking with atomic operations
	stats *ParallelProcessingStats
}

// ParallelProcessingStats tracks statistics for dual backend processing
type ParallelProcessingStats struct {
	// Overall statistics
	FilesProcessed  int64
	FilesSuccessful int64
	FilesFailed     int64
	StartTime       time.Time
	EndTime         time.Time
	TotalDuration   time.Duration

	// S3 Vector specific statistics
	S3ProcessedCount int64
	S3SuccessCount   int64
	S3FailureCount   int64
	S3ProcessingTime time.Duration

	// OpenSearch specific statistics
	OSProcessedCount int64
	OSSuccessCount   int64
	OSFailureCount   int64
	OSProcessingTime time.Duration
	OSIndexedCount   int64
	OSSkippedCount   int64
	OSRetryCount     int64

	// Error tracking
	Errors   []ProcessingError
	errorsMu sync.Mutex

	// Performance metrics
	AverageS3Time    time.Duration
	AverageOSTime    time.Duration
	ThroughputPerSec float64
	ConcurrencyUsed  int
}

// ProcessingDecision represents the outcome of processing attempts
type ProcessingDecision int

const (
	ProcessingSuccess         ProcessingDecision = iota // Both backends succeeded
	ProcessingPartialSuccess                            // One backend succeeded, one failed
	ProcessingCompleteFailure                           // Both backends failed
	ProcessingSkipped                                   // Processing was skipped (e.g., dry run)
)

// FileProcessingResult represents the result of processing a single file
type FileProcessingResult struct {
	FileInfo       *FileInfo
	Decision       ProcessingDecision
	S3Success      bool
	S3Error        error
	S3Duration     time.Duration
	OSSuccess      bool
	OSError        error
	OSDuration     time.Duration
	ProcessingTime time.Duration
}

// NewParallelController creates a new parallel controller
func NewParallelController(s3Client S3VectorClient, opensearchIndexer OpenSearchIndexer, concurrencyLimit int) *ParallelController {
	if concurrencyLimit <= 0 {
		concurrencyLimit = 3 // Default concurrency
	}

	return &ParallelController{
		s3Client:          s3Client,
		opensearchIndexer: opensearchIndexer,
		concurrencyLimit:  concurrencyLimit,
		stats: &ParallelProcessingStats{
			StartTime: time.Now(),
			Errors:    make([]ProcessingError, 0),
		},
	}
}

// ProcessFiles processes multiple files concurrently with dual backend support
func (pc *ParallelController) ProcessFiles(
	ctx context.Context,
	files []*FileInfo,
	indexName string,
	embeddingClient EmbeddingClient,
	metadataExtractor MetadataExtractor,
	dryRun bool,
) (*ProcessingResult, error) {

	if len(files) == 0 {
		return pc.createEmptyResult(), nil
	}

	pc.stats.StartTime = time.Now()
	log.Printf("Starting parallel processing of %d files (concurrency: %d, dry-run: %v)",
		len(files), pc.concurrencyLimit, dryRun)

	// Create semaphore for concurrency control
	semaphore := make(chan struct{}, pc.concurrencyLimit)
	var wg sync.WaitGroup
	resultChan := make(chan *FileProcessingResult, len(files))

	// Progress tracking
	var processedCount int64
	totalFiles := int64(len(files))

	// Process each file
	for i, file := range files {
		wg.Add(1)
		go func(idx int, f *FileInfo) {
			defer wg.Done()

			// Acquire semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			result := pc.processFile(ctx, f, indexName, embeddingClient, metadataExtractor, dryRun)
			resultChan <- result

			// Update progress
			current := atomic.AddInt64(&processedCount, 1)
			pc.updateStatisticsFromResult(result)

			// Progress reporting every 10%
			percent := (current * 100) / totalFiles
			if percent%10 == 0 || current == totalFiles {
				log.Printf("Parallel processing progress: %d%% (%d/%d files)", percent, current, totalFiles)
			}
		}(i, file)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(resultChan)

	// Collect all results
	var results []*FileProcessingResult
	for result := range resultChan {
		results = append(results, result)
	}

	// Finalize statistics and create processing result
	return pc.finalizeResults(results), nil
}

// processFile processes a single file with both S3 Vector and OpenSearch backends
func (pc *ParallelController) processFile(
	ctx context.Context,
	fileInfo *FileInfo,
	indexName string,
	embeddingClient EmbeddingClient,
	metadataExtractor MetadataExtractor,
	dryRun bool,
) *FileProcessingResult {

	startTime := time.Now()
	result := &FileProcessingResult{
		FileInfo: fileInfo,
	}

	if dryRun {
		result.Decision = ProcessingSkipped
		result.ProcessingTime = time.Since(startTime)
		return result
	}

	// Load file content if not already loaded
	if fileInfo.Content == "" {
		// Load the file content directly here
		content, err := os.ReadFile(fileInfo.Path)
		if err != nil {
			log.Printf("Failed to read file %s: %v", fileInfo.Name, err)
			result.Decision = ProcessingCompleteFailure
			result.ProcessingTime = time.Since(startTime)
			return result
		}
		fileInfo.Content = string(content)
	}

	// Extract metadata
	metadata, err := metadataExtractor.ExtractMetadata(fileInfo.Path, fileInfo.Content)
	if err != nil {
		log.Printf("Failed to extract metadata for %s: %v", fileInfo.Name, err)
		result.Decision = ProcessingCompleteFailure
		result.ProcessingTime = time.Since(startTime)
		return result
	}
	fileInfo.Metadata = *metadata

	// Check if document needs splitting
	splitter := NewDocumentSplitter()
	documentID := metadataExtractor.GenerateKey(metadata)

	chunks, err := splitter.SplitDocument(fileInfo.Content, documentID)
	if err != nil {
		log.Printf("Failed to split document %s: %v", fileInfo.Name, err)
		result.Decision = ProcessingCompleteFailure
		result.ProcessingTime = time.Since(startTime)
		return result
	}

	// Process each chunk
	var allS3Success = true
	var allOSSuccess = true
	var anyS3Success = false
	var anyOSSuccess = false

	log.Printf("Processing %s as %d chunk(s)", fileInfo.Name, len(chunks))

	for _, chunk := range chunks {
		// Generate embedding for this chunk
		embedding, err := embeddingClient.GenerateEmbedding(ctx, chunk.Content)
		if err != nil {
			log.Printf("Failed to generate embedding for %s (chunk %d/%d): %v",
				fileInfo.Name, chunk.ChunkIndex+1, chunk.TotalChunks, err)
			allS3Success = false
			allOSSuccess = false
			continue
		}

		// Validate embedding is not empty
		if len(embedding) == 0 {
			log.Printf("ERROR: Generated embedding is empty for %s (chunk %d/%d)",
				fileInfo.Name, chunk.ChunkIndex+1, chunk.TotalChunks)
			allS3Success = false
			allOSSuccess = false
			continue
		}

		// Create metadata for this chunk
		chunkMetadata := *metadata
		// Add chunk-specific metadata
		if chunk.TotalChunks > 1 {
			chunkMetadata.Title = fmt.Sprintf("%s (Part %d/%d)", metadata.Title, chunk.ChunkIndex+1, chunk.TotalChunks)
			// Store chunk info in metadata
			if chunkMetadata.Category == "" {
				chunkMetadata.Category = fmt.Sprintf("chunk_%d_of_%d", chunk.ChunkIndex+1, chunk.TotalChunks)
			} else {
				chunkMetadata.Category = fmt.Sprintf("%s,chunk_%d_of_%d", chunkMetadata.Category, chunk.ChunkIndex+1, chunk.TotalChunks)
			}
		}

		// Generate unique ID for this chunk
		chunkID := splitter.GenerateChunkID(documentID, chunk.ChunkIndex)

		// Create vector data for this chunk
		vectorData := &VectorData{
			ID:        chunkID,
			Embedding: embedding,
			Metadata:  chunkMetadata,
			Content:   chunk.Content,
			CreatedAt: time.Now(),
		}

		// Process with both backends in parallel
		var wg sync.WaitGroup

		// Create deep copies for goroutines to avoid closure issues
		// Copy the VectorData value (not just the pointer)
		localVectorData := *vectorData
		// Deep copy the embedding slice
		localEmbedding := make([]float64, len(vectorData.Embedding))
		copy(localEmbedding, vectorData.Embedding)
		localVectorData.Embedding = localEmbedding

		localChunk := *chunk

		// S3 Vector processing
		wg.Add(1)
		go func(vData *VectorData, chunkInfo ChunkedDocument) {
			defer wg.Done()
			s3Start := time.Now()
			err := pc.s3Client.StoreVector(ctx, vData)
			duration := time.Since(s3Start)
			if err == nil {
				anyS3Success = true
				if chunkInfo.TotalChunks > 1 {
					log.Printf("S3: Stored chunk %d/%d of %s in %v",
						chunkInfo.ChunkIndex+1, chunkInfo.TotalChunks, fileInfo.Name, duration)
				}
			} else {
				allS3Success = false
				log.Printf("S3 Vector storage failed for %s (chunk %d/%d): %v",
					fileInfo.Name, chunkInfo.ChunkIndex+1, chunkInfo.TotalChunks, err)
			}
			result.S3Duration += duration
		}(&localVectorData, localChunk)

		// OpenSearch processing
		wg.Add(1)
		go func(vData *VectorData, chunkInfo ChunkedDocument) {
			defer wg.Done()
			osStart := time.Now()

			// Create OpenSearch document with Japanese processing
			osDoc := NewOpenSearchDocument(vData, "")
			// Add chunk metadata to OpenSearch document
			if chunkInfo.TotalChunks > 1 {
				osDoc.ChunkIndex = &chunkInfo.ChunkIndex
				osDoc.TotalChunks = &chunkInfo.TotalChunks
			}

			err := pc.opensearchIndexer.IndexDocument(ctx, indexName, osDoc)
			duration := time.Since(osStart)

			if err == nil {
				anyOSSuccess = true
				if chunkInfo.TotalChunks > 1 {
					log.Printf("OpenSearch: Indexed chunk %d/%d of %s in %v",
						chunkInfo.ChunkIndex+1, chunkInfo.TotalChunks, fileInfo.Name, duration)
				}
			} else {
				allOSSuccess = false
				log.Printf("OpenSearch indexing failed for %s (chunk %d/%d): %v",
					fileInfo.Name, chunkInfo.ChunkIndex+1, chunkInfo.TotalChunks, err)
			}
			result.OSDuration += duration
		}(&localVectorData, localChunk)

		// Wait for both operations to complete
		wg.Wait()
	}

	// Set final results based on chunk processing
	result.S3Success = anyS3Success
	result.OSSuccess = anyOSSuccess
	result.S3Error = nil
	result.OSError = nil

	if !anyS3Success {
		result.S3Error = fmt.Errorf("all chunks failed for S3 storage")
	}
	if !anyOSSuccess {
		result.OSError = fmt.Errorf("all chunks failed for OpenSearch indexing")
	}

	// Determine overall result
	if allS3Success && allOSSuccess {
		result.Decision = ProcessingSuccess
	} else if anyS3Success || anyOSSuccess {
		result.Decision = ProcessingPartialSuccess
	} else {
		result.Decision = ProcessingCompleteFailure
	}

	result.ProcessingTime = time.Since(startTime)

	if result.Decision == ProcessingSuccess {
		if len(chunks) > 1 {
			log.Printf("Successfully processed %s as %d chunks with both backends (Total S3: %v, Total OS: %v)",
				fileInfo.Name, len(chunks), result.S3Duration, result.OSDuration)
		} else {
			log.Printf("Successfully processed %s with both backends (S3: %v, OS: %v)",
				fileInfo.Name, result.S3Duration, result.OSDuration)
		}
	}

	return result
}

// updateStatisticsFromResult updates processing statistics from a single file result
func (pc *ParallelController) updateStatisticsFromResult(result *FileProcessingResult) {
	atomic.AddInt64(&pc.stats.FilesProcessed, 1)

	switch result.Decision {
	case ProcessingSuccess:
		atomic.AddInt64(&pc.stats.FilesSuccessful, 1)
	case ProcessingPartialSuccess:
		// Count as success but track the failure
		atomic.AddInt64(&pc.stats.FilesSuccessful, 1)
		atomic.AddInt64(&pc.stats.FilesFailed, 1)
	case ProcessingCompleteFailure:
		atomic.AddInt64(&pc.stats.FilesFailed, 1)
	case ProcessingSkipped:
		// Don't count as success or failure
	}

	// Update S3 statistics
	atomic.AddInt64(&pc.stats.S3ProcessedCount, 1)
	if result.S3Success {
		atomic.AddInt64(&pc.stats.S3SuccessCount, 1)
	} else {
		atomic.AddInt64(&pc.stats.S3FailureCount, 1)
		if result.S3Error != nil {
			pc.addError(result.S3Error, result.FileInfo.Path, ErrorTypeS3Upload)
		}
	}

	// Update OpenSearch statistics
	atomic.AddInt64(&pc.stats.OSProcessedCount, 1)
	if result.OSSuccess {
		atomic.AddInt64(&pc.stats.OSSuccessCount, 1)
		atomic.AddInt64(&pc.stats.OSIndexedCount, 1)
	} else {
		atomic.AddInt64(&pc.stats.OSFailureCount, 1)
		if result.OSError != nil {
			pc.addError(result.OSError, result.FileInfo.Path, ErrorTypeOpenSearchIndexing)
		}
	}

	// Update timing statistics (using atomic operations for thread safety)
	pc.addDurationToStats(&pc.stats.S3ProcessingTime, result.S3Duration)
	pc.addDurationToStats(&pc.stats.OSProcessingTime, result.OSDuration)
}

// addError safely adds an error to the statistics
func (pc *ParallelController) addError(err error, filePath string, errorType ErrorType) {
	pc.stats.errorsMu.Lock()
	defer pc.stats.errorsMu.Unlock()

	procErr := ProcessingError{
		Type:      errorType,
		Message:   err.Error(),
		FilePath:  filePath,
		Timestamp: time.Now(),
		Retryable: false, // Determine this based on error type
	}

	pc.stats.Errors = append(pc.stats.Errors, procErr)
}

// addDurationToStats atomically adds duration to cumulative timing stats
func (pc *ParallelController) addDurationToStats(target *time.Duration, duration time.Duration) {
	// Convert to nanoseconds for atomic operations
	for {
		currentNanos := atomic.LoadInt64((*int64)(target))
		newNanos := currentNanos + int64(duration)
		if atomic.CompareAndSwapInt64((*int64)(target), currentNanos, newNanos) {
			break
		}
	}
}

// finalizeResults creates the final processing result from all file results
func (pc *ParallelController) finalizeResults(results []*FileProcessingResult) *ProcessingResult {
	pc.stats.EndTime = time.Now()
	pc.stats.TotalDuration = pc.stats.EndTime.Sub(pc.stats.StartTime)

	// Calculate averages and throughput
	if pc.stats.S3ProcessedCount > 0 {
		pc.stats.AverageS3Time = time.Duration(int64(pc.stats.S3ProcessingTime) / pc.stats.S3ProcessedCount)
	}
	if pc.stats.OSProcessedCount > 0 {
		pc.stats.AverageOSTime = time.Duration(int64(pc.stats.OSProcessingTime) / pc.stats.OSProcessedCount)
	}
	if pc.stats.TotalDuration.Seconds() > 0 {
		pc.stats.ThroughputPerSec = float64(pc.stats.FilesProcessed) / pc.stats.TotalDuration.Seconds()
	}

	// Create the final result
	result := &ProcessingResult{
		ProcessedFiles: int(pc.stats.FilesProcessed),
		SuccessCount:   int(pc.stats.FilesSuccessful),
		FailureCount:   int(pc.stats.FilesFailed),
		Errors:         pc.stats.Errors,
		StartTime:      pc.stats.StartTime,
		EndTime:        pc.stats.EndTime,
		Duration:       pc.stats.TotalDuration,
		// OpenSearch specific statistics
		OpenSearchEnabled:        pc.opensearchIndexer != nil,
		OpenSearchSuccessCount:   int(pc.stats.OSSuccessCount),
		OpenSearchFailureCount:   int(pc.stats.OSFailureCount),
		OpenSearchIndexedCount:   int(pc.stats.OSIndexedCount),
		OpenSearchSkippedCount:   int(pc.stats.OSSkippedCount),
		OpenSearchRetryCount:     int(pc.stats.OSRetryCount),
		OpenSearchProcessingTime: pc.stats.OSProcessingTime,
	}

	log.Printf("Parallel processing completed: %d files processed, %d successful, %d failed",
		result.ProcessedFiles, result.SuccessCount, result.FailureCount)
	log.Printf("S3 Vector: %d success, %d failures (avg: %v)",
		pc.stats.S3SuccessCount, pc.stats.S3FailureCount, pc.stats.AverageS3Time)
	log.Printf("OpenSearch: %d success, %d failures (avg: %v)",
		pc.stats.OSSuccessCount, pc.stats.OSFailureCount, pc.stats.AverageOSTime)
	log.Printf("Throughput: %.2f files/sec, Total time: %v",
		pc.stats.ThroughputPerSec, pc.stats.TotalDuration)

	return result
}

// createEmptyResult creates an empty processing result
func (pc *ParallelController) createEmptyResult() *ProcessingResult {
	return &ProcessingResult{
		ProcessedFiles:           0,
		SuccessCount:             0,
		FailureCount:             0,
		Errors:                   []ProcessingError{},
		StartTime:                pc.stats.StartTime,
		EndTime:                  time.Now(),
		Duration:                 time.Since(pc.stats.StartTime),
		OpenSearchEnabled:        pc.opensearchIndexer != nil,
		OpenSearchSuccessCount:   0,
		OpenSearchFailureCount:   0,
		OpenSearchIndexedCount:   0,
		OpenSearchSkippedCount:   0,
		OpenSearchRetryCount:     0,
		OpenSearchProcessingTime: 0,
	}
}

// GetStatistics returns current processing statistics
func (pc *ParallelController) GetStatistics() *ParallelProcessingStats {
	pc.stats.errorsMu.Lock()
	defer pc.stats.errorsMu.Unlock()

	// Create a copy to avoid race conditions (manually copy fields to avoid mutex copy)
	statsCopy := ParallelProcessingStats{
		FilesProcessed:  pc.stats.FilesProcessed,
		FilesSuccessful: pc.stats.FilesSuccessful,
		FilesFailed:     pc.stats.FilesFailed,
		StartTime:       pc.stats.StartTime,
		EndTime:         pc.stats.EndTime,
		TotalDuration:   pc.stats.TotalDuration,

		S3ProcessedCount: pc.stats.S3ProcessedCount,
		S3SuccessCount:   pc.stats.S3SuccessCount,
		S3FailureCount:   pc.stats.S3FailureCount,
		S3ProcessingTime: pc.stats.S3ProcessingTime,

		OSProcessedCount: pc.stats.OSProcessedCount,
		OSSuccessCount:   pc.stats.OSSuccessCount,
		OSFailureCount:   pc.stats.OSFailureCount,
		OSProcessingTime: pc.stats.OSProcessingTime,
		OSIndexedCount:   pc.stats.OSIndexedCount,
		OSSkippedCount:   pc.stats.OSSkippedCount,
		OSRetryCount:     pc.stats.OSRetryCount,

		AverageS3Time:    pc.stats.AverageS3Time,
		AverageOSTime:    pc.stats.AverageOSTime,
		ThroughputPerSec: pc.stats.ThroughputPerSec,
		ConcurrencyUsed:  pc.stats.ConcurrencyUsed,

		Errors: make([]ProcessingError, len(pc.stats.Errors)),
	}
	copy(statsCopy.Errors, pc.stats.Errors)

	return &statsCopy
}

// SetConcurrency updates the concurrency limit
func (pc *ParallelController) SetConcurrency(limit int) {
	if limit > 0 {
		pc.concurrencyLimit = limit
		log.Printf("Parallel controller concurrency updated to %d", limit)
	}
}

// GetConcurrency returns the current concurrency limit
func (pc *ParallelController) GetConcurrency() int {
	return pc.concurrencyLimit
}

// IsHealthy checks if both backends are healthy and responsive
func (pc *ParallelController) IsHealthy(ctx context.Context) (bool, error) {
	var wg sync.WaitGroup
	var s3Healthy, osHealthy bool
	var s3Err, osErr error

	// Check S3 Vector health
	wg.Add(1)
	go func() {
		defer wg.Done()
		s3Err = pc.s3Client.ValidateAccess(ctx)
		s3Healthy = (s3Err == nil)
	}()

	// Check OpenSearch health
	if pc.opensearchIndexer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			osErr = pc.opensearchIndexer.ValidateConnection(ctx)
			osHealthy = (osErr == nil)
		}()
	} else {
		osHealthy = true // Consider healthy if not configured
	}

	wg.Wait()

	if !s3Healthy && !osHealthy {
		return false, fmt.Errorf("both backends unhealthy - S3: %v, OpenSearch: %v", s3Err, osErr)
	}

	if !s3Healthy {
		return false, fmt.Errorf("S3 Vector backend unhealthy: %v", s3Err)
	}

	if !osHealthy {
		return false, fmt.Errorf("OpenSearch backend unhealthy: %v", osErr)
	}

	return true, nil
}
