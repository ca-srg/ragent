package ingestion

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ca-srg/ragent/internal/ingestion/vectorizer"
	"github.com/ca-srg/ragent/internal/pkg/config"
)

// RunList lists all vectors stored in the vector store with optional prefix filtering.
func RunList(prefix string) error {
	log.Println("Listing vectors from vector store...")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	sf := vectorizer.NewServiceFactory(cfg)
	service, err := sf.CreateVectorStore()
	if err != nil {
		return fmt.Errorf("failed to create vector store service: %w", err)
	}

	// Validate access
	log.Println("Validating vector store access...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := service.ValidateAccess(ctx); err != nil {
		return fmt.Errorf("vector store access validation failed: %w", err)
	}

	// List vectors
	log.Printf("Fetching vectors with prefix: '%s'", prefix)
	vectors, err := service.ListVectors(ctx, prefix)
	if err != nil {
		return fmt.Errorf("failed to list vectors: %w", err)
	}

	// Display results
	fmt.Printf("\nFound %d vectors in vector store:\n", len(vectors))
	info, err := service.GetBackendInfo(ctx)
	if err == nil {
		for k, v := range info {
			fmt.Printf("%s: %v\n", k, v)
		}
	}

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
