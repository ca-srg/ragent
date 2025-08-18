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

	appconfig "github.com/ca-srg/kiberag/internal/config"
	"github.com/ca-srg/kiberag/internal/embedding/bedrock"
	"github.com/ca-srg/kiberag/internal/metadata"
	"github.com/ca-srg/kiberag/internal/s3vector"
	"github.com/ca-srg/kiberag/internal/scanner"
	"github.com/ca-srg/kiberag/internal/types"
	"github.com/ca-srg/kiberag/internal/vectorizer"
)

var (
	directory    string
	dryRun       bool
	concurrency  int
	clearVectors bool
)

var vectorizeCmd = &cobra.Command{
	Use:   "vectorize",
	Short: "Convert markdown files to vectors and store in S3",
	Long: `
The vectorize command processes markdown files in a directory,
extracts metadata, generates embeddings using Voyage AI,
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

	// Handle --clear flag to delete existing vectors
	if clearVectors && !dryRun {
		if !confirmDeletePrompt() {
			fmt.Println("Operation cancelled by user.")
			return nil
		}

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

		log.Println("Deleting all existing vectors from S3 Vector index...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		deletedCount, err := s3Client.DeleteAllVectors(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete existing vectors: %w", err)
		}

		log.Printf("Successfully deleted %d vectors from S3 Vector index", deletedCount)
	} else if clearVectors && dryRun {
		log.Println("DRY RUN: Would delete all existing vectors from S3 Vector index")
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
	s3Config := &s3vector.S3Config{
		VectorBucketName: cfg.AWSS3VectorBucket,
		IndexName:        cfg.AWSS3VectorIndex,
		Region:           cfg.AWSS3Region,
	}
	s3Client, err := s3vector.NewS3VectorService(s3Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 client: %w", err)
	}

	// Create metadata extractor and file scanner
	metadataExtractor := metadata.NewMetadataExtractor()
	fileScanner := scanner.NewFileScanner()

	// Create service configuration
	serviceConfig := &vectorizer.ServiceConfig{
		Config:            cfg,
		EmbeddingClient:   embeddingClient,
		S3Client:          s3Client,
		MetadataExtractor: metadataExtractor,
		FileScanner:       fileScanner,
	}

	return vectorizer.NewVectorizerService(serviceConfig)
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
	fmt.Printf("Successful:          %d\n", result.SuccessCount)
	fmt.Printf("Failed:              %d\n", result.FailureCount)

	if result.FailureCount > 0 {
		fmt.Printf("Success Rate:        %.1f%%\n",
			float64(result.SuccessCount)/float64(result.ProcessedFiles)*100)
	} else {
		fmt.Printf("Success Rate:        100.0%%\n")
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
		fmt.Println("To perform actual vectorization, run without --dry-run flag.")
	} else if result.SuccessCount > 0 {
		fmt.Printf("\n✅ Successfully vectorized %d files!\n", result.SuccessCount)
		fmt.Println("Vectors have been stored in S3 and are ready for use.")
	}

	if result.FailureCount > 0 {
		fmt.Printf("\n⚠️  %d files failed to process. Check errors above.\n", result.FailureCount)
	}
}

// confirmDeletePrompt asks user for confirmation before deleting all vectors
func confirmDeletePrompt() bool {
	fmt.Print("⚠️  This will delete ALL existing vectors in the S3 Vector index. Continue? (y/N): ")

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	input = strings.TrimSpace(strings.ToLower(input))
	return input == "y" || input == "yes"
}
