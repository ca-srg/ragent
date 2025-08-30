package cmd

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/spf13/cobra"

	"github.com/ca-srg/mdrag/internal/config"
	"github.com/ca-srg/mdrag/internal/s3vector"
)

var (
	prefix string
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List vectors stored in S3 Vector Index",
	Long: `
List all vectors stored in the specified S3 Vector Index.
You can optionally provide a prefix to filter the results.

This command shows vector keys that have been stored in your S3 Vector Index,
helping you understand what data is available for querying.
`,
	RunE: runList,
}

func init() {
	listCmd.Flags().StringVarP(&prefix, "prefix", "p", "", "Prefix to filter vector keys")
}

func runList(cmd *cobra.Command, args []string) error {
	log.Println("Listing vectors from S3 Vector Index...")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Create S3 Vector service
	s3Config := &s3vector.S3Config{
		VectorBucketName: cfg.AWSS3VectorBucket,
		IndexName:        cfg.AWSS3VectorIndex,
		Region:           cfg.AWSS3Region,
	}

	service, err := s3vector.NewS3VectorService(s3Config)
	if err != nil {
		return fmt.Errorf("failed to create S3 Vector service: %w", err)
	}

	// Validate access
	log.Println("Validating S3 Vector access...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := service.ValidateAccess(ctx); err != nil {
		return fmt.Errorf("S3 Vector access validation failed: %w", err)
	}

	// List vectors
	log.Printf("Fetching vectors with prefix: '%s'", prefix)
	vectors, err := service.ListVectors(ctx, prefix)
	if err != nil {
		return fmt.Errorf("failed to list vectors: %w", err)
	}

	// Display results
	fmt.Printf("\nFound %d vectors in S3 Vector Index:\n", len(vectors))
	fmt.Printf("Bucket: %s\n", cfg.AWSS3VectorBucket)
	fmt.Printf("Index: %s\n", cfg.AWSS3VectorIndex)
	fmt.Printf("Region: %s\n", cfg.AWSS3Region)

	if prefix != "" {
		fmt.Printf("Prefix filter: %s\n", prefix)
	}

	fmt.Println("\nVector Keys:")
	if len(vectors) == 0 {
		fmt.Println("  (no vectors found)")
	} else {
		for i, key := range vectors {
			fmt.Printf("  %d. %s\n", i+1, key)
		}
	}

	return nil
}
