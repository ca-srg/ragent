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
	"github.com/ca-srg/ragent/internal/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/metadata"
	"github.com/ca-srg/ragent/internal/s3vector"
	"github.com/ca-srg/ragent/internal/scanner"
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
)

var vectorizationRunner = executeVectorizationOnce

var vectorizeCmd = &cobra.Command{
	Use:   "vectorize",
	Short: "Convert markdown files to vectors and store in S3",
	Long: `
The vectorize command processes markdown files in a directory,
extracts metadata, generates embeddings using Amazon Bedrock,
and stores the vectors in Amazon S3.

This enables the creation of a vector database from your markdown
documentation for RAG (Retrieval Augmented Generation) applications.
`,
	RunE: runVectorize,
}

func init() {
	vectorizeCmd.Flags().StringVarP(&directory, "directory", "d", "./markdown", "Directory containing markdown files to process")
	vectorizeCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be processed without making API calls")
	vectorizeCmd.Flags().IntVarP(&concurrency, "concurrency", "c", 0, "Number of concurrent operations (0 = use config default)")
	vectorizeCmd.Flags().BoolVar(&clearVectors, "clear", false, "Delete all existing vectors before processing new ones")
	vectorizeCmd.Flags().BoolVarP(&followMode, "follow", "f", false, "Continuously vectorize at a fixed interval")
	vectorizeCmd.Flags().StringVar(&followInterval, "interval", defaultFollowInterval, "Interval between vectorization runs in follow mode (e.g. 30m, 1h)")
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
	if ctx == nil {
		ctx = context.Background()
	}

	if _, err := os.Stat(directory); os.IsNotExist(err) {
		return nil, fmt.Errorf("directory does not exist: %s", directory)
	}

	if clearVectors && !dryRun {
		if !confirmDeletePrompt(openSearchIndexName) {
			fmt.Println("Operation cancelled by user.")
			return nil, nil
		}

		s3Config := &s3vector.S3Config{
			VectorBucketName: cfg.AWSS3VectorBucket,
			IndexName:        cfg.AWSS3VectorIndex,
			Region:           cfg.AWSS3Region,
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

	service, err := createVectorizerService(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create vectorizer service: %w", err)
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

	result, err := service.VectorizeMarkdownFiles(ctx, directory, dryRun)
	if err != nil {
		return nil, fmt.Errorf("vectorization failed: %w", err)
	}

	return result, nil
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

func runFollowCycle(ctx context.Context, cfg *types.Config) (*types.ProcessingResult, error) {
	if !startFollowProcessing() {
		log.Println("[Follow Mode] Previous vectorization still running. Skipping this cycle.")
		return nil, nil
	}

	defer finishFollowProcessing()

	log.Println("[Follow Mode] Starting vectorization cycle...")

	result, err := vectorizationRunner(ctx, cfg)
	if err != nil {
		return nil, err
	}

	if result != nil {
		printResults(result, dryRun)
	}

	return result, nil
}

func runFollowMode(ctx context.Context, cfg *types.Config) error {
	followCtx, cancel := setupSignalHandler(ctx)
	defer cancel()

	interval := followIntervalDuration
	if interval <= 0 {
		interval = 30 * time.Minute
	}

	log.Printf("Follow mode enabled. Interval: %s. Press Ctrl+C to stop.", interval)

	result, err := runFollowCycle(followCtx, cfg)
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
		case <-ticker.C:
			result, err := runFollowCycle(followCtx, cfg)
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

// createVectorizerService creates a vectorizer service with concrete implementations
func createVectorizerService(cfg *types.Config) (*vectorizer.VectorizerService, error) {
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
		Region:           cfg.AWSS3Region,
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
