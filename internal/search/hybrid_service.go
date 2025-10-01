package search

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/ca-srg/ragent/internal/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/opensearch"
	"github.com/ca-srg/ragent/internal/types"
)

// HybridSearchService provides reusable hybrid search functionality
type HybridSearchService struct {
	config          *types.Config
	embeddingClient *bedrock.BedrockClient
	osClient        *opensearch.Client
	hybridEngine    *opensearch.HybridSearchEngine
	logger          *log.Logger
}

// SearchRequest represents a search request with all parameters
type SearchRequest struct {
	Query          string            `json:"query"`
	IndexName      string            `json:"index_name"`
	ContextSize    int               `json:"context_size"`
	BM25Weight     float64           `json:"bm25_weight"`
	VectorWeight   float64           `json:"vector_weight"`
	UseJapaneseNLP bool              `json:"use_japanese_nlp"`
	TimeoutSeconds int               `json:"timeout_seconds"`
	Filters        map[string]string `json:"filters,omitempty"`
}

// SearchResponse represents the search response with context and references
type SearchResponse struct {
	ContextParts []string          `json:"context_parts"`
	References   map[string]string `json:"references"`
	TotalResults int               `json:"total_results"`
	SearchTime   string            `json:"search_time"`
	IndexUsed    string            `json:"index_used"`
	SearchMethod string            `json:"search_method"`
}

// NewHybridSearchService creates a new hybrid search service
func NewHybridSearchService(config *types.Config, embeddingClient *bedrock.BedrockClient) (*HybridSearchService, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}
	if embeddingClient == nil {
		return nil, fmt.Errorf("embedding client cannot be nil")
	}

	// Validate OpenSearch configuration
	if config.OpenSearchEndpoint == "" {
		return nil, fmt.Errorf("OpenSearch endpoint not configured")
	}

	service := &HybridSearchService{
		config:          config,
		embeddingClient: embeddingClient,
		logger:          log.Default(),
	}

	return service, nil
}

// Initialize sets up the OpenSearch client and hybrid engine
func (s *HybridSearchService) Initialize(ctx context.Context) error {
	// Create OpenSearch client
	osConfig, err := opensearch.NewConfigFromTypes(s.config)
	if err != nil {
		return fmt.Errorf("failed to create OpenSearch config: %w", err)
	}

	if err := osConfig.Validate(); err != nil {
		return fmt.Errorf("OpenSearch config validation failed: %w", err)
	}

	s.osClient, err = opensearch.NewClient(osConfig)
	if err != nil {
		return fmt.Errorf("failed to create OpenSearch client: %w", err)
	}

	// Test connection
	if err := s.osClient.HealthCheck(ctx); err != nil {
		return fmt.Errorf("OpenSearch health check failed: %w", err)
	}

	// Create hybrid search engine
	s.hybridEngine = opensearch.NewHybridSearchEngine(s.osClient, s.embeddingClient)

	s.logger.Printf("Hybrid search service initialized successfully")
	return nil
}

// Search performs hybrid search with the given parameters
func (s *HybridSearchService) Search(ctx context.Context, request *SearchRequest) (*SearchResponse, error) {
	if s.hybridEngine == nil {
		return nil, fmt.Errorf("service not initialized - call Initialize() first")
	}

	if request == nil {
		return nil, fmt.Errorf("search request cannot be nil")
	}

	if request.Query == "" {
		return nil, fmt.Errorf("search query cannot be empty")
	}

	// Set default values if not provided
	if request.ContextSize <= 0 {
		request.ContextSize = 10
	}
	if request.BM25Weight == 0 && request.VectorWeight == 0 {
		request.BM25Weight = 0.5
		request.VectorWeight = 0.5
	}
	if request.TimeoutSeconds <= 0 {
		request.TimeoutSeconds = 30
	}

	// Build hybrid query
	hybridQuery := &opensearch.HybridQuery{
		Query:          request.Query,
		IndexName:      request.IndexName,
		Size:           request.ContextSize,
		BM25Weight:     request.BM25Weight,
		VectorWeight:   request.VectorWeight,
		FusionMethod:   opensearch.FusionMethodWeightedSum,
		UseJapaneseNLP: request.UseJapaneseNLP,
		TimeoutSeconds: request.TimeoutSeconds,
		Filters:        request.Filters,
	}

	// Execute search
	s.logger.Printf("Executing hybrid search: query='%s', index='%s'", request.Query, request.IndexName)
	result, err := s.hybridEngine.Search(ctx, hybridQuery)
	if err != nil {
		return nil, fmt.Errorf("hybrid search failed: %w", err)
	}

	// Extract context and references from results
	response := &SearchResponse{
		ContextParts: make([]string, 0, len(result.FusionResult.Documents)),
		References:   make(map[string]string),
		TotalResults: result.FusionResult.TotalHits,
		SearchTime:   result.ExecutionTime.String(),
		IndexUsed:    request.IndexName,
		SearchMethod: result.SearchMethod,
	}

	for _, doc := range result.FusionResult.Documents {
		// Unmarshal the source JSON
		var source map[string]interface{}
		if err := json.Unmarshal(doc.Source, &source); err != nil {
			s.logger.Printf("Failed to unmarshal document source: %v", err)
			continue // Skip this document if we can't unmarshal
		}

		// Extract content
		if content, ok := source["content"].(string); ok && content != "" {
			response.ContextParts = append(response.ContextParts, content)
		}

		// Extract title and reference
		var title, reference string
		if t, ok := source["title"].(string); ok {
			title = t
		}
		if ref, ok := source["reference"].(string); ok && ref != "" {
			reference = ref
		}
		if title != "" && reference != "" {
			response.References[title] = reference
		}
	}

	s.logger.Printf("Search completed: found %d results in %s", len(response.ContextParts), result.ExecutionTime)
	return response, nil
}

// SearchWithDefaults performs search using configuration defaults
func (s *HybridSearchService) SearchWithDefaults(ctx context.Context, query string, indexName string) (*SearchResponse, error) {
	request := &SearchRequest{
		Query:          query,
		IndexName:      indexName,
		ContextSize:    10, // Default context size
		BM25Weight:     0.5,
		VectorWeight:   0.5,
		UseJapaneseNLP: true,
		TimeoutSeconds: 30,
	}

	return s.Search(ctx, request)
}

// GetIndexName returns the appropriate index name based on search type
func (s *HybridSearchService) GetIndexName(searchType string) string {
	// This mirrors the logic from cmd/chat.go's getIndexNameForChat function
	if s.config.OpenSearchIndex != "" {
		return s.config.OpenSearchIndex
	}

	// Fallback to default naming pattern
	return "ragent-docs"
}

// Close cleans up resources
func (s *HybridSearchService) Close() error {
	if s.osClient != nil {
		// OpenSearch client cleanup if needed
		s.logger.Printf("Hybrid search service closed")
	}
	return nil
}

// SetLogger sets a custom logger for the service
func (s *HybridSearchService) SetLogger(logger *log.Logger) {
	s.logger = logger
}

// GetClient returns the OpenSearch client (for advanced usage)
func (s *HybridSearchService) GetClient() *opensearch.Client {
	return s.osClient
}

// GetHybridEngine returns the hybrid engine (for advanced usage)
func (s *HybridSearchService) GetHybridEngine() *opensearch.HybridSearchEngine {
	return s.hybridEngine
}

// HealthCheck checks if the service and its dependencies are healthy
func (s *HybridSearchService) HealthCheck(ctx context.Context) error {
	if s.osClient == nil {
		return fmt.Errorf("OpenSearch client not initialized")
	}

	if err := s.osClient.HealthCheck(ctx); err != nil {
		return fmt.Errorf("OpenSearch health check failed: %w", err)
	}

	return nil
}
