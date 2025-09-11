package cmd

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ca-srg/mdrag/internal/config"
	"github.com/ca-srg/mdrag/internal/opensearch"
	"github.com/ca-srg/mdrag/internal/vectorizer"
	"github.com/spf13/cobra"
)

var recreateIndexCmd = &cobra.Command{
	Use:   "recreate-index",
	Short: "Recreate OpenSearch index with correct mapping",
	Long:  `Delete and recreate the OpenSearch index with the proper mapping for vector embeddings`,
	RunE:  runRecreateIndex,
}

func init() {
	rootCmd.AddCommand(recreateIndexCmd)
}

func runRecreateIndex(cmd *cobra.Command, args []string) error {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Create OpenSearch configuration
	osConfig := &opensearch.Config{
		Endpoint:          cfg.OpenSearchEndpoint,
		Region:            cfg.OpenSearchRegion,
		InsecureSkipTLS:   cfg.OpenSearchInsecureSkipTLS,
		RateLimit:         cfg.OpenSearchRateLimit,
		RateBurst:         cfg.OpenSearchRateBurst,
		ConnectionTimeout: cfg.OpenSearchConnectionTimeout,
		RequestTimeout:    cfg.OpenSearchRequestTimeout,
		MaxRetries:        cfg.OpenSearchMaxRetries,
		RetryDelay:        cfg.OpenSearchRetryDelay,
		MaxConnections:    cfg.OpenSearchMaxConnections,
		MaxIdleConns:      cfg.OpenSearchMaxIdleConns,
		IdleConnTimeout:   cfg.OpenSearchIdleConnTimeout,
	}

	// Create OpenSearch client
	osClient, err := opensearch.NewClient(osConfig)
	if err != nil {
		return fmt.Errorf("failed to create OpenSearch client: %w", err)
	}

	ctx := context.Background()
	indexName := cfg.OpenSearchIndex
	if indexName == "" {
		indexName = "kiberag-vectors"
	}

	// Create indexer
	indexer := vectorizer.NewOpenSearchIndexer(osClient, indexName, 1024)

	// Check if index exists
	exists, err := indexer.IndexExists(ctx, indexName)
	if err != nil {
		log.Printf("Warning: Could not check if index exists: %v", err)
	}

	// Delete existing index if it exists
	if exists {
		log.Printf("Deleting existing index: %s", indexName)
		if err := indexer.DeleteIndex(ctx, indexName); err != nil {
			log.Printf("Warning: Could not delete index: %v", err)
		} else {
			log.Printf("Successfully deleted index: %s", indexName)
		}

		// Wait a bit for the deletion to propagate
		time.Sleep(2 * time.Second)
	}

	// Create new index with correct mapping
	log.Printf("Creating new index with proper mapping: %s", indexName)
	if err := indexer.CreateIndex(ctx, indexName, 1024); err != nil {
		return fmt.Errorf("failed to create index: %w", err)
	}

	log.Printf("Successfully created index: %s with 1024-dimensional embedding field", indexName)

	// Verify the index was created
	exists, err = indexer.IndexExists(ctx, indexName)
	if err != nil {
		return fmt.Errorf("failed to verify index creation: %w", err)
	}

	if !exists {
		return fmt.Errorf("index was not created successfully")
	}

	log.Printf("✓ Index %s has been recreated with the correct mapping", indexName)
	log.Printf("You can now run 'kiberag vectorize' to index your documents")

	return nil
}
