package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/spf13/cobra"

	appconfig "github.com/ca-srg/kiberag/internal/config"
	"github.com/ca-srg/kiberag/internal/embedding/bedrock"
	"github.com/ca-srg/kiberag/internal/filter"
	"github.com/ca-srg/kiberag/internal/s3vector"
	commontypes "github.com/ca-srg/kiberag/internal/types"
)

var (
	queryText   string
	topK        int
	outputJSON  bool
	filterQuery string
)

var queryCmd = &cobra.Command{
	Use:   "query",
	Short: "Search vectors in S3 Vector Index using semantic similarity",
	Long: `
Search vectors stored in the S3 Vector Index using semantic similarity.
Provide a text query and this command will convert it to an embedding
and find the most similar vectors in your index.

You can also apply metadata filters to narrow down results.

Examples:
  kiberag query -q "machine learning algorithms"
  kiberag query -q "API documentation" --top-k 5
  kiberag query -q "error handling" --filter '{"category":"programming"}'
`,
	RunE: runQuery,
}

func init() {
	queryCmd.Flags().StringVarP(&queryText, "query", "q", "", "Text query to search for (required)")
	queryCmd.Flags().IntVarP(&topK, "top-k", "k", 10, "Number of similar results to return")
	queryCmd.Flags().BoolVarP(&outputJSON, "json", "j", false, "Output results in JSON format")
	queryCmd.Flags().StringVarP(&filterQuery, "filter", "f", "", "JSON metadata filter (e.g., '{\"category\":\"docs\"}')")

	queryCmd.MarkFlagRequired("query")
}

func runQuery(cmd *cobra.Command, args []string) error {
	log.Printf("Searching for: %s", queryText)

	// Load configuration
	cfg, err := appconfig.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Load AWS configuration with fixed region
	awsConfig, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion("us-east-1"))
	if err != nil {
		return fmt.Errorf("failed to load AWS configuration: %w", err)
	}

	// Create embedding client using Bedrock
	embeddingClient := bedrock.NewBedrockClient(awsConfig, "amazon.titan-embed-text-v2:0")

	// Create S3 Vector service
	s3Config := &s3vector.S3Config{
		VectorBucketName: cfg.AWSS3VectorBucket,
		IndexName:        cfg.AWSS3VectorIndex,
		Region:           cfg.AWSS3Region,
	}

	s3Service, err := s3vector.NewS3VectorService(s3Config)
	if err != nil {
		return fmt.Errorf("failed to create S3 Vector service: %w", err)
	}

	// Validate connections
	log.Println("Validating service connections...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := embeddingClient.ValidateConnection(ctx); err != nil {
		return fmt.Errorf("embedding service validation failed: %w", err)
	}

	if err := s3Service.ValidateAccess(ctx); err != nil {
		return fmt.Errorf("S3 Vector access validation failed: %w", err)
	}

	// Generate embedding for query text
	log.Println("Generating embedding for query...")
	queryEmbedding, err := embeddingClient.GenerateEmbedding(ctx, queryText)
	if err != nil {
		return fmt.Errorf("failed to generate embedding: %w", err)
	}

	// Build filter with exclusions and user filter
	filter, err := filter.BuildExclusionFilterFromJSON(cfg, filterQuery)
	if err != nil {
		return fmt.Errorf("failed to build filter: %w", err)
	}

	// Log filter for debugging
	if len(cfg.ExcludeCategories) > 0 {
		log.Printf("Excluding categories: %v", cfg.ExcludeCategories)
	}

	// Execute vector query
	log.Printf("Searching for %d similar vectors...", topK)
	result, err := s3Service.QueryVectors(ctx, queryEmbedding, topK, filter)
	if err != nil {
		return fmt.Errorf("failed to query vectors: %w", err)
	}

	// Output results
	if outputJSON {
		result.Query = queryText
		jsonOutput, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON output: %w", err)
		}
		fmt.Println(string(jsonOutput))
	} else {
		printQueryResults(result, queryText, cfg)
	}

	return nil
}

func printQueryResults(result *commontypes.QueryVectorsResult, query string, cfg *commontypes.Config) {
	fmt.Printf("\nQuery: %s\n", query)
	fmt.Printf("Found %d similar vectors (Top-%d results):\n", result.TotalCount, result.TopK)
	fmt.Printf("Bucket: %s\n", cfg.AWSS3VectorBucket)
	fmt.Printf("Index: %s\n", cfg.AWSS3VectorIndex)
	fmt.Println("\nResults:")

	if len(result.Results) == 0 {
		fmt.Println("  (no similar vectors found)")
		return
	}

	for i, res := range result.Results {
		fmt.Printf("\n  %d. Vector: %s\n", i+1, res.Key)
		fmt.Printf("     Distance: %.4f\n", res.Distance)

		if res.Metadata != nil {
			if title, ok := res.Metadata["title"].(string); ok && title != "" {
				fmt.Printf("     Title: %s\n", title)
			}
			if category, ok := res.Metadata["category"].(string); ok && category != "" {
				fmt.Printf("     Category: %s\n", category)
			}
			if filePath, ok := res.Metadata["file_path"].(string); ok && filePath != "" {
				fmt.Printf("     File: %s\n", filePath)
			}
			if createdAt, ok := res.Metadata["created_at"].(string); ok && createdAt != "" {
				fmt.Printf("     Created: %s\n", createdAt)
			}
		}
	}
}
