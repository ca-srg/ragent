package search

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/ca-srg/ragent/internal/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/opensearch"
	"github.com/ca-srg/ragent/internal/slacksearch"
	"github.com/ca-srg/ragent/internal/types"
	"github.com/slack-go/slack"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
)

// HybridSearchService provides reusable hybrid search functionality
type HybridSearchService struct {
	config          *types.Config
	embeddingClient *bedrock.BedrockClient
	osClient        *opensearch.Client
	hybridEngine    *opensearch.HybridSearchEngine
	logger          *log.Logger
	slackService    *slacksearch.SlackSearchService
}

var (
	searchTracer = otel.Tracer("ragent/search")
)

// SearchRequest represents a search request with all parameters
type SearchRequest struct {
	Query             string            `json:"query"`
	IndexName         string            `json:"index_name"`
	ContextSize       int               `json:"context_size"`
	BM25Weight        float64           `json:"bm25_weight"`
	VectorWeight      float64           `json:"vector_weight"`
	UseJapaneseNLP    bool              `json:"use_japanese_nlp"`
	TimeoutSeconds    int               `json:"timeout_seconds"`
	Filters           map[string]string `json:"filters,omitempty"`
	EnableSlackSearch bool              `json:"enable_slack_search"`
	SlackChannels     []string          `json:"slack_channels,omitempty"`
}

// SearchResponse represents the search response with context and references
type SearchResponse struct {
	ContextParts  []string                       `json:"context_parts"`
	References    map[string]string              `json:"references"`
	TotalResults  int                            `json:"total_results"`
	SearchTime    string                         `json:"search_time"`
	IndexUsed     string                         `json:"index_used"`
	SearchMethod  string                         `json:"search_method"`
	SlackResults  *slacksearch.SlackSearchResult `json:"slack_results,omitempty"`
	SearchSources []string                       `json:"search_sources,omitempty"`
}

// NewHybridSearchService creates a new hybrid search service
func NewHybridSearchService(
	config *types.Config,
	embeddingClient *bedrock.BedrockClient,
	slackClient *slack.Client,
	slackBedrockClient *bedrock.BedrockClient,
) (*HybridSearchService, error) {
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

	service.slackService = nil

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
	ctx, span := searchTracer.Start(ctx, "search.hybrid")
	defer span.End()

	if s.hybridEngine == nil {
		err := fmt.Errorf("service not initialized - call Initialize() first")
		span.RecordError(err)
		span.SetStatus(codes.Error, "service_not_initialized")
		return nil, err
	}

	if request == nil {
		err := fmt.Errorf("search request cannot be nil")
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid_request")
		return nil, err
	}

	if request.Query == "" {
		err := fmt.Errorf("search query cannot be empty")
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid_query")
		return nil, err
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

	span.SetAttributes(
		attribute.String("search.query", truncateQueryAttribute(request.Query)),
		attribute.String("search.index", request.IndexName),
		attribute.Int("search.context_size", request.ContextSize),
		attribute.Float64("search.bm25_weight", request.BM25Weight),
		attribute.Float64("search.vector_weight", request.VectorWeight),
		attribute.Int("search.timeout_seconds", request.TimeoutSeconds),
	)
	if len(request.Filters) > 0 {
		span.SetAttributes(attribute.String("search.filters", formatFilters(request.Filters)))
	}

	type (
		documentResult struct {
			response *SearchResponse
			err      error
		}
		slackResult struct {
			response *slacksearch.SlackSearchResult
			err      error
		}
	)

	docCh := make(chan documentResult, 1)
	slackCh := make(chan slackResult, 1)

	group, ctx := errgroup.WithContext(ctx)

	group.Go(func() error {
		s.logger.Printf("Executing hybrid search: query='%s', index='%s'", request.Query, request.IndexName)
		result, err := s.hybridEngine.Search(ctx, hybridQuery)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "hybrid_search_failed")
			docCh <- documentResult{err: fmt.Errorf("hybrid search failed: %w", err)}
			return nil
		}

		resp := &SearchResponse{
			ContextParts: make([]string, 0, len(result.FusionResult.Documents)),
			References:   make(map[string]string),
			TotalResults: result.FusionResult.TotalHits,
			SearchTime:   result.ExecutionTime.String(),
			IndexUsed:    request.IndexName,
			SearchMethod: result.SearchMethod,
		}

		for _, doc := range result.FusionResult.Documents {
			var source map[string]interface{}
			if err := json.Unmarshal(doc.Source, &source); err != nil {
				s.logger.Printf("Failed to unmarshal document source: %v", err)
				span.RecordError(err, trace.WithAttributes(attribute.String("search.document.id", doc.ID)))
				continue
			}
			if content, ok := source["content"].(string); ok && content != "" {
				resp.ContextParts = append(resp.ContextParts, content)
			}
			var title, reference string
			if t, ok := source["title"].(string); ok {
				title = t
			}
			if ref, ok := source["reference"].(string); ok && ref != "" {
				reference = ref
			}
			if title != "" && reference != "" {
				resp.References[title] = reference
			}
		}

		s.logger.Printf("Document search completed: found %d results in %s", len(resp.ContextParts), result.ExecutionTime)

		span.SetAttributes(
			attribute.Int("search.results.total_hits", result.FusionResult.TotalHits),
			attribute.Int("search.results.returned", len(resp.ContextParts)),
			attribute.String("search.method", result.SearchMethod),
			attribute.Float64("search.execution_ms", float64(result.ExecutionTime.Milliseconds())),
		)
		if result.FallbackReason != "" {
			span.SetAttributes(attribute.String("search.fallback_reason", result.FallbackReason))
		}
		if result.URLDetected {
			span.SetAttributes(attribute.Bool("search.url_detected", true))
		}

		docCh <- documentResult{response: resp}
		return nil
	})

	slackEnabled := request.EnableSlackSearch && s.slackService != nil
	if slackEnabled {
		group.Go(func() error {
			timeoutSeconds := s.config.SlackSearchTimeoutSeconds
			if timeoutSeconds <= 0 {
				timeoutSeconds = 5
			}
			slackCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
			defer cancel()
			slackResp, err := s.slackService.Search(slackCtx, request.Query, request.SlackChannels)
			if err != nil {
				slackCh <- slackResult{err: err}
				return nil
			}
			slackCh <- slackResult{response: slackResp}
			return nil
		})
	}

	if err := group.Wait(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "parallel_search_failed")
		return nil, err
	}
	close(docCh)
	close(slackCh)

	var (
		docRes   documentResult
		slackRes slackResult
	)

	if result, ok := <-docCh; ok && result.err != nil {
		return nil, result.err
	} else if ok {
		docRes = result
	}

	if result, ok := <-slackCh; ok {
		slackRes = result
	}

	if docRes.response == nil {
		docRes = documentResult{response: &SearchResponse{
			ContextParts: []string{},
			References:   map[string]string{},
			TotalResults: 0,
			SearchTime:   "0s",
			IndexUsed:    request.IndexName,
		}}
	}

	response := docRes.response
	searchSources := make([]string, 0, 2)
	if len(response.ContextParts) > 0 || response.TotalResults > 0 {
		searchSources = append(searchSources, "documents")
	}
	if slackRes.response != nil {
		response.SlackResults = slackRes.response
		response.TotalResults += slackRes.response.TotalMatches
		searchSources = append(searchSources, "slack")
	} else if slackRes.err != nil {
		s.logger.Printf("Slack search failed, continuing with document results: %v", slackRes.err)
	}
	if len(searchSources) == 0 {
		searchSources = append(searchSources, "documents")
	}

	response.SearchSources = searchSources
	span.SetStatus(codes.Ok, "search_completed")
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

func truncateQueryAttribute(query string) string {
	const maxAttributeLength = 120
	trimmed := strings.TrimSpace(query)
	if len([]rune(trimmed)) <= maxAttributeLength {
		return trimmed
	}
	runes := []rune(trimmed)
	return string(runes[:maxAttributeLength]) + "…"
}

func formatFilters(filters map[string]string) string {
	if len(filters) == 0 {
		return ""
	}
	parts := make([]string, 0, len(filters))
	for key, value := range filters {
		parts = append(parts, fmt.Sprintf("%s=%s", key, value))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
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
