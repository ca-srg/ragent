package vectorizer

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ca-srg/ragent/internal/csv"
)

// ProgressCallback is called when processing progress is updated
type ProgressCallback func(processed, total int)

// VectorizerService orchestrates the vectorization process
type VectorizerService struct {
	embeddingClient     EmbeddingClient
	s3Client            S3VectorClient
	opensearchIndexer   OpenSearchIndexer
	metadataExtractor   MetadataExtractor
	fileScanner         FileScanner
	parallelController  *ParallelController
	errorHandler        *DualBackendErrorHandler
	config              *Config
	stats               *ProcessingStats
	enableOpenSearch    bool
	opensearchIndexName string
	csvReader           *csv.Reader
	progressCallback    ProgressCallback
	progressMu          sync.RWMutex
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
	Config              *Config
	EmbeddingClient     EmbeddingClient
	S3Client            S3VectorClient
	OpenSearchIndexer   OpenSearchIndexer
	MetadataExtractor   MetadataExtractor
	FileScanner         FileScanner
	ParallelController  *ParallelController
	ErrorHandler        *DualBackendErrorHandler
	EnableOpenSearch    bool
	OpenSearchIndexName string
	CSVConfig           *csv.Config
}

// NewVectorizerService creates a new vectorizer service with the given configuration
func NewVectorizerService(serviceConfig *ServiceConfig) (*VectorizerService, error) {
	if serviceConfig == nil {
		return nil, fmt.Errorf("service config cannot be nil")
	}

	// Initialize parallel controller if not provided
	var parallelController *ParallelController
	if serviceConfig.ParallelController != nil {
		parallelController = serviceConfig.ParallelController
	} else if serviceConfig.EnableOpenSearch && serviceConfig.OpenSearchIndexer != nil {
		// Create parallel controller for dual backend processing
		parallelController = NewParallelController(
			serviceConfig.S3Client,
			serviceConfig.OpenSearchIndexer,
			serviceConfig.Config.Concurrency,
		)
	}

	// Initialize error handler if not provided
	errorHandler := serviceConfig.ErrorHandler
	if errorHandler == nil {
		errorHandler = NewDualBackendErrorHandler(
			serviceConfig.Config.RetryAttempts,
			serviceConfig.Config.RetryDelay,
		)
	}

	// Initialize CSV reader
	var csvReader *csv.Reader
	if serviceConfig.CSVConfig != nil {
		csvReader = csv.NewReader(serviceConfig.CSVConfig)
	} else {
		csvReader = csv.NewReader(nil) // Use default config with auto-detection
	}

	service := &VectorizerService{
		embeddingClient:     serviceConfig.EmbeddingClient,
		s3Client:            serviceConfig.S3Client,
		opensearchIndexer:   serviceConfig.OpenSearchIndexer,
		metadataExtractor:   serviceConfig.MetadataExtractor,
		fileScanner:         serviceConfig.FileScanner,
		parallelController:  parallelController,
		errorHandler:        errorHandler,
		config:              serviceConfig.Config,
		enableOpenSearch:    serviceConfig.EnableOpenSearch,
		opensearchIndexName: serviceConfig.OpenSearchIndexName,
		csvReader:           csvReader,
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
		return fmt.Errorf("embedding service validation failed: %w\nGuidance: Please check your AWS credentials and Bedrock service access", err)
	}
	log.Println("✓ Embedding service connection validated successfully")

	log.Println("Validating S3 bucket access...")
	if err := vs.s3Client.ValidateAccess(ctx); err != nil {
		return fmt.Errorf("S3 bucket validation failed: %w\nGuidance: Please check your S3 bucket name and AWS credentials", err)
	}
	log.Println("✓ S3 bucket access validated successfully")

	// Validate OpenSearch connection if enabled
	if vs.enableOpenSearch {
		if err := vs.validateOpenSearchConnection(ctx); err != nil {
			return fmt.Errorf("OpenSearch validation failed: %w", err)
		}
	} else {
		log.Println("OpenSearch integration disabled - skipping OpenSearch validation")
	}

	log.Println("All configured services validated successfully")
	return nil
}

// VectorizeMarkdownFiles processes all supported files (markdown and CSV) in a directory
func (vs *VectorizerService) VectorizeMarkdownFiles(ctx context.Context, directory string, dryRun bool) (*ProcessingResult, error) {
	vs.stats.StartTime = time.Now()

	log.Printf("Scanning directory: %s", directory)
	files, err := vs.fileScanner.ScanDirectory(directory)
	if err != nil {
		return nil, fmt.Errorf("failed to scan directory: %w", err)
	}

	if len(files) == 0 {
		log.Println("No supported files found (markdown or CSV)")
		return vs.createEmptyResult(), nil
	}

	// Count file types
	mdCount, csvCount := 0, 0
	for _, f := range files {
		if f.IsMarkdown {
			mdCount++
		} else if f.IsCSV {
			csvCount++
		}
	}
	log.Printf("Found %d files to process (%d markdown, %d CSV)", len(files), mdCount, csvCount)

	// Expand CSV files into individual rows
	expandedFiles, err := vs.expandCSVFiles(files)
	if err != nil {
		return nil, fmt.Errorf("failed to expand CSV files: %w", err)
	}

	if len(expandedFiles) != len(files) {
		log.Printf("After CSV expansion: %d total documents to process", len(expandedFiles))
	}

	return vs.VectorizeFiles(ctx, expandedFiles, dryRun)
}

// expandCSVFiles expands CSV files into individual FileInfo entries (one per row)
func (vs *VectorizerService) expandCSVFiles(files []*FileInfo) ([]*FileInfo, error) {
	var result []*FileInfo

	for _, file := range files {
		if file.IsCSV {
			// Expand CSV file into rows
			log.Printf("Expanding CSV file: %s", file.Path)
			csvFiles, err := vs.csvReader.ReadFile(file.Path)
			if err != nil {
				return nil, fmt.Errorf("failed to read CSV file %s: %w", file.Path, err)
			}
			log.Printf("  Expanded to %d rows", len(csvFiles))
			result = append(result, csvFiles...)
		} else {
			// Keep markdown files as-is
			result = append(result, file)
		}
	}

	return result, nil
}

// VectorizeFiles processes a slice of FileInfo objects
// This can be used for both markdown files and spreadsheet rows
func (vs *VectorizerService) VectorizeFiles(ctx context.Context, files []*FileInfo, dryRun bool) (*ProcessingResult, error) {
	vs.stats.StartTime = time.Now()

	if len(files) == 0 {
		log.Println("No files to process")
		return vs.createEmptyResult(), nil
	}

	log.Printf("Processing %d files", len(files))

	// Determine processing mode
	if vs.enableOpenSearch && vs.parallelController != nil {
		log.Printf("Using dual backend processing (S3 Vector + OpenSearch) with index: %s",
			vs.opensearchIndexName)
		return vs.processDualBackend(ctx, files, dryRun)
	} else {
		log.Println("Using single backend processing (S3 Vector only)")
		if dryRun {
			log.Println("Dry run mode: will not actually create embeddings or upload to S3")
			return vs.dryRunProcessing(files)
		}
		// Fallback to original single backend processing
		return vs.processFilesConcurrently(ctx, files)
	}
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
	log.Printf("Generating embedding for file: %s", fileInfo.Path)
	embedding, err := vs.embeddingClient.GenerateEmbedding(ctx, fileInfo.Content)
	if err != nil {
		log.Printf("ERROR: Failed to generate embedding for %s: %v", fileInfo.Path, err)
		return WrapError(err, ErrorTypeEmbedding, fileInfo.Path)
	}

	// Validate embedding is not empty
	if len(embedding) == 0 {
		log.Printf("ERROR: Generated embedding is empty for file: %s", fileInfo.Path)
		return WrapError(fmt.Errorf("embedding is empty"), ErrorTypeEmbedding, fileInfo.Path)
	}

	log.Printf("Successfully generated embedding with %d dimensions for file: %s", len(embedding), fileInfo.Path)

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

			// Notify progress callback
			vs.notifyProgress(int(current), int(totalFiles))

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

	// Log OpenSearch simulation if enabled
	if vs.enableOpenSearch && vs.opensearchIndexer != nil {
		indexName := vs.opensearchIndexName
		if indexName == "" {
			indexName = "kiberag-vectors"
		}
		log.Printf("DRY RUN: OpenSearch indexing simulation enabled for index: %s", indexName)
		log.Println("DRY RUN: Would create/verify OpenSearch index with Japanese-optimized mappings")
		log.Println("DRY RUN: Would index documents with BM25 full-text search and k-NN vector search capabilities")
	} else {
		log.Println("DRY RUN: OpenSearch indexing disabled - S3 Vector only")
	}

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

		// Simulate processing actions
		documentID := vs.metadataExtractor.GenerateKey(metadata)
		log.Printf("  Document ID: %s", documentID)
		log.Printf("  Would generate embedding vector (1024 dimensions)")
		log.Printf("  Would store to S3 Vector with metadata")

		if vs.enableOpenSearch && vs.opensearchIndexer != nil {
			log.Printf("  Would index to OpenSearch with:")
			log.Printf("    - BM25 searchable content (Japanese-optimized)")
			log.Printf("    - k-NN vector field for semantic search")
			log.Printf("    - Structured metadata fields (category, tags, date)")
			if len(content) > 1000 {
				log.Printf("    - Content preview: %s...", content[:100])
			} else {
				log.Printf("    - Full content length: %d characters", len(content))
			}
		}
	}

	endTime := time.Now()
	result := &ProcessingResult{
		ProcessedFiles:           len(files),
		SuccessCount:             len(files), // All succeed in dry run
		FailureCount:             0,
		Errors:                   []ProcessingError{},
		StartTime:                vs.stats.StartTime,
		EndTime:                  endTime,
		Duration:                 endTime.Sub(vs.stats.StartTime),
		OpenSearchEnabled:        vs.enableOpenSearch,
		OpenSearchSuccessCount:   0,
		OpenSearchFailureCount:   0,
		OpenSearchIndexedCount:   0,
		OpenSearchSkippedCount:   0,
		OpenSearchRetryCount:     0,
		OpenSearchProcessingTime: 0,
	}

	if vs.enableOpenSearch {
		result.OpenSearchSuccessCount = len(files) // All would succeed in dry run
		result.OpenSearchIndexedCount = len(files)
	}

	log.Printf("DRY RUN: Simulation completed for %d files", len(files))
	if vs.enableOpenSearch {
		log.Printf("DRY RUN: Would have indexed %d documents to both S3 Vector and OpenSearch", len(files))
	} else {
		log.Printf("DRY RUN: Would have indexed %d documents to S3 Vector only", len(files))
	}

	return result, nil
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

// SetProgressCallback sets a callback function to be called when progress is updated
func (vs *VectorizerService) SetProgressCallback(callback ProgressCallback) {
	vs.progressMu.Lock()
	defer vs.progressMu.Unlock()
	vs.progressCallback = callback
}

// notifyProgress calls the progress callback if set
func (vs *VectorizerService) notifyProgress(processed, total int) {
	vs.progressMu.RLock()
	callback := vs.progressCallback
	vs.progressMu.RUnlock()

	if callback != nil {
		callback(processed, total)
	}
}

// processDualBackend processes files using both S3 Vector and OpenSearch backends
func (vs *VectorizerService) processDualBackend(ctx context.Context, files []*FileInfo, dryRun bool) (*ProcessingResult, error) {
	log.Printf("Starting dual backend processing for %d files", len(files))

	// Validate OpenSearch index exists or create it if needed
	if !dryRun && vs.opensearchIndexer != nil {
		if err := vs.ensureOpenSearchIndex(ctx); err != nil {
			log.Printf("Warning: Failed to ensure OpenSearch index: %v", err)
			// Don't fail the entire operation, just log the warning
		}
	}

	// Propagate progress callback to parallel controller
	vs.progressMu.RLock()
	callback := vs.progressCallback
	vs.progressMu.RUnlock()
	if callback != nil {
		vs.parallelController.SetProgressCallback(callback)
	}

	// Use parallel controller for dual backend processing
	result, err := vs.parallelController.ProcessFiles(
		ctx,
		files,
		vs.opensearchIndexName,
		vs.embeddingClient,
		vs.metadataExtractor,
		dryRun,
	)

	if err != nil {
		return nil, fmt.Errorf("dual backend processing failed: %w", err)
	}

	log.Printf("Dual backend processing completed: %d files processed, %d successful, %d failed",
		result.ProcessedFiles, result.SuccessCount, result.FailureCount)

	return result, nil
}

// ensureOpenSearchIndex ensures the OpenSearch index exists with proper mappings
func (vs *VectorizerService) ensureOpenSearchIndex(ctx context.Context) error {
	if vs.opensearchIndexer == nil {
		return fmt.Errorf("OpenSearch indexer not available")
	}

	indexName := vs.opensearchIndexName
	if indexName == "" {
		indexName = "kiberag-vectors" // Default index name
	}

	// Check if index exists
	exists, err := vs.opensearchIndexer.IndexExists(ctx, indexName)
	if err != nil {
		return fmt.Errorf("failed to check index existence: %w", err)
	}

	if !exists {
		log.Printf("Creating OpenSearch index: %s", indexName)
		// Create index with Japanese-optimized mappings
		if osIndexer, ok := vs.opensearchIndexer.(*OpenSearchIndexerImpl); ok {
			// Use the Japanese-optimized index creation
			err = osIndexer.CreateVectorIndexWithJapanese(ctx, indexName, 1024)
		} else {
			// Fallback to standard index creation
			err = vs.opensearchIndexer.CreateIndex(ctx, indexName, 1024)
		}

		if err != nil {
			return fmt.Errorf("failed to create OpenSearch index: %w", err)
		}
		log.Printf("Successfully created OpenSearch index: %s", indexName)
	} else {
		log.Printf("OpenSearch index already exists: %s", indexName)
	}

	return nil
}

// validateOpenSearchConnection validates OpenSearch connection with detailed logging and error guidance
func (vs *VectorizerService) validateOpenSearchConnection(ctx context.Context) error {
	log.Println("Validating OpenSearch connection...")

	// Check if OpenSearch indexer is available
	if vs.opensearchIndexer == nil {
		return fmt.Errorf("OpenSearch indexer is not initialized\nGuidance: Ensure OPENSEARCH_ENDPOINT environment variable is set and OpenSearch client is properly configured")
	}

	// Log OpenSearch configuration details
	indexName := vs.opensearchIndexName
	if indexName == "" {
		indexName = "kiberag-vectors" // Default index name
	}
	log.Printf("Target OpenSearch index: %s", indexName)

	// Test OpenSearch connection
	log.Println("Testing OpenSearch cluster connection...")
	if err := vs.opensearchIndexer.ValidateConnection(ctx); err != nil {
		// Provide specific error guidance based on error type
		errorGuidance := vs.generateOpenSearchErrorGuidance(err)
		return fmt.Errorf("OpenSearch connection test failed: %w\n%s", err, errorGuidance)
	}

	log.Println("✓ OpenSearch connection validated successfully")

	// Test index operations (non-destructive)
	log.Printf("Checking OpenSearch index status for: %s", indexName)
	exists, err := vs.opensearchIndexer.IndexExists(ctx, indexName)
	if err != nil {
		log.Printf("Warning: Failed to check index existence: %v", err)
		log.Println("Index will be created during processing if needed")
	} else if exists {
		log.Printf("✓ OpenSearch index '%s' already exists", indexName)

		// Get index information if available
		if info, err := vs.opensearchIndexer.GetIndexInfo(ctx, indexName); err == nil {
			if docCount, ok := info["document_count"].(int64); ok && docCount > 0 {
				log.Printf("  Existing documents: %d", docCount)
			}
		}
	} else {
		log.Printf("OpenSearch index '%s' does not exist - will be created during processing", indexName)
	}

	log.Println("✓ OpenSearch validation completed successfully")
	return nil
}

// generateOpenSearchErrorGuidance provides specific guidance based on OpenSearch error types
func (vs *VectorizerService) generateOpenSearchErrorGuidance(err error) string {
	errorStr := err.Error()

	if contains(errorStr, "connection refused") || contains(errorStr, "no such host") {
		return "Guidance: Please check your OPENSEARCH_ENDPOINT setting and ensure the OpenSearch cluster is running and accessible"
	}

	if contains(errorStr, "unauthorized") || contains(errorStr, "authentication") {
		return "Guidance: Please check your OPENSEARCH_USERNAME and OPENSEARCH_PASSWORD credentials, or verify AWS IAM permissions for OpenSearch access"
	}

	if contains(errorStr, "timeout") {
		return "Guidance: OpenSearch cluster may be overloaded or network connectivity is slow. Consider increasing timeout settings or checking network connectivity"
	}

	if contains(errorStr, "forbidden") || contains(errorStr, "permission") {
		return "Guidance: Your credentials may be correct, but lack sufficient permissions. Please verify your user has appropriate index and cluster permissions"
	}

	if contains(errorStr, "ssl") || contains(errorStr, "tls") || contains(errorStr, "certificate") {
		return "Guidance: SSL/TLS connection issue. Please check if your OpenSearch endpoint uses HTTPS and certificate validation is properly configured"
	}

	return "Guidance: Please check your OpenSearch endpoint configuration and ensure the cluster is accessible. See OpenSearch logs for more detailed error information"
}

// contains is a helper function for case-insensitive string containment check
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		strings.Contains(strings.ToLower(s), strings.ToLower(substr)))
}

// createEmptyResult creates an empty processing result
func (vs *VectorizerService) createEmptyResult() *ProcessingResult {
	now := time.Now()
	return &ProcessingResult{
		ProcessedFiles:           0,
		SuccessCount:             0,
		FailureCount:             0,
		Errors:                   []ProcessingError{},
		StartTime:                vs.stats.StartTime,
		EndTime:                  now,
		Duration:                 now.Sub(vs.stats.StartTime),
		OpenSearchEnabled:        vs.enableOpenSearch,
		OpenSearchSuccessCount:   0,
		OpenSearchFailureCount:   0,
		OpenSearchIndexedCount:   0,
		OpenSearchSkippedCount:   0,
		OpenSearchRetryCount:     0,
		OpenSearchProcessingTime: 0,
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

	// Add OpenSearch info if enabled
	if vs.enableOpenSearch && vs.opensearchIndexer != nil {
		info["opensearch_enabled"] = true
		info["opensearch_index"] = vs.opensearchIndexName

		// Get OpenSearch index info
		if indexInfo, err := vs.opensearchIndexer.GetIndexInfo(ctx, vs.opensearchIndexName); err == nil {
			info["opensearch_info"] = indexInfo
		}
	} else {
		info["opensearch_enabled"] = false
	}

	// Add configuration info
	info["concurrency"] = vs.config.Concurrency
	info["retry_attempts"] = vs.config.RetryAttempts
	info["retry_delay"] = vs.config.RetryDelay.String()
	info["dual_backend_mode"] = vs.enableOpenSearch && vs.parallelController != nil

	return info, nil
}
