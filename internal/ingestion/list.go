package ingestion

import (
	"context"
	"encoding/json"
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

	log.Printf("Fetching vectors with prefix: '%s'", prefix)
	items, err := service.ListVectorsWithMetadata(ctx, prefix)
	if err != nil {
		return fmt.Errorf("failed to list vectors: %w", err)
	}

	fmt.Printf("\nFound %d vectors in vector store:\n", len(items))
	info, err := service.GetBackendInfo(ctx)
	if err == nil {
		for k, v := range info {
			fmt.Printf("%s: %v\n", k, v)
		}
	}

	if prefix != "" {
		fmt.Printf("Prefix filter: %s\n", prefix)
	}

	fmt.Println("\nVectors:")
	if len(items) == 0 {
		fmt.Println("  (no vectors found)")
	} else {
		for i, item := range items {
			fmt.Printf("  %d. %s\n", i+1, item.Key)
			if item.RawMetadata != nil {
				raw, err := json.MarshalIndent(item.RawMetadata, "     ", "  ")
				if err == nil {
					fmt.Printf("     _source: %s\n", raw)
				}
			}
		}
	}

	return nil
}
