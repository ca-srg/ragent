package vectorizer

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// VectorizerService orchestrates the vectorization process
type VectorizerService struct {
	embeddingClient   EmbeddingClient
	s3Client          S3VectorClient
	metadataExtractor MetadataExtractor
	fileScanner       FileScanner
	config            *Config
	stats             *ProcessingStats
}

// ProcessingStats tracks processing statistics
type ProcessingStats struct {
	mu                sync.RWMutex
	FilesProcessed    int
	FilesSuccessful   int
	FilesFailed       int
	EmbeddingsCreated int
	TotalTokens       int
	StartTime         time.Time
	Errors            []ProcessingError
}

// ServiceConfig contains the configuration for creating a VectorizerService
type ServiceConfig struct {
	Config            *Config
	EmbeddingClient   EmbeddingClient
	S3Client          S3VectorClient
	MetadataExtractor MetadataExtractor
	FileScanner       FileScanner
}

// NewVectorizerService creates a new vectorizer service with the given configuration
func NewVectorizerService(serviceConfig *ServiceConfig) (*VectorizerService, error) {
	if serviceConfig == nil {
		return nil, fmt.Errorf("service config cannot be nil")
	}

	service := &VectorizerService{
		embeddingClient:   serviceConfig.EmbeddingClient,
		s3Client:          serviceConfig.S3Client,
		metadataExtractor: serviceConfig.MetadataExtractor,
		fileScanner:       serviceConfig.FileScanner,
		config:            serviceConfig.Config,
		stats: &ProcessingStats{
			StartTime: time.Now(),
			Errors:    make([]ProcessingError, 0),
		},
	}

	return service, nil
}

// TODO: NewDefaultVectorizerService - implement in CLI layer to avoid circular imports
// This function will be implemented in the cmd package to avoid circular dependencies

// ValidateConfiguration validates all external service connections
func (vs *VectorizerService) ValidateConfiguration(ctx context.Context) error {
	log.Println("Validating embedding service connection...")
	if err := vs.embeddingClient.ValidateConnection(ctx); err != nil {
		return fmt.Errorf("embedding service validation failed: %w", err)
	}

	log.Println("Validating S3 bucket access...")
	if err := vs.s3Client.ValidateAccess(ctx); err != nil {
		return fmt.Errorf("S3 bucket validation failed: %w", err)
	}

	log.Println("All services validated successfully")
	return nil
}

// VectorizeMarkdownFiles processes all markdown files in a directory
func (vs *VectorizerService) VectorizeMarkdownFiles(ctx context.Context, directory string, dryRun bool) (*ProcessingResult, error) {
	vs.stats.StartTime = time.Now()

	log.Printf("Scanning directory: %s", directory)
	files, err := vs.fileScanner.ScanDirectory(directory)
	if err != nil {
		return nil, fmt.Errorf("failed to scan directory: %w", err)
	}

	if len(files) == 0 {
		log.Println("No markdown files found")
		return &ProcessingResult{
			ProcessedFiles: 0,
			SuccessCount:   0,
			FailureCount:   0,
			StartTime:      vs.stats.StartTime,
			EndTime:        time.Now(),
			Duration:       time.Since(vs.stats.StartTime),
		}, nil
	}

	log.Printf("Found %d markdown files to process", len(files))

	if dryRun {
		log.Println("Dry run mode: will not actually create embeddings or upload to S3")
		return vs.dryRunProcessing(files)
	}

	// Process files with concurrency control
	return vs.processFilesConcurrently(ctx, files)
}

// ProcessSingleFile processes a single markdown file
func (vs *VectorizerService) ProcessSingleFile(ctx context.Context, fileInfo *FileInfo, dryRun bool) error {
	// Load file content if not already loaded
	if fileInfo.Content == "" {
		content, err := vs.fileScanner.ReadFileContent(fileInfo.Path)
		if err != nil {
			return WrapError(err, ErrorTypeFileRead, fileInfo.Path)
		}
		fileInfo.Content = content
	}

	// Extract metadata
	metadata, err := vs.metadataExtractor.ExtractMetadata(fileInfo.Path, fileInfo.Content)
	if err != nil {
		return WrapError(err, ErrorTypeMetadata, fileInfo.Path)
	}

	fileInfo.Metadata = *metadata

	if dryRun {
		log.Printf("DRY RUN: Would process file %s (title: %s, word count: %d)",
			fileInfo.Name, metadata.Title, metadata.WordCount)
		return nil
	}

	// Generate embedding
	embedding, err := vs.embeddingClient.GenerateEmbedding(ctx, fileInfo.Content)
	if err != nil {
		return WrapError(err, ErrorTypeEmbedding, fileInfo.Path)
	}

	// Create vector data
	vectorData := &VectorData{
		ID:        vs.metadataExtractor.GenerateKey(metadata),
		Embedding: embedding,
		Metadata:  *metadata,
		Content:   fileInfo.Content,
		CreatedAt: time.Now(),
	}

	// Store in S3
	if err := vs.s3Client.StoreVector(ctx, vectorData); err != nil {
		return WrapError(err, ErrorTypeS3Upload, fileInfo.Path)
	}

	return nil
}

// processFilesConcurrently processes files with controlled concurrency
func (vs *VectorizerService) processFilesConcurrently(ctx context.Context, files []*FileInfo) (*ProcessingResult, error) {
	// Create semaphore for concurrency control
	semaphore := make(chan struct{}, vs.config.Concurrency)
	var wg sync.WaitGroup
	errorChan := make(chan ProcessingError, len(files))

	// Progress tracking
	var processedCount int64
	totalFiles := int64(len(files))
	var lastReportedPercent int64
	var progressMutex sync.Mutex

	log.Printf("Processing %d files with concurrency limit of %d", len(files), vs.config.Concurrency)

	for i, file := range files {
		wg.Add(1)
		go func(idx int, f *FileInfo) {
			defer wg.Done()

			// Acquire semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			if err := vs.ProcessSingleFile(ctx, f, false); err != nil {
				if procErr, ok := err.(*ProcessingError); ok {
					errorChan <- *procErr
				} else {
					errorChan <- *WrapError(err, ErrorTypeUnknown, f.Path)
				}
				vs.updateStats(false)
			} else {
				vs.updateStats(true)
			}

			// Update progress and report every 10%
			current := atomic.AddInt64(&processedCount, 1)
			percent := (current * 100) / totalFiles

			// Report progress at 10% intervals (thread-safe)
			progressMutex.Lock()
			reportedPercent := atomic.LoadInt64(&lastReportedPercent)
			if percent >= reportedPercent+10 && percent <= 100 {
				newReportedPercent := (percent / 10) * 10
				if atomic.CompareAndSwapInt64(&lastReportedPercent, reportedPercent, newReportedPercent) {
					log.Printf("Progress: %d%% (%d/%d files)", newReportedPercent, current, totalFiles)
				}
			}
			progressMutex.Unlock()
		}(i, file)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(errorChan)

	// Collect errors
	var errors []ProcessingError
	for err := range errorChan {
		errors = append(errors, err)
	}

	endTime := time.Now()
	result := &ProcessingResult{
		ProcessedFiles: len(files),
		SuccessCount:   vs.stats.FilesSuccessful,
		FailureCount:   vs.stats.FilesFailed,
		Errors:         errors,
		StartTime:      vs.stats.StartTime,
		EndTime:        endTime,
		Duration:       endTime.Sub(vs.stats.StartTime),
	}

	log.Printf("Processing completed: %d success, %d failures", result.SuccessCount, result.FailureCount)
	return result, nil
}

// dryRunProcessing simulates processing without making actual API calls
func (vs *VectorizerService) dryRunProcessing(files []*FileInfo) (*ProcessingResult, error) {
	log.Println("Starting dry run processing...")

	for i, file := range files {
		log.Printf("DRY RUN [%d/%d]: %s", i+1, len(files), file.Name)

		// Load content for metadata extraction
		content, err := vs.fileScanner.ReadFileContent(file.Path)
		if err != nil {
			log.Printf("  ERROR reading file: %v", err)
			continue
		}

		// Extract metadata
		metadata, err := vs.metadataExtractor.ExtractMetadata(file.Path, content)
		if err != nil {
			log.Printf("  ERROR extracting metadata: %v", err)
			continue
		}

		log.Printf("  Title: %s", metadata.Title)
		log.Printf("  Category: %s", metadata.Category)
		log.Printf("  Word Count: %d", metadata.WordCount)
		log.Printf("  Tags: %v", metadata.Tags)
		if metadata.Reference != "" {
			log.Printf("  Reference: %s", metadata.Reference)
		}
	}

	endTime := time.Now()
	return &ProcessingResult{
		ProcessedFiles: len(files),
		SuccessCount:   len(files), // All succeed in dry run
		FailureCount:   0,
		Errors:         []ProcessingError{},
		StartTime:      vs.stats.StartTime,
		EndTime:        endTime,
		Duration:       endTime.Sub(vs.stats.StartTime),
	}, nil
}

// updateStats updates processing statistics thread-safely
func (vs *VectorizerService) updateStats(success bool) {
	vs.stats.mu.Lock()
	defer vs.stats.mu.Unlock()

	vs.stats.FilesProcessed++
	if success {
		vs.stats.FilesSuccessful++
		vs.stats.EmbeddingsCreated++
	} else {
		vs.stats.FilesFailed++
	}
}

// GetStats returns current processing statistics
func (vs *VectorizerService) GetStats() ProcessingStats {
	vs.stats.mu.RLock()
	defer vs.stats.mu.RUnlock()

	return ProcessingStats{
		FilesProcessed:    vs.stats.FilesProcessed,
		FilesSuccessful:   vs.stats.FilesSuccessful,
		FilesFailed:       vs.stats.FilesFailed,
		EmbeddingsCreated: vs.stats.EmbeddingsCreated,
		TotalTokens:       vs.stats.TotalTokens,
		StartTime:         vs.stats.StartTime,
		Errors:            append([]ProcessingError{}, vs.stats.Errors...),
	}
}

// GetServiceInfo returns information about the configured services
func (vs *VectorizerService) GetServiceInfo(ctx context.Context) (map[string]interface{}, error) {
	info := make(map[string]interface{})

	// Get embedding model info
	modelName, dimensions, err := vs.embeddingClient.GetModelInfo()
	if err == nil {
		info["embedding_model"] = modelName
		info["embedding_dimensions"] = dimensions
	}

	// Get S3 bucket info
	if bucketInfo, err := vs.s3Client.GetBucketInfo(ctx); err == nil {
		info["s3_info"] = bucketInfo
	}

	// Add configuration info
	info["concurrency"] = vs.config.Concurrency
	info["retry_attempts"] = vs.config.RetryAttempts
	info["retry_delay"] = vs.config.RetryDelay.String()

	return info, nil
}
