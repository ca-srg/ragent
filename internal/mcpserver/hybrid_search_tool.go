package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"

	"github.com/ca-srg/ragent/internal/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/opensearch"
	"github.com/ca-srg/ragent/internal/types"
	"github.com/google/jsonschema-go/jsonschema"
)

// HybridSearchToolAdapter adapts existing hybrid search functionality to MCP tool interface
type HybridSearchToolAdapter struct {
	osClient        *opensearch.Client
	embeddingClient *bedrock.BedrockClient
	hybridEngine    *opensearch.HybridSearchEngine
	defaultConfig   *HybridSearchConfig
	logger          *log.Logger
}

// HybridSearchConfig contains configuration for hybrid search
type HybridSearchConfig struct {
	DefaultIndexName      string
	DefaultSize           int
	DefaultBM25Weight     float64
	DefaultVectorWeight   float64
	DefaultFusionMethod   string
	DefaultUseJapaneseNLP bool
	DefaultTimeoutSeconds int
}

// NewHybridSearchToolAdapter creates a new hybrid search tool adapter
func NewHybridSearchToolAdapter(osClient *opensearch.Client, embeddingClient *bedrock.BedrockClient, config *HybridSearchConfig) *HybridSearchToolAdapter {
	if config == nil {
		config = &HybridSearchConfig{
			DefaultIndexName:      "ragent-docs",
			DefaultSize:           10,
			DefaultBM25Weight:     0.5,
			DefaultVectorWeight:   0.5,
			DefaultFusionMethod:   string(opensearch.FusionMethodWeightedSum),
			DefaultUseJapaneseNLP: true,
			DefaultTimeoutSeconds: 30,
		}
	}

	hybridEngine := opensearch.NewHybridSearchEngine(osClient, embeddingClient)

	return &HybridSearchToolAdapter{
		osClient:        osClient,
		embeddingClient: embeddingClient,
		hybridEngine:    hybridEngine,
		defaultConfig:   config,
		logger:          log.New(log.Writer(), "[HybridSearchTool] ", log.LstdFlags),
	}
}

// GetToolDefinition returns the MCP tool definition for hybrid search
func (hsta *HybridSearchToolAdapter) GetToolDefinition() types.MCPToolDefinition {
	// Define the input schema as a map first
	schemaMap := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Search query text",
			},
			"top_k": map[string]interface{}{
				"type":        "integer",
				"description": "Number of results to return (1-100)",
				"minimum":     1,
				"maximum":     100,
				"default":     10,
			},
			"filters": map[string]interface{}{
				"type":        "object",
				"description": "Key-value filters to apply to search",
				"additionalProperties": map[string]interface{}{
					"type": "string",
				},
			},
			"search_mode": map[string]interface{}{
				"type":        "string",
				"description": "Search mode: 'hybrid', 'bm25', or 'vector'",
				"enum":        []string{"hybrid", "bm25", "vector"},
				"default":     "hybrid",
			},
			"bm25_weight": map[string]interface{}{
				"type":        "number",
				"description": "Weight for BM25 scoring in hybrid mode (0.0-1.0)",
				"minimum":     0.0,
				"maximum":     1.0,
				"default":     0.5,
			},
			"vector_weight": map[string]interface{}{
				"type":        "number",
				"description": "Weight for vector scoring in hybrid mode (0.0-1.0)",
				"minimum":     0.0,
				"maximum":     1.0,
				"default":     0.5,
			},
			"min_score": map[string]interface{}{
				"type":        "number",
				"description": "Minimum score threshold for results",
				"minimum":     0.0,
				"default":     0.0,
			},
			"include_metadata": map[string]interface{}{
				"type":        "boolean",
				"description": "Include search execution metadata in response",
				"default":     false,
			},
			"fusion_method": map[string]interface{}{
				"type":        "string",
				"description": "Fusion method for combining BM25 and vector results",
				"enum":        []string{"weighted_sum", "rrf"},
				"default":     "weighted_sum",
			},
			"use_japanese_nlp": map[string]interface{}{
				"type":        "boolean",
				"description": "Enable Japanese NLP processing for better Japanese text matching",
				"default":     true,
			},
		},
		"required": []string{"query"},
	}

	// Convert map to *jsonschema.Schema
	var inputSchema *jsonschema.Schema
	schemaBytes, err := json.Marshal(schemaMap)
	if err == nil {
		inputSchema = &jsonschema.Schema{}
		_ = json.Unmarshal(schemaBytes, inputSchema)
	}

	return types.MCPToolDefinition{
		Name:        "hybrid_search",
		Description: "Perform hybrid search using BM25 + vector search with configurable fusion methods",
		InputSchema: inputSchema,
	}
}

// HandleToolCall executes the hybrid search tool
func (hsta *HybridSearchToolAdapter) HandleToolCall(ctx context.Context, params map[string]interface{}) (*types.MCPToolCallResult, error) {
	hsta.logger.Printf("Executing hybrid search tool with params: %+v", params)

	// Extract and validate parameters
	searchRequest, err := hsta.parseParams(params)
	if err != nil {
		return CreateToolCallErrorResult(fmt.Sprintf("Invalid parameters: %v", err)), err
	}

	// Test OpenSearch connection
	if err := hsta.osClient.HealthCheck(ctx); err != nil {
		errorMsg := fmt.Sprintf("OpenSearch connection failed: %v", err)
		hsta.logger.Printf("Health check failed: %v", err)
		return CreateToolCallErrorResult(errorMsg), fmt.Errorf("%s", errorMsg)
	}

	// Execute search based on mode
	var result *opensearch.HybridSearchResult
	switch searchRequest.SearchMode {
	case "hybrid":
		result, err = hsta.executeHybridSearch(ctx, searchRequest)
	case "bm25":
		result, err = hsta.executeBM25Search(ctx, searchRequest)
	case "vector":
		result, err = hsta.executeVectorSearch(ctx, searchRequest)
	default:
		result, err = hsta.executeHybridSearch(ctx, searchRequest)
	}

	if err != nil {
		errorMsg := fmt.Sprintf("Search execution failed: %v", err)
		hsta.logger.Printf("Search failed: %v", err)
		return CreateToolCallErrorResult(errorMsg), err
	}

	// Convert to MCP format
	mcpResponse := hsta.convertToMCPResponse(searchRequest, result)
	responseJSON, err := json.MarshalIndent(mcpResponse, "", "  ")
	if err != nil {
		errorMsg := fmt.Sprintf("Failed to serialize response: %v", err)
		return CreateToolCallErrorResult(errorMsg), err
	}

	hsta.logger.Printf("Search completed successfully - found %d results in %v",
		len(result.FusionResult.Documents), result.ExecutionTime)

	return CreateToolCallResult(string(responseJSON)), nil
}

// parseParams extracts and validates parameters from MCP tool call
func (hsta *HybridSearchToolAdapter) parseParams(params map[string]interface{}) (*types.HybridSearchRequest, error) {
	request := &types.HybridSearchRequest{
		SearchMode:      "hybrid",
		TopK:            hsta.defaultConfig.DefaultSize,
		BM25Weight:      hsta.defaultConfig.DefaultBM25Weight,
		VectorWeight:    hsta.defaultConfig.DefaultVectorWeight,
		MinScore:        0.0,
		IncludeMetadata: false,
		Filters:         make(map[string]string),
	}

	// Required query parameter
	if queryInterface, ok := params["query"]; ok {
		if query, ok := queryInterface.(string); ok {
			request.Query = query
		} else {
			return nil, fmt.Errorf("query must be a string")
		}
	} else {
		return nil, fmt.Errorf("query parameter is required")
	}

	// Optional parameters
	if topKInterface, ok := params["top_k"]; ok {
		if topK, ok := topKInterface.(float64); ok {
			request.TopK = int(topK)
		} else if topKStr, ok := topKInterface.(string); ok {
			if topK, err := strconv.Atoi(topKStr); err == nil {
				request.TopK = topK
			}
		}
	}

	if searchModeInterface, ok := params["search_mode"]; ok {
		if searchMode, ok := searchModeInterface.(string); ok {
			request.SearchMode = searchMode
		}
	}

	if bm25WeightInterface, ok := params["bm25_weight"]; ok {
		if bm25Weight, ok := bm25WeightInterface.(float64); ok {
			request.BM25Weight = bm25Weight
		}
	}

	if vectorWeightInterface, ok := params["vector_weight"]; ok {
		if vectorWeight, ok := vectorWeightInterface.(float64); ok {
			request.VectorWeight = vectorWeight
		}
	}

	if minScoreInterface, ok := params["min_score"]; ok {
		if minScore, ok := minScoreInterface.(float64); ok {
			request.MinScore = minScore
		}
	}

	if includeMetadataInterface, ok := params["include_metadata"]; ok {
		if includeMetadata, ok := includeMetadataInterface.(bool); ok {
			request.IncludeMetadata = includeMetadata
		}
	}

	if filtersInterface, ok := params["filters"]; ok {
		if filters, ok := filtersInterface.(map[string]interface{}); ok {
			for k, v := range filters {
				if strVal, ok := v.(string); ok {
					request.Filters[k] = strVal
				}
			}
		}
	}

	// Validate parameters
	if request.Query == "" {
		return nil, fmt.Errorf("query cannot be empty")
	}
	if request.TopK < 1 || request.TopK > 100 {
		return nil, fmt.Errorf("top_k must be between 1 and 100")
	}
	if request.BM25Weight < 0 || request.BM25Weight > 1 {
		return nil, fmt.Errorf("bm25_weight must be between 0.0 and 1.0")
	}
	if request.VectorWeight < 0 || request.VectorWeight > 1 {
		return nil, fmt.Errorf("vector_weight must be between 0.0 and 1.0")
	}

	return request, nil
}

// executeHybridSearch performs hybrid search
func (hsta *HybridSearchToolAdapter) executeHybridSearch(ctx context.Context, request *types.HybridSearchRequest) (*opensearch.HybridSearchResult, error) {
	hybridQuery := hsta.buildHybridQuery(request)
	return hsta.hybridEngine.Search(ctx, hybridQuery)
}

// executeBM25Search performs BM25-only search
func (hsta *HybridSearchToolAdapter) executeBM25Search(ctx context.Context, request *types.HybridSearchRequest) (*opensearch.HybridSearchResult, error) {
	hybridQuery := hsta.buildHybridQuery(request)
	return hsta.hybridEngine.SearchBM25Only(ctx, hybridQuery)
}

// executeVectorSearch performs vector-only search
func (hsta *HybridSearchToolAdapter) executeVectorSearch(ctx context.Context, request *types.HybridSearchRequest) (*opensearch.HybridSearchResult, error) {
	hybridQuery := hsta.buildHybridQuery(request)
	return hsta.hybridEngine.SearchVectorOnly(ctx, hybridQuery)
}

// buildHybridQuery constructs HybridQuery from MCP request
func (hsta *HybridSearchToolAdapter) buildHybridQuery(request *types.HybridSearchRequest) *opensearch.HybridQuery {
	fusionMethod := opensearch.FusionMethodWeightedSum
	if request.SearchMode == "rrf" || len(request.Query) > 0 {
		// Can be extended to support different fusion methods
		fusionMethod = opensearch.FusionMethodWeightedSum
	}

	return &opensearch.HybridQuery{
		Query:          request.Query,
		IndexName:      hsta.defaultConfig.DefaultIndexName,
		Size:           request.TopK,
		BM25Weight:     request.BM25Weight,
		VectorWeight:   request.VectorWeight,
		FusionMethod:   fusionMethod,
		UseJapaneseNLP: hsta.defaultConfig.DefaultUseJapaneseNLP,
		TimeoutSeconds: hsta.defaultConfig.DefaultTimeoutSeconds,
		Filters:        request.Filters,
		MinScore:       request.MinScore,
		K:              request.TopK * 2, // Fetch more candidates for better fusion
	}
}

// convertToMCPResponse converts HybridSearchResult to MCP response format
func (hsta *HybridSearchToolAdapter) convertToMCPResponse(request *types.HybridSearchRequest, result *opensearch.HybridSearchResult) *types.HybridSearchResponse {
	response := &types.HybridSearchResponse{
		Query:      request.Query,
		Total:      result.FusionResult.TotalHits,
		SearchMode: request.SearchMode,
		Results:    make([]types.HybridSearchResultItem, 0, len(result.FusionResult.Documents)),
	}

	// Convert documents to result items
	for _, doc := range result.FusionResult.Documents {
		var source map[string]interface{}
		if err := json.Unmarshal(doc.Source, &source); err != nil {
			continue // Skip documents that can't be parsed
		}

		item := types.HybridSearchResultItem{
			ID:     doc.ID,
			Score:  doc.Score,
			Source: request.SearchMode,
		}

		// Extract standard fields
		if title, ok := source["title"].(string); ok {
			item.Title = title
		}
		if content, ok := source["content"].(string); ok {
			item.Content = content
		}
		if path, ok := source["path"].(string); ok {
			item.Path = path
		}
		if category, ok := source["category"].(string); ok {
			item.Category = category
		}
		if createdAt, ok := source["created_at"].(string); ok {
			item.CreatedAt = createdAt
		}
		if updatedAt, ok := source["updated_at"].(string); ok {
			item.UpdatedAt = updatedAt
		}

		// Extract tags if present
		if tagsInterface, ok := source["tags"]; ok {
			if tags, ok := tagsInterface.([]interface{}); ok {
				for _, tag := range tags {
					if tagStr, ok := tag.(string); ok {
						item.Tags = append(item.Tags, tagStr)
					}
				}
			}
		}

		// Include metadata if requested
		if request.IncludeMetadata {
			item.Metadata = source
		}

		response.Results = append(response.Results, item)
	}

	// Add metadata if requested
	if request.IncludeMetadata {
		response.Metadata = &types.HybridSearchMetadata{
			ExecutionTimeMs: result.ExecutionTime.Milliseconds(),
			BM25Weight:      request.BM25Weight,
			VectorWeight:    request.VectorWeight,
		}

		if result.BM25Response != nil {
			response.Metadata.S3VectorResults = len(result.BM25Response.Hits.Hits)
		}
		if result.VectorResponse != nil {
			response.Metadata.OpenSearchResults = len(result.VectorResponse.Hits.Hits)
		}
	}

	return response
}

// SetDefaultConfig updates the default configuration
func (hsta *HybridSearchToolAdapter) SetDefaultConfig(config *HybridSearchConfig) {
	hsta.defaultConfig = config
}

// GetDefaultConfig returns the current default configuration
func (hsta *HybridSearchToolAdapter) GetDefaultConfig() *HybridSearchConfig {
	return hsta.defaultConfig
}
