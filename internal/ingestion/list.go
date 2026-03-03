package ingestion

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ca-srg/ragent/internal/pkg/config"
	"github.com/ca-srg/ragent/internal/pkg/s3vector"
)

// RunList lists all vectors stored in the S3 Vector Index with optional prefix filtering.
func RunList(prefix string) error {
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
		Region:           cfg.S3VectorRegion,
		MaxRetries:       cfg.RetryAttempts,
		RetryDelay:       cfg.RetryDelay,
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
	fmt.Printf("Region: %s\n", cfg.S3VectorRegion)

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
