package cmd

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/spf13/cobra"

	appconfig "github.com/ca-srg/ragent/internal/config"
	"github.com/ca-srg/ragent/internal/csv"
	"github.com/ca-srg/ragent/internal/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/hashstore"
	"github.com/ca-srg/ragent/internal/ipc"
	"github.com/ca-srg/ragent/internal/metadata"
	"github.com/ca-srg/ragent/internal/s3vector"
	"github.com/ca-srg/ragent/internal/scanner"
	"github.com/ca-srg/ragent/internal/spreadsheet"
	"github.com/ca-srg/ragent/internal/types"
	"github.com/ca-srg/ragent/internal/vectorizer"
)

const (
	defaultFollowInterval = "30m"
	minFollowInterval     = 5 * time.Minute
)

var (
	directory           string
	dryRun              bool
	concurrency         int
	clearVectors        bool
	openSearchIndexName string

	followMode             bool
	followInterval         string
	followIntervalDuration time.Duration
	followModeProcessing   atomic.Bool

	// Spreadsheet mode
	spreadsheetConfigPath string

	// CSV mode
	csvConfigPath string

	// S3 source mode
	enableS3       bool
	s3Bucket       string
	s3Prefix       string
	s3VectorRegion string
	s3SourceRegion string

	// Incremental processing options
	forceProcess bool // Force re-vectorization of all files
	pruneDeleted bool // Remove vectors for deleted files
)

// ProgressCallback is called when processing progress is updated
type ProgressCallback func(processed, total int)

var vectorizationRunner = executeVectorizationOnce

// currentProgressCallback is set by follow mode to receive progress updates
var currentProgressCallback ProgressCallback

var vectorizeCmd = &cobra.Command{
	Use:   "vectorize",
	Short: "Convert source files (markdown and CSV) to vectors and store in S3",
	Long: `
The vectorize command processes source files (markdown and CSV) in a directory,
extracts metadata, generates embeddings using Amazon Bedrock,
and stores the vectors in Amazon S3.

Supported file types:
  - Markdown (.md, .markdown): Each file becomes one document
  - CSV (.csv): Each row becomes one document (header row required)

This enables the creation of a vector database from your documentation
and data files for RAG (Retrieval Augmented Generation) applications.
`,
	RunE: runVectorize,
}

func init() {
	vectorizeCmd.Flags().StringVarP(&directory, "directory", "d", "./source", "Directory containing source files to process (markdown and CSV)")
	vectorizeCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be processed without making API calls")
	vectorizeCmd.Flags().IntVarP(&concurrency, "concurrency", "c", 0, "Number of concurrent operations (0 = use config default)")
	vectorizeCmd.Flags().BoolVar(&clearVectors, "clear", false, "Delete all existing vectors before processing new ones")
	vectorizeCmd.Flags().BoolVar(&followMode, "follow", false, "Continuously vectorize at a fixed interval")
	vectorizeCmd.Flags().StringVar(&followInterval, "interval", defaultFollowInterval, "Interval between vectorization runs in follow mode (e.g. 30m, 1h)")
	vectorizeCmd.Flags().StringVar(&spreadsheetConfigPath, "spreadsheet-config", "", "Path to spreadsheet configuration YAML file (enables spreadsheet mode)")
	vectorizeCmd.Flags().StringVar(&csvConfigPath, "csv-config", "", "Path to CSV configuration YAML file (for column mapping)")

	// Incremental processing options
	vectorizeCmd.Flags().BoolVarP(&forceProcess, "force", "f", false, "Force re-vectorization of all files, ignoring hash cache")
	vectorizeCmd.Flags().BoolVar(&pruneDeleted, "prune", false, "Remove vectors for files that no longer exist")

	// S3 source options
	vectorizeCmd.Flags().BoolVar(&enableS3, "enable-s3", false, "Enable S3 source file fetching")
	vectorizeCmd.Flags().StringVar(&s3Bucket, "s3-bucket", "", "S3 bucket name for source files (required when --enable-s3 is set)")
	vectorizeCmd.Flags().StringVar(&s3Prefix, "s3-prefix", "", "S3 prefix (directory) to scan (optional, defaults to bucket root)")

	// S3 region options
	vectorizeCmd.Flags().StringVar(&s3VectorRegion, "s3-vector-region", "", "AWS region for S3 Vector bucket (overrides S3_VECTOR_REGION, default: us-east-1)")
	vectorizeCmd.Flags().StringVar(&s3SourceRegion, "s3-source-region", "", "AWS region for source S3 bucket (overrides S3_SOURCE_REGION, default: us-east-1)")
}

func runVectorize(cmd *cobra.Command, args []string) error {
	log.Println("Starting vectorization process...")

	if err := validateOpenSearchFlags(); err != nil {
		return fmt.Errorf("flag validation failed: %w", err)
	}

	if err := validateFollowModeFlags(cmd); err != nil {
		return fmt.Errorf("follow mode flag validation failed: %w", err)
	}

	cfg, err := appconfig.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	if concurrency > 0 {
		cfg.Concurrency = concurrency
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	// Check for spreadsheet mode
	if spreadsheetConfigPath != "" {
		return runSpreadsheetVectorize(ctx, cfg)
	}

	if followMode {
		return runFollowMode(ctx, cfg)
	}

	result, err := vectorizationRunner(ctx, cfg)
	if err != nil {
		return err
	}

	if result != nil {
		printResults(result, dryRun)
	}

	return nil
}

func executeVectorizationOnce(ctx context.Context, cfg *types.Config) (*types.ProcessingResult, error) {
	return executeVectorizationOnceWithProgress(ctx, cfg, currentProgressCallback)
}

func executeVectorizationOnceWithProgress(ctx context.Context, cfg *types.Config, progressCallback ProgressCallback) (*types.ProcessingResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	// Validate S3 flags
	if enableS3 && s3Bucket == "" {
		return nil, fmt.Errorf("--s3-bucket is required when --enable-s3 is set")
	}

	// Validate at least one source is specified
	hasLocalSource := directory != ""
	hasS3Source := enableS3

	if !hasLocalSource && !hasS3Source {
		return nil, fmt.Errorf("at least one source must be specified: --directory or --enable-s3 with --s3-bucket")
	}

	// Validate local directory if specified
	if hasLocalSource {
		if _, err := os.Stat(directory); os.IsNotExist(err) {
			return nil, fmt.Errorf("directory does not exist: %s", directory)
		}
	}

	if clearVectors && !dryRun {
		if !confirmDeletePrompt(openSearchIndexName) {
			fmt.Println("Operation cancelled by user.")
			return nil, nil
		}

		s3Config := &s3vector.S3Config{
			VectorBucketName: cfg.AWSS3VectorBucket,
			IndexName:        cfg.AWSS3VectorIndex,
			Region:           resolveS3VectorRegion(cfg),
			MaxRetries:       cfg.RetryAttempts,
			RetryDelay:       cfg.RetryDelay,
		}
		s3Client, err := s3vector.NewS3VectorService(s3Config)
		if err != nil {
			return nil, fmt.Errorf("failed to create S3 client for deletion: %w", err)
		}

		log.Printf("S3 Vector index name: %s", cfg.AWSS3VectorIndex)
		log.Printf("S3 Vector bucket name: %s", cfg.AWSS3VectorBucket)
		log.Println("Deleting all existing vectors from S3 Vector index...")
		timedCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()

		deletedCount, err := s3Client.DeleteAllVectors(timedCtx)
		if err != nil {
			return nil, fmt.Errorf("failed to delete existing vectors: %w", err)
		}

		log.Printf("Successfully deleted %d vectors from S3 Vector index (%s) in bucket (%s)", deletedCount, cfg.AWSS3VectorIndex, cfg.AWSS3VectorBucket)

		log.Printf("Deleting OpenSearch index: %s", openSearchIndexName)
		tempService, err := createVectorizerService(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create service for OpenSearch deletion: %w", err)
		}

		deleteCtx, deleteCancel := context.WithTimeout(ctx, 2*time.Minute)
		defer deleteCancel()

		if tempService != nil {
			serviceFactory := vectorizer.NewServiceFactory(cfg)
			if serviceFactory != nil {
				indexerFactory := vectorizer.NewIndexerFactory(cfg)
				if indexerFactory.IsOpenSearchEnabled() {
					indexer, err := indexerFactory.CreateOpenSearchIndexer(openSearchIndexName, 1024)
					if err != nil {
						log.Printf("Warning: Failed to create OpenSearch indexer for deletion: %v", err)
					} else {
						if err := indexer.DeleteIndex(deleteCtx, openSearchIndexName); err != nil {
							log.Printf("Warning: Failed to delete OpenSearch index: %v", err)
						} else {
							log.Printf("Successfully deleted OpenSearch index: %s", openSearchIndexName)
							log.Printf("Recreating OpenSearch index: %s", openSearchIndexName)
							if err := indexer.CreateIndex(deleteCtx, openSearchIndexName, 1024); err != nil {
								log.Printf("Warning: Failed to recreate OpenSearch index: %v", err)
							} else {
								log.Printf("Successfully recreated OpenSearch index: %s", openSearchIndexName)
							}
						}
					}
				}
			}
		}

		return nil, nil
	}

	if clearVectors && dryRun {
		log.Printf("DRY RUN: Would delete and recreate OpenSearch index: %s", openSearchIndexName)
		log.Println("DRY RUN: Would delete all existing vectors from S3 Vector index")
		return nil, nil
	}

	// Load CSV configuration if provided
	var csvCfg *csv.Config
	if csvConfigPath != "" {
		log.Printf("Loading CSV configuration: %s", csvConfigPath)
		var err error
		csvCfg, err = csv.LoadConfig(csvConfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load CSV config: %w", err)
		}
		log.Println("CSV configuration loaded successfully")
	}

	service, err := createVectorizerServiceWithCSVConfig(cfg, csvCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create vectorizer service: %w", err)
	}

	// Set progress callback if provided
	if progressCallback != nil {
		service.SetProgressCallback(func(processed, total int) {
			progressCallback(processed, total)
		})
	}

	if !dryRun {
		log.Println("Validating configuration and service connections...")
		validationCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		if err := service.ValidateConfiguration(validationCtx); err != nil {
			return nil, fmt.Errorf("configuration validation failed: %w", err)
		}
		log.Println("Configuration validation successful")
	}

	// Collect files from all sources
	var allFiles []*types.FileInfo

	// 1. Scan local directory if specified
	if hasLocalSource {
		log.Printf("Scanning local directory: %s", directory)
		localFiles, err := scanLocalDirectoryWithHash(directory)
		if err != nil {
			return nil, fmt.Errorf("failed to scan local directory: %w", err)
		}
		log.Printf("Found %d files in local directory", len(localFiles))
		allFiles = append(allFiles, localFiles...)
	}

	// 2. Scan S3 bucket if enabled
	if hasS3Source {
		log.Printf("Scanning S3 bucket: s3://%s/%s", s3Bucket, s3Prefix)

		// Validate S3 bucket access first (unless dry run)
		s3Scanner, err := scanner.NewS3Scanner(s3Bucket, s3Prefix, resolveS3SourceRegion(cfg))
		if err != nil {
			return nil, fmt.Errorf("failed to create S3 scanner: %w", err)
		}

		if !dryRun {
			if err := s3Scanner.ValidateBucket(ctx); err != nil {
				return nil, fmt.Errorf("S3 bucket validation failed: %w", err)
			}
		}

		s3Files, err := s3Scanner.ScanBucket(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to scan S3 bucket: %w", err)
		}
		log.Printf("Found %d files in S3 bucket", len(s3Files))

		// Download content for S3 files and compute hash if needed
		// For CSV files, download even in dry-run mode to show configuration preview
		for _, f := range s3Files {
			shouldDownload := !dryRun || f.IsCSV
			if shouldDownload {
				content, hash, err := s3Scanner.DownloadFileWithHash(ctx, f.Path)
				if err != nil {
					log.Printf("Warning: Failed to download S3 file %s: %v", f.Path, err)
					if !dryRun {
						continue
					}
					// In dry-run, still add file but without content
				} else {
					f.Content = content
					// Use computed hash if ETag was not available (multipart upload)
					if f.ContentHash == "" {
						f.ContentHash = hash
					}
				}
			}
			allFiles = append(allFiles, f)
		}
	}

	if len(allFiles) == 0 {
		log.Println("No supported files found")
		return &types.ProcessingResult{
			ProcessedFiles: 0,
			SuccessCount:   0,
			FailureCount:   0,
		}, nil
	}

	log.Printf("Total files found: %d", len(allFiles))

	// Change detection using hash store (unless --force is specified)
	var changeResult *hashstore.ChangeDetectionResult
	var hashStore *hashstore.HashStore
	var filesToProcess []*types.FileInfo

	if !forceProcess && !dryRun {
		var err error
		hashStore, err = hashstore.NewHashStore()
		if err != nil {
			log.Printf("Warning: Failed to initialize hash store, processing all files: %v", err)
			filesToProcess = allFiles
		} else {
			defer func() { _ = hashStore.Close() }()

			// Determine source types for change detection
			var sourceTypes []string
			if hasS3Source && hasLocalSource {
				sourceTypes = []string{"local", "s3"}
			} else if hasS3Source {
				sourceTypes = []string{"s3"}
			} else {
				sourceTypes = []string{"local"}
			}

			detector := hashstore.NewChangeDetector(hashStore)
			filesToProcess, changeResult, err = detector.FilterFilesToProcess(ctx, sourceTypes, allFiles)
			if err != nil {
				log.Printf("Warning: Change detection failed, processing all files: %v", err)
				filesToProcess = allFiles
				changeResult = nil
			}
		}
	} else {
		filesToProcess = allFiles
		if forceProcess {
			log.Println("Force mode: processing all files (ignoring hash cache)")
		}
	}

	// Print change detection summary
	if changeResult != nil {
		printChangeDetectionSummary(changeResult)
	}

	if len(filesToProcess) == 0 && !dryRun {
		log.Println("No files need processing (all files are unchanged)")
		return &types.ProcessingResult{
			ProcessedFiles: 0,
			SuccessCount:   0,
			FailureCount:   0,
		}, nil
	}

	log.Printf("Files to process: %d", len(filesToProcess))

	// Print CSV configuration info in dry-run mode
	if dryRun {
		printCSVConfigInfo(allFiles, csvCfg)
	}

	// Use VectorizeFiles for combined processing
	result, err := service.VectorizeFiles(ctx, filesToProcess, dryRun)
	if err != nil {
		return nil, fmt.Errorf("vectorization failed: %w", err)
	}

	// Update hash store for successfully processed files
	if hashStore != nil && result != nil && result.SuccessCount > 0 && !dryRun {
		updateHashStoreForSuccessfulFiles(ctx, hashStore, filesToProcess, result)
	}

	// Handle pruning of deleted files
	if pruneDeleted && changeResult != nil && len(changeResult.Deleted) > 0 && !dryRun {
		log.Printf("Pruning %d deleted files from hash store...", len(changeResult.Deleted))
		for _, deletedPath := range changeResult.Deleted {
			sourceType := "local"
			if strings.HasPrefix(deletedPath, "s3://") {
				sourceType = "s3"
			}
			if err := hashStore.DeleteFileHash(ctx, sourceType, deletedPath); err != nil {
				log.Printf("Warning: Failed to delete hash for %s: %v", deletedPath, err)
			}
		}
		// TODO: Also delete vectors from S3 Vectors and OpenSearch
		log.Printf("Note: Vector deletion from backends not yet implemented")
	}

	return result, nil
}

// scanLocalDirectoryWithHash scans a local directory and computes MD5 hash for each file
func scanLocalDirectoryWithHash(dirPath string) ([]*types.FileInfo, error) {
	fileScanner := scanner.NewFileScanner()
	files, err := fileScanner.ScanDirectory(dirPath)
	if err != nil {
		return nil, err
	}

	// Load content and compute hash for each file
	for _, f := range files {
		if err := fileScanner.LoadFileWithContentAndHash(f); err != nil {
			log.Printf("Warning: Failed to load file %s: %v", f.Path, err)
			continue
		}
	}

	return files, nil
}

// printChangeDetectionSummary prints a summary of detected changes
func printChangeDetectionSummary(result *hashstore.ChangeDetectionResult) {
	fmt.Println("\n" + strings.Repeat("-", 40))
	fmt.Println("Change Detection Summary:")
	fmt.Println(strings.Repeat("-", 40))
	fmt.Printf("  New:       %d files\n", result.NewCount)
	fmt.Printf("  Modified:  %d files\n", result.ModCount)
	fmt.Printf("  Unchanged: %d files (skipped)\n", result.UnchangeCount)
	fmt.Printf("  Deleted:   %d files\n", result.DeleteCount)
	fmt.Println(strings.Repeat("-", 40))
}

// updateHashStoreForSuccessfulFiles updates the hash store after successful processing.
// Only files that were successfully processed (not in the error list) are updated.
func updateHashStoreForSuccessfulFiles(
	ctx context.Context,
	store *hashstore.HashStore,
	files []*types.FileInfo,
	result *types.ProcessingResult,
) {
	// Build a set of failed file paths from processing errors
	failedPaths := make(map[string]bool)
	for _, procErr := range result.Errors {
		failedPaths[procErr.FilePath] = true
	}

	successCount := 0
	skippedCount := 0
	for _, f := range files {
		// Skip files without content hash
		if f.ContentHash == "" {
			continue
		}

		// Skip files that failed processing
		if failedPaths[f.Path] {
			skippedCount++
			continue
		}

		sourceType := f.SourceType
		if sourceType == "" {
			sourceType = "local"
		}

		record := &hashstore.FileHashRecord{
			SourceType:   sourceType,
			FilePath:     f.Path,
			ContentHash:  f.ContentHash,
			FileSize:     f.Size,
			VectorizedAt: time.Now(),
		}

		if err := store.UpsertFileHash(ctx, record); err != nil {
			log.Printf("Warning: Failed to update hash for %s: %v", f.Path, err)
		} else {
			successCount++
		}
	}

	if successCount > 0 {
		log.Printf("Updated hash store for %d files", successCount)
	}
	if skippedCount > 0 {
		log.Printf("Skipped hash update for %d failed files", skippedCount)
	}
}

func startFollowProcessing() bool {
	return followModeProcessing.CompareAndSwap(false, true)
}

func finishFollowProcessing() {
	followModeProcessing.Store(false)
}

func setupSignalHandler(parent context.Context) (context.Context, context.CancelFunc) {
	baseCtx := parent
	if baseCtx == nil {
		baseCtx = context.Background()
	}

	signalCtx, stop := signal.NotifyContext(baseCtx, syscall.SIGINT, syscall.SIGTERM)
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		select {
		case <-signalChan:
			log.Println("[Follow Mode] Shutdown signal received. Waiting for current vectorization to complete...")
			stop()
		case <-signalCtx.Done():
		}
		signal.Stop(signalChan)
		close(signalChan)
	}()

	return signalCtx, stop
}

func runFollowMode(ctx context.Context, cfg *types.Config) error {
	followCtx, cancel := setupSignalHandler(ctx)
	defer cancel()

	interval := followIntervalDuration
	if interval <= 0 {
		interval = 30 * time.Minute
	}

	// Start IPC server for cross-process status sharing
	ipcLogger := log.New(os.Stdout, "[ipc] ", log.LstdFlags)
	ipcServer, err := ipc.NewServer(ipc.ServerConfig{}, ipcLogger)
	if err != nil {
		if err == ipc.ErrAnotherInstanceRunning {
			return fmt.Errorf("another vectorize process is already running")
		}
		log.Printf("[Follow Mode] Warning: Failed to start IPC server: %v", err)
		// Continue without IPC - degraded mode
	} else {
		if err := ipcServer.Start(followCtx); err != nil {
			log.Printf("[Follow Mode] Warning: Failed to start IPC server: %v", err)
		} else {
			defer func() { _ = ipcServer.Shutdown(context.Background()) }()
			log.Println("[Follow Mode] IPC server started for status sharing")
		}
	}

	log.Printf("Follow mode enabled. Interval: %s. Press Ctrl+C to stop.", interval)

	result, err := runFollowCycleWithIPC(followCtx, cfg, ipcServer)
	if err != nil {
		log.Printf("[Follow Mode] Vectorization cycle failed: %v", err)
	} else if result != nil {
		nextRun := time.Now().Add(interval)
		log.Printf("[Follow Mode] Completed. Processed %d files. Next run at: %s", result.ProcessedFiles, nextRun.Format(time.RFC3339))
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-followCtx.Done():
			log.Println("[Follow Mode] Shutdown complete.")
			return nil
		case <-ipcServer.StopChan():
			log.Println("[Follow Mode] Stop requested via IPC.")
			return nil
		case <-ticker.C:
			result, err := runFollowCycleWithIPC(followCtx, cfg, ipcServer)
			if err != nil {
				log.Printf("[Follow Mode] Vectorization cycle failed: %v", err)
				continue
			}

			if result != nil {
				nextRun := time.Now().Add(interval)
				log.Printf("[Follow Mode] Completed. Processed %d files. Next run at: %s", result.ProcessedFiles, nextRun.Format(time.RFC3339))
			}
		}
	}
}

// runFollowCycleWithIPC runs a single vectorization cycle with IPC status updates
func runFollowCycleWithIPC(ctx context.Context, cfg *types.Config, ipcServer *ipc.Server) (*types.ProcessingResult, error) {
	if !startFollowProcessing() {
		log.Println("[Follow Mode] Previous vectorization still running. Skipping this cycle.")
		return nil, nil
	}

	defer finishFollowProcessing()

	// Update IPC status to running
	if ipcServer != nil {
		startTime := time.Now()
		ipcServer.SetStateWithTime(ipc.StateRunning, startTime)
	}

	log.Println("[Follow Mode] Starting vectorization cycle...")

	// Set progress callback for IPC updates
	if ipcServer != nil {
		currentProgressCallback = func(processed, total int) {
			percentage := 0.0
			if total > 0 {
				percentage = float64(processed) / float64(total) * 100.0
			}
			ipcServer.UpdateProgress(&ipc.ProgressResponse{
				TotalFiles:     total,
				ProcessedFiles: processed,
				Percentage:     percentage,
			})
		}
	} else {
		currentProgressCallback = nil
	}
	defer func() {
		currentProgressCallback = nil
	}()

	result, err := vectorizationRunner(ctx, cfg)
	if err != nil {
		// Update IPC status to error
		if ipcServer != nil {
			ipcServer.UpdateStatus(&ipc.StatusResponse{
				State: ipc.StateError,
				Error: err.Error(),
				PID:   os.Getpid(),
			})
		}
		return nil, err
	}

	// Update IPC status to waiting (for next follow mode cycle)
	if ipcServer != nil {
		ipcServer.SetState(ipc.StateWaiting)
		if result != nil {
			ipcServer.UpdateProgress(&ipc.ProgressResponse{
				TotalFiles:     result.ProcessedFiles,
				ProcessedFiles: result.ProcessedFiles,
				SuccessCount:   result.SuccessCount,
				FailedCount:    result.FailureCount,
				Percentage:     100.0,
			})
		}
	}

	if result != nil {
		printResults(result, dryRun)
	}

	return result, nil
}

// createVectorizerService creates a vectorizer service with concrete implementations
func createVectorizerService(cfg *types.Config) (*vectorizer.VectorizerService, error) {
	return createVectorizerServiceWithCSVConfig(cfg, nil)
}

// createVectorizerServiceWithCSVConfig creates a vectorizer service with CSV configuration
func createVectorizerServiceWithCSVConfig(cfg *types.Config, csvCfg *csv.Config) (*vectorizer.VectorizerService, error) {
	// Load AWS configuration with fixed region
	awsConfig, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion("us-east-1"))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS configuration: %w", err)
	}

	// Create embedding client using Bedrock
	embeddingClient := bedrock.NewBedrockClient(awsConfig, "amazon.titan-embed-text-v2:0")

	// Create S3 Vectors client
	var s3Client vectorizer.S3VectorClient
	s3Config := &s3vector.S3Config{
		VectorBucketName: cfg.AWSS3VectorBucket,
		IndexName:        cfg.AWSS3VectorIndex,
		Region:           resolveS3VectorRegion(cfg),
		MaxRetries:       cfg.RetryAttempts,
		RetryDelay:       cfg.RetryDelay,
	}
	s3ClientImpl, err := s3vector.NewS3VectorService(s3Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 client: %w", err)
	}
	s3Client = s3ClientImpl
	log.Println("S3 Vector client initialized")

	// Create metadata extractor and file scanner
	metadataExtractor := metadata.NewMetadataExtractor()
	fileScanner := scanner.NewFileScanner()

	// Create service factory for dependency injection
	serviceFactory := vectorizer.NewServiceFactory(cfg)

	// OpenSearch is always enabled
	enableOpenSearch := true
	indexName := openSearchIndexName
	if indexName == "" {
		// Should not happen as validateOpenSearchFlags ensures it's set
		return nil, fmt.Errorf("OpenSearch index name is not set")
	}

	// Create vectorizer service with all dependencies including CSV config
	return serviceFactory.CreateVectorizerServiceWithCSVConfig(
		embeddingClient,
		s3Client,
		metadataExtractor,
		fileScanner,
		enableOpenSearch,
		indexName,
		csvCfg,
	)
}

// printResults prints the processing results in a user-friendly format
func printResults(result *types.ProcessingResult, dryRun bool) {
	fmt.Println("\n" + strings.Repeat("=", 60))
	if dryRun {
		fmt.Println("DRY RUN RESULTS")
	} else {
		fmt.Println("VECTORIZATION RESULTS")
	}
	fmt.Println(strings.Repeat("=", 60))

	fmt.Printf("Processing Time:     %v\n", result.Duration)
	fmt.Printf("Files Processed:     %d\n", result.ProcessedFiles)

	// Show backend-specific statistics
	if result.OpenSearchEnabled {
		fmt.Println("\nBackend Statistics:")
		fmt.Println(strings.Repeat("-", 30))
		fmt.Printf("S3 Vector Successful:      %d\n", result.SuccessCount)
		fmt.Printf("S3 Vector Failed:          %d\n", result.FailureCount)
		fmt.Printf("OpenSearch Successful:     %d\n", result.OpenSearchSuccessCount)
		fmt.Printf("OpenSearch Failed:         %d\n", result.OpenSearchFailureCount)
		fmt.Printf("OpenSearch Indexed:        %d\n", result.OpenSearchIndexedCount)
		fmt.Printf("OpenSearch Skipped:        %d\n", result.OpenSearchSkippedCount)
		if result.OpenSearchRetryCount > 0 {
			fmt.Printf("OpenSearch Retries:        %d\n", result.OpenSearchRetryCount)
		}
		if result.OpenSearchProcessingTime > 0 {
			fmt.Printf("OpenSearch Processing:     %v\n", result.OpenSearchProcessingTime)
		}

		// Overall success rate calculation for dual backend
		totalOperations := result.ProcessedFiles * 2 // Each file processed by both backends
		totalSuccesses := result.SuccessCount + result.OpenSearchSuccessCount
		if totalOperations > 0 {
			fmt.Printf("\nOverall Success Rate:      %.1f%% (both backends)\n",
				float64(totalSuccesses)/float64(totalOperations)*100)
		}
	} else {
		fmt.Printf("S3 Vector Successful:      %d\n", result.SuccessCount)
		fmt.Printf("S3 Vector Failed:          %d\n", result.FailureCount)

		if result.FailureCount > 0 {
			fmt.Printf("Success Rate:              %.1f%%\n",
				float64(result.SuccessCount)/float64(result.ProcessedFiles)*100)
		} else {
			fmt.Printf("Success Rate:              100.0%%\n")
		}
	}

	// Show errors if any
	if len(result.Errors) > 0 {
		fmt.Println("\nErrors encountered:")
		fmt.Println(strings.Repeat("-", 40))

		errorCounts := make(map[types.ErrorType]int)
		for _, err := range result.Errors {
			errorCounts[err.Type]++
		}

		for errorType, count := range errorCounts {
			fmt.Printf("  %s: %d error(s)\n", errorType, count)
		}

		if len(result.Errors) <= 5 {
			fmt.Println("\nDetailed errors:")
			for i, err := range result.Errors {
				fmt.Printf("  %d. %s\n", i+1, err.Error())
			}
		} else {
			fmt.Println("\nFirst 5 errors:")
			for i := 0; i < 5; i++ {
				fmt.Printf("  %d. %s\n", i+1, result.Errors[i].Error())
			}
			fmt.Printf("  ... and %d more errors\n", len(result.Errors)-5)
		}
	}

	fmt.Println(strings.Repeat("=", 60))

	if dryRun {
		fmt.Println("\nThis was a dry run. No actual processing was performed.")
		if result.OpenSearchEnabled {
			fmt.Println("To perform actual vectorization to both S3 Vector and OpenSearch, run without --dry-run flag.")
		} else {
			fmt.Println("To perform actual vectorization, run without --dry-run flag.")
		}
	} else {
		// Success messages
		if result.OpenSearchEnabled {
			s3Success := result.SuccessCount > 0
			osSuccess := result.OpenSearchSuccessCount > 0

			if s3Success && osSuccess {
				fmt.Printf("\n✅ Successfully processed %d files to both S3 Vector and OpenSearch!\n", result.ProcessedFiles)
				fmt.Println("Vectors stored in S3 and documents indexed in OpenSearch are ready for hybrid search.")
			} else if s3Success && !osSuccess {
				fmt.Printf("\n⚠️  Successfully processed %d files to S3 Vector, but OpenSearch indexing failed.\n", result.SuccessCount)
				fmt.Println("S3 vectors are ready for use. Check OpenSearch errors above.")
			} else if !s3Success && osSuccess {
				fmt.Printf("\n⚠️  Successfully indexed %d files to OpenSearch, but S3 Vector processing failed.\n", result.OpenSearchSuccessCount)
				fmt.Println("OpenSearch index is ready for use. Check S3 Vector errors above.")
			} else {
				fmt.Println("\n❌ Processing failed for both S3 Vector and OpenSearch.")
				fmt.Println("Check errors above for troubleshooting.")
			}
		} else if result.SuccessCount > 0 {
			fmt.Printf("\n✅ Successfully vectorized %d files!\n", result.SuccessCount)
			fmt.Println("Vectors have been stored in S3 and are ready for use.")
		}
	}

	// Failure summary
	if result.OpenSearchEnabled {
		totalFailures := result.FailureCount + result.OpenSearchFailureCount
		if totalFailures > 0 {
			fmt.Printf("\n⚠️  %d total processing failures across both backends. Check errors above.\n", totalFailures)
		}
	} else if result.FailureCount > 0 {
		fmt.Printf("\n⚠️  %d files failed to process. Check errors above.\n", result.FailureCount)
	}
}

// confirmDeletePrompt asks user for confirmation before deleting vectors and/or OpenSearch index
func confirmDeletePrompt(indexName string) bool {
	if indexName != "" {
		fmt.Printf("⚠️  This will delete ALL existing vectors in the S3 Vector index AND delete/recreate the OpenSearch index '%s' (empty state). Continue? (y/N): ", indexName)
	} else {
		fmt.Print("⚠️  This will delete ALL existing vectors in the S3 Vector index. Continue? (y/N): ")
	}

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	input = strings.TrimSpace(strings.ToLower(input))
	return input == "y" || input == "yes"
}

// validateFollowModeFlags ensures follow mode related flags are used with valid combinations.
func validateFollowModeFlags(cmd *cobra.Command) error {
	if !followMode {
		flag := cmd.Flags().Lookup("interval")
		if flag != nil && flag.Changed {
			return fmt.Errorf("--interval flag requires --follow")
		}
		followIntervalDuration = 0
		return nil
	}

	if dryRun {
		return fmt.Errorf("--follow cannot be used with --dry-run")
	}

	if clearVectors {
		return fmt.Errorf("--follow cannot be used with --clear")
	}

	intervalValue := followInterval
	if intervalValue == "" {
		intervalValue = defaultFollowInterval
	}

	duration, err := time.ParseDuration(intervalValue)
	if err != nil {
		return fmt.Errorf("invalid interval: %w", err)
	}

	if duration < minFollowInterval {
		return fmt.Errorf("interval must be at least %s, got: %s", minFollowInterval, duration)
	}

	followIntervalDuration = duration
	return nil
}

// validateOpenSearchFlags validates OpenSearch related requirements
func validateOpenSearchFlags() error {
	// Validate OPENSEARCH_ENDPOINT (always required)
	endpoint := os.Getenv("OPENSEARCH_ENDPOINT")
	if endpoint == "" {
		return fmt.Errorf("OPENSEARCH_ENDPOINT environment variable is required")
	}
	log.Printf("OpenSearch endpoint configured: %s", endpoint)

	// Validate OPENSEARCH_INDEX is set (no default value)
	indexName := os.Getenv("OPENSEARCH_INDEX")
	if indexName == "" {
		return fmt.Errorf("OPENSEARCH_INDEX environment variable is required")
	}
	openSearchIndexName = indexName
	log.Printf("OpenSearch index name from environment: %s", openSearchIndexName)

	// Log final configuration
	log.Println("Dual backend mode: Both S3 Vector and OpenSearch will be used")
	log.Printf("Target OpenSearch index: %s", openSearchIndexName)

	return nil
}

// runSpreadsheetVectorize handles spreadsheet mode vectorization
func runSpreadsheetVectorize(ctx context.Context, cfg *types.Config) error {
	log.Printf("Loading spreadsheet configuration: %s", spreadsheetConfigPath)

	// Load spreadsheet configuration
	spreadsheetCfg, err := spreadsheet.LoadConfig(spreadsheetConfigPath)
	if err != nil {
		return fmt.Errorf("failed to load spreadsheet config: %w", err)
	}

	log.Println("Authenticating with GCP...")
	fetcher, err := spreadsheet.NewFetcher(ctx, spreadsheetCfg)
	if err != nil {
		return fmt.Errorf("failed to create spreadsheet fetcher: %w", err)
	}

	// Validate connection
	log.Println("Validating spreadsheet access...")
	if err := fetcher.ValidateConnection(ctx); err != nil {
		return fmt.Errorf("spreadsheet validation failed: %w", err)
	}
	log.Println("✓ Authentication successful")

	// Dry-run mode
	if dryRun {
		return runSpreadsheetDryRun(ctx, fetcher)
	}

	// Fetch data from spreadsheets
	log.Println("Fetching data from spreadsheets...")
	files, err := fetcher.Fetch(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch spreadsheet data: %w", err)
	}

	if len(files) == 0 {
		log.Println("No data rows found in spreadsheets")
		return nil
	}

	log.Printf("Found %d rows to process", len(files))

	// Create vectorizer service
	service, err := createVectorizerServiceForSpreadsheet(cfg)
	if err != nil {
		return fmt.Errorf("failed to create vectorizer service: %w", err)
	}

	// Validate configuration
	log.Println("Validating configuration and service connections...")
	validationCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := service.ValidateConfiguration(validationCtx); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}
	log.Println("Configuration validation successful")

	// Process files using the vectorizer service
	result, err := service.VectorizeFiles(ctx, files, false)
	if err != nil {
		return fmt.Errorf("vectorization failed: %w", err)
	}

	printResults(result, false)
	return nil
}

// runSpreadsheetDryRun shows preview of what would be processed
func runSpreadsheetDryRun(ctx context.Context, fetcher *spreadsheet.Fetcher) error {
	log.Println("DRY RUN: Fetching spreadsheet data for preview...")

	infos, err := fetcher.FetchWithDryRun(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch spreadsheet data: %w", err)
	}

	if len(infos) == 0 {
		log.Println("No data rows found in spreadsheets")
		return nil
	}

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("DRY RUN: Spreadsheet Processing Preview")
	fmt.Println(strings.Repeat("=", 60))

	for i, info := range infos {
		if i >= 10 {
			fmt.Printf("\n... and %d more rows\n", len(infos)-10)
			break
		}

		fmt.Printf("\n[Row %d]\n", info.RowIndex)
		fmt.Printf("  ID: %s\n", info.ID)
		fmt.Printf("  Title: %s\n", truncateString(info.Title, 60))
		if info.Category != "" {
			fmt.Printf("  Category: %s\n", info.Category)
		}
		fmt.Printf("  Content Preview: %s\n", truncateString(info.ContentPreview, 80))
		fmt.Printf("  Content Length: %d characters\n", info.ContentLength)
		fmt.Printf("  Content Column(s): %v\n", info.ContentColumns)
	}

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("Summary:")
	fmt.Printf("  Total rows: %d\n", len(infos))
	fmt.Printf("  Would generate: %d embeddings\n", len(infos))
	fmt.Printf("  Estimated processing time: ~%d minutes\n", estimateProcessingTime(len(infos)))
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("\nThis was a dry run. No embeddings were created.")
	fmt.Println("To process, run without --dry-run flag.")

	return nil
}

// createVectorizerServiceForSpreadsheet creates a vectorizer service for spreadsheet processing
func createVectorizerServiceForSpreadsheet(cfg *types.Config) (*vectorizer.VectorizerService, error) {
	// Load AWS configuration with fixed region
	awsConfig, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion("us-east-1"))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS configuration: %w", err)
	}

	// Create embedding client using Bedrock
	embeddingClient := bedrock.NewBedrockClient(awsConfig, "amazon.titan-embed-text-v2:0")

	// Create S3 Vectors client
	s3Config := &s3vector.S3Config{
		VectorBucketName: cfg.AWSS3VectorBucket,
		IndexName:        cfg.AWSS3VectorIndex,
		Region:           resolveS3VectorRegion(cfg),
		MaxRetries:       cfg.RetryAttempts,
		RetryDelay:       cfg.RetryDelay,
	}
	s3Client, err := s3vector.NewS3VectorService(s3Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 client: %w", err)
	}
	log.Println("S3 Vector client initialized")

	// Create metadata extractor (reuse from existing)
	metadataExtractor := metadata.NewMetadataExtractor()

	// Create a no-op file scanner since we're using spreadsheet data
	fileScanner := scanner.NewFileScanner()

	// Create service factory for dependency injection
	serviceFactory := vectorizer.NewServiceFactory(cfg)

	// OpenSearch is always enabled
	enableOpenSearch := true
	indexName := openSearchIndexName

	// Create vectorizer service with all dependencies
	return serviceFactory.CreateVectorizerServiceWithDefaults(
		embeddingClient,
		s3Client,
		metadataExtractor,
		fileScanner,
		enableOpenSearch,
		indexName,
	)
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// estimateProcessingTime estimates processing time in minutes based on row count
func estimateProcessingTime(rowCount int) int {
	// Rough estimate: ~2 seconds per row (embedding + storage)
	seconds := rowCount * 2
	minutes := seconds / 60
	if minutes < 1 {
		return 1
	}
	return minutes
}

// printCSVConfigInfo prints CSV configuration details for dry-run mode
func printCSVConfigInfo(files []*types.FileInfo, csvCfg *csv.Config) {
	// Filter CSV files
	var csvFiles []*types.FileInfo
	for _, f := range files {
		if f.IsCSV {
			csvFiles = append(csvFiles, f)
		}
	}

	if len(csvFiles) == 0 {
		return
	}

	// Create reader to get detected columns
	reader := csv.NewReader(csvCfg)

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("CSV CONFIGURATION PREVIEW")
	fmt.Println(strings.Repeat("=", 60))

	// Show loaded configuration patterns
	if csvCfg != nil {
		fmt.Println("\nLoaded Patterns from csv-config:")
		for i, fc := range csvCfg.CSV.Files {
			fmt.Printf("  [%d] %s (header_row: %d)\n", i+1, fc.Pattern, fc.GetHeaderRow())
		}
	} else {
		fmt.Println("\nUsing default configuration (*.csv)")
	}

	for _, f := range csvFiles {
		fmt.Printf("\nFile: %s\n", f.Name)
		fmt.Println(strings.Repeat("-", 30))

		// Use content-based detection for S3 files (Content is already downloaded)
		// Use file-based detection for local files
		var info *csv.DetectedColumnsInfo
		var err error
		if f.Content != "" {
			info, err = reader.GetDetectedColumnsFromContent(f.Path, f.Content)
		} else {
			info, err = reader.GetDetectedColumns(f.Path)
		}
		if err != nil {
			fmt.Printf("  Error: %v\n", err)
			continue
		}

		// Show matched pattern
		fmt.Printf("  Matched Pattern:  %s\n", info.MatchedPattern)

		// Show header row position
		fmt.Printf("  Header Row:       %d\n", info.HeaderRow)

		// Show headers (truncate if too many)
		if len(info.Headers) > 0 {
			headerDisplay := formatHeadersDisplay(info.Headers, 60)
			fmt.Printf("  Headers:          %s\n", headerDisplay)
		}

		// Show content columns with source indication
		if len(info.ContentColumns) > 0 {
			source := "from config"
			if info.IsAutoDetected {
				source = "auto-detected"
			}
			fmt.Printf("  Content Columns:  %v (%s)\n", info.ContentColumns, source)
		}

		// Show template if configured
		if csvCfg != nil {
			fileConfig := csvCfg.GetConfigForFile(f.Path)
			if fileConfig != nil && fileConfig.Content.Template != "" {
				templatePreview := truncateString(fileConfig.Content.Template, 50)
				fmt.Printf("  Template:         %s\n", templatePreview)

				// Show metadata mappings
				fmt.Println("\n  Metadata Mappings:")
				if fileConfig.Metadata.Title != "" {
					fmt.Printf("    Title:      %s\n", fileConfig.Metadata.Title)
				}
				if fileConfig.Metadata.Category != "" {
					fmt.Printf("    Category:   %s\n", fileConfig.Metadata.Category)
				}
				if fileConfig.Metadata.ID != "" {
					fmt.Printf("    ID:         %s\n", fileConfig.Metadata.ID)
				}
				if len(fileConfig.Metadata.Tags) > 0 {
					fmt.Printf("    Tags:       %v\n", fileConfig.Metadata.Tags)
				}
				if fileConfig.Metadata.CreatedAt != "" {
					fmt.Printf("    CreatedAt:  %s\n", fileConfig.Metadata.CreatedAt)
				}
				if fileConfig.Metadata.UpdatedAt != "" {
					fmt.Printf("    UpdatedAt:  %s\n", fileConfig.Metadata.UpdatedAt)
				}
				if fileConfig.Metadata.Reference != "" {
					fmt.Printf("    Reference:  %s\n", fileConfig.Metadata.Reference)
				}

				// Show auto-detect status
				autoDetect := "enabled"
				if !fileConfig.Content.IsAutoDetectEnabled() {
					autoDetect = "disabled"
				}
				fmt.Printf("\n  Auto Detection:   %s\n", autoDetect)
			}
		} else {
			// Show auto-detected metadata columns
			fmt.Println("\n  Detected Metadata (auto):")
			if info.TitleColumn != "" {
				fmt.Printf("    Title:      %s\n", info.TitleColumn)
			}
			if info.CategoryColumn != "" {
				fmt.Printf("    Category:   %s\n", info.CategoryColumn)
			}
			if info.IDColumn != "" {
				fmt.Printf("    ID:         %s\n", info.IDColumn)
			}
			fmt.Printf("\n  Auto Detection:   enabled (no config file)\n")
		}

		// Show data rows count
		fmt.Printf("  Data Rows:        %d\n", info.TotalRows)
	}

	fmt.Println("\n" + strings.Repeat("=", 60))
}

// formatHeadersDisplay formats headers for display, truncating if necessary
func formatHeadersDisplay(headers []string, maxLen int) string {
	if len(headers) == 0 {
		return "[]"
	}

	// Build header string
	result := "["
	currentLen := 1 // Starting bracket

	for i, h := range headers {
		addition := h
		if i > 0 {
			addition = ", " + h
		}

		// Check if adding this would exceed max length
		if currentLen+len(addition)+4 > maxLen && i < len(headers)-1 {
			remaining := len(headers) - i
			result += fmt.Sprintf(", ... +%d more]", remaining)
			return result
		}

		result += addition
		currentLen += len(addition)
	}

	result += "]"
	return result
}

// resolveS3VectorRegion returns the S3 Vector region with priority: flag > env > default
func resolveS3VectorRegion(cfg *types.Config) string {
	if s3VectorRegion != "" {
		return s3VectorRegion
	}
	return cfg.S3VectorRegion
}

// resolveS3SourceRegion returns the S3 Source region with priority: flag > env > default
func resolveS3SourceRegion(cfg *types.Config) string {
	if s3SourceRegion != "" {
		return s3SourceRegion
	}
	return cfg.S3SourceRegion
}
