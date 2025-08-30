package cmd

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/spf13/cobra"

	appconfig "github.com/ca-srg/mdrag/internal/config"
	"github.com/ca-srg/mdrag/internal/embedding/bedrock"
	"github.com/ca-srg/mdrag/internal/metadata"
	"github.com/ca-srg/mdrag/internal/s3vector"
	"github.com/ca-srg/mdrag/internal/scanner"
	"github.com/ca-srg/mdrag/internal/types"
	"github.com/ca-srg/mdrag/internal/vectorizer"
)

var (
	directory           string
	dryRun              bool
	concurrency         int
	clearVectors        bool
	openSearchIndexName string
)

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
}

func runVectorize(cmd *cobra.Command, args []string) error {
	log.Println("Starting vectorization process...")

	// Validate OpenSearch flags
	if err := validateOpenSearchFlags(); err != nil {
		return fmt.Errorf("flag validation failed: %w", err)
	}

	// Load configuration
	cfg, err := appconfig.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Override concurrency if specified via flag
	if concurrency > 0 {
		cfg.Concurrency = concurrency
	}

	// Validate directory exists
	if _, err := os.Stat(directory); os.IsNotExist(err) {
		return fmt.Errorf("directory does not exist: %s", directory)
	}

	// Handle --clear flag to delete existing vectors and/or OpenSearch index
	if clearVectors && !dryRun {
		if !confirmDeletePrompt(openSearchIndexName) {
			fmt.Println("Operation cancelled by user.")
			return nil
		}

		// Delete from S3 Vector
		{
			// Create S3 Vector client for deletion
			s3Config := &s3vector.S3Config{
				VectorBucketName: cfg.AWSS3VectorBucket,
				IndexName:        cfg.AWSS3VectorIndex,
				Region:           cfg.AWSS3Region,
			}
			s3Client, err := s3vector.NewS3VectorService(s3Config)
			if err != nil {
				return fmt.Errorf("failed to create S3 client for deletion: %w", err)
			}

			log.Printf("S3 Vector index name: %s", cfg.AWSS3VectorIndex)
			log.Printf("S3 Vector bucket name: %s", cfg.AWSS3VectorBucket)
			log.Println("Deleting all existing vectors from S3 Vector index...")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			deletedCount, err := s3Client.DeleteAllVectors(ctx)
			if err != nil {
				return fmt.Errorf("failed to delete existing vectors: %w", err)
			}

			log.Printf("Successfully deleted %d vectors from S3 Vector index (%s) in bucket (%s)", deletedCount, cfg.AWSS3VectorIndex, cfg.AWSS3VectorBucket)
		}

		// Delete OpenSearch index
		{
			log.Printf("Deleting OpenSearch index: %s", openSearchIndexName)

			// Create a temporary service to get OpenSearch indexer
			tempService, err := createVectorizerService(cfg)
			if err != nil {
				return fmt.Errorf("failed to create service for OpenSearch deletion: %w", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			// Use the service's OpenSearch indexer to delete the index
			if tempService != nil {
				// We need to access the indexer through the service
				// For now, we'll create the indexer directly
				serviceFactory := vectorizer.NewServiceFactory(cfg)
				if serviceFactory != nil {
					indexerFactory := vectorizer.NewIndexerFactory(cfg)
					if indexerFactory.IsOpenSearchEnabled() {
						indexer, err := indexerFactory.CreateOpenSearchIndexer(openSearchIndexName, 1024)
						if err != nil {
							log.Printf("Warning: Failed to create OpenSearch indexer for deletion: %v", err)
						} else {
							err = indexer.DeleteIndex(ctx, openSearchIndexName)
							if err != nil {
								log.Printf("Warning: Failed to delete OpenSearch index: %v", err)
							} else {
								log.Printf("Successfully deleted OpenSearch index: %s", openSearchIndexName)

								// Recreate the empty index
								log.Printf("Recreating OpenSearch index: %s", openSearchIndexName)
								err = indexer.CreateIndex(ctx, openSearchIndexName, 1024)
								if err != nil {
									log.Printf("Warning: Failed to recreate OpenSearch index: %v", err)
								} else {
									log.Printf("Successfully recreated OpenSearch index: %s", openSearchIndexName)
								}
							}
						}
					}
				}
			}
		}
		return nil // Exit after clearing vectors and recreating OpenSearch index, don't proceed with vectorization
	} else if clearVectors && dryRun {
		log.Printf("DRY RUN: Would delete and recreate OpenSearch index: %s", openSearchIndexName)
		log.Println("DRY RUN: Would delete all existing vectors from S3 Vector index")
		return nil // Exit after dry-run clear, don't proceed with vectorization
	}

	// Create service with concrete implementations
	service, err := createVectorizerService(cfg)
	if err != nil {
		return fmt.Errorf("failed to create vectorizer service: %w", err)
	}

	// Validate configuration and connections (skip in dry-run mode)
	if !dryRun {
		log.Println("Validating configuration and service connections...")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := service.ValidateConfiguration(ctx); err != nil {
			return fmt.Errorf("configuration validation failed: %w", err)
		}
		log.Println("Configuration validation successful")
	}

	// Process markdown files
	ctx := context.Background()
	result, err := service.VectorizeMarkdownFiles(ctx, directory, dryRun)
	if err != nil {
		return fmt.Errorf("vectorization failed: %w", err)
	}

	// Print results
	printResults(result, dryRun)

	return nil
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
