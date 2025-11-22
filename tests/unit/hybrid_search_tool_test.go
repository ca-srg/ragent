package unit

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/ca-srg/ragent/internal/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/mcpserver"
	"github.com/ca-srg/ragent/internal/opensearch"
)

// Helper function to create test adapter
func createTestAdapter() *mcpserver.HybridSearchToolAdapter {
	// Create an OpenSearch client pointing to an unreachable local endpoint
	// so that health checks fail fast without external dependencies.
	osCfg := &opensearch.Config{
		Endpoint:          "http://127.0.0.1:1",
		Region:            "us-east-1",
		InsecureSkipTLS:   true,
		RateLimit:         10,
		RateBurst:         20,
		ConnectionTimeout: 0,
		RequestTimeout:    0,
		MaxRetries:        1,
		RetryDelay:        0,
		MaxConnections:    5,
		MaxIdleConns:      2,
		IdleConnTimeout:   0,
	}
	osClient, _ := opensearch.NewClient(osCfg)

	// Create a Bedrock client with a dummy region; it won’t be used because
	// the OpenSearch health check fails first in HandleToolCall.
	awsCfg, _ := awsconfig.LoadDefaultConfig(context.Background(), awsconfig.WithRegion("us-east-1"))
	brClient := bedrock.NewBedrockClient(awsCfg, "")

	config := &mcpserver.HybridSearchConfig{
		DefaultIndexName:      "test-index",
		DefaultSize:           5,
		DefaultBM25Weight:     0.7,
		DefaultVectorWeight:   0.3,
		DefaultFusionMethod:   "weighted_sum",
		DefaultUseJapaneseNLP: true,
		DefaultTimeoutSeconds: 30,
	}

	return mcpserver.NewHybridSearchToolAdapter(osClient, brClient, config, nil)
}

func TestHybridSearchToolAdapter_GetToolDefinition(t *testing.T) {
	adapter := createTestAdapter()

	definition := adapter.GetToolDefinition()

	if definition.Name == "" {
		t.Error("Tool definition should have a name")
	}

	if definition.Name != "hybrid_search" {
		t.Errorf("Expected tool name 'hybrid_search', got '%s'", definition.Name)
	}

	if definition.Description == "" {
		t.Error("Tool definition should have a description")
	}

	if definition.InputSchema == nil {
		t.Error("Tool definition should have input schema")
	}

	// Marshal the tool definition to JSON and inspect the inputSchema
	defBytes, err := json.Marshal(definition)
	if err != nil {
		t.Fatalf("Failed to marshal tool definition: %v", err)
	}
	var defMap map[string]interface{}
	if err := json.Unmarshal(defBytes, &defMap); err != nil {
		t.Fatalf("Failed to unmarshal tool definition JSON: %v", err)
	}
	inputSchema, ok := defMap["inputSchema"].(map[string]interface{})
	if !ok {
		t.Error("Input schema should be an object")
		return
	}
	properties, ok := inputSchema["properties"].(map[string]interface{})
	if !ok {
		t.Error("Input schema should have properties")
		return
	}

	// Check required query parameter
	if _, exists := properties["query"]; !exists {
		t.Error("Input schema should have 'query' property")
	}

	// Check optional parameters exist
	expectedParams := []string{"top_k", "search_mode", "bm25_weight", "vector_weight", "filters", "enable_slack_search", "slack_channels"}
	for _, param := range expectedParams {
		if _, exists := properties[param]; !exists {
			t.Errorf("Input schema should have '%s' property", param)
		}
	}

	// Check required parameters
	requiredIface, ok := inputSchema["required"].([]interface{})
	if !ok {
		t.Error("Input schema should have required array")
		return
	}

	hasQuery := false
	for _, req := range requiredIface {
		if s, ok := req.(string); ok && s == "query" {
			hasQuery = true
			break
		}
	}
	if !hasQuery {
		t.Error("Input schema should require 'query' parameter")
	}
}

func TestHybridSearchToolAdapter_DefaultConfig(t *testing.T) {
	adapter := createTestAdapter()

	config := adapter.GetDefaultConfig()

	if config == nil {
		t.Error("GetDefaultConfig() should return config")
		return
	}

	if config.DefaultSize != 5 {
		t.Errorf("Default size should be 5, got %d", config.DefaultSize)
	}

	if config.DefaultBM25Weight != 0.7 {
		t.Errorf("Default BM25 weight should be 0.7, got %f", config.DefaultBM25Weight)
	}

	if config.DefaultVectorWeight != 0.3 {
		t.Errorf("Default vector weight should be 0.3, got %f", config.DefaultVectorWeight)
	}

	if config.DefaultIndexName != "test-index" {
		t.Errorf("Default index name should be 'test-index', got '%s'", config.DefaultIndexName)
	}

	if config.DefaultFusionMethod != "weighted_sum" {
		t.Errorf("Default fusion method should be 'weighted_sum', got '%s'", config.DefaultFusionMethod)
	}

	if !config.DefaultUseJapaneseNLP {
		t.Error("Default UseJapaneseNLP should be true")
	}

	if config.DefaultTimeoutSeconds != 30 {
		t.Errorf("Default timeout should be 30, got %d", config.DefaultTimeoutSeconds)
	}
}

func TestHybridSearchToolAdapter_HandleToolCall_ParameterValidation(t *testing.T) {
	tests := []struct {
		name           string
		params         map[string]interface{}
		expectError    bool
		errorSubstring string
	}{
		{
			name: "missing query parameter",
			params: map[string]interface{}{
				"top_k": 10,
			},
			expectError:    true,
			errorSubstring: "query parameter is required",
		},
		{
			name: "empty query parameter",
			params: map[string]interface{}{
				"query": "",
			},
			expectError:    true,
			errorSubstring: "query cannot be empty",
		},
		{
			name: "invalid query type",
			params: map[string]interface{}{
				"query": 123,
			},
			expectError:    true,
			errorSubstring: "query must be a string",
		},
		{
			name: "invalid top_k - too high",
			params: map[string]interface{}{
				"query": "test query",
				"top_k": 150,
			},
			expectError:    true,
			errorSubstring: "top_k must be between 1 and 100",
		},
		{
			name: "invalid top_k - too low",
			params: map[string]interface{}{
				"query": "test query",
				"top_k": 0,
			},
			expectError:    true,
			errorSubstring: "top_k must be between 1 and 100",
		},
		{
			name: "invalid bm25_weight - too high",
			params: map[string]interface{}{
				"query":       "test query",
				"bm25_weight": 1.5,
			},
			expectError:    true,
			errorSubstring: "bm25_weight must be between 0.0 and 1.0",
		},
		{
			name: "invalid vector_weight - negative",
			params: map[string]interface{}{
				"query":         "test query",
				"vector_weight": -0.1,
			},
			expectError:    true,
			errorSubstring: "vector_weight must be between 0.0 and 1.0",
		},
		{
			name: "slack search requested without configuration",
			params: map[string]interface{}{
				"query":               "test query",
				"enable_slack_search": true,
			},
			expectError:    true,
			errorSubstring: "slack search requested but not configured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := createTestAdapter()
			ctx := context.Background()

			result, _ := adapter.HandleToolCall(ctx, tt.params)

			if tt.expectError {
				// Should get error result for invalid parameters
				if result == nil {
					t.Error("HandleToolCall() should return result even for invalid params")
					return
				}

				if !result.IsError {
					t.Error("HandleToolCall() result should indicate error for invalid params")
					return
				}

				// Check error message contains expected substring
				if tt.errorSubstring != "" && len(result.Content) > 0 {
					found := false
					for _, content := range result.Content {
						if strings.Contains(content.Text, tt.errorSubstring) {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("Expected error message to contain '%s', got: %v", tt.errorSubstring, result.Content)
					}
				}
			}
		})
	}
}

func TestHybridSearchToolAdapter_HandleToolCall_HealthCheckFailure(t *testing.T) {
	// Use the default test adapter which points to an unreachable OpenSearch
	// endpoint so that health check fails.
	adapter := createTestAdapter()

	params := map[string]interface{}{
		"query": "test query",
	}

	ctx := context.Background()
	result, _ := adapter.HandleToolCall(ctx, params)

	// Should get error result when health check fails
	if result == nil {
		t.Error("HandleToolCall() should return result even when health check fails")
		return
	}

	if !result.IsError {
		t.Error("HandleToolCall() result should indicate error when health check fails")
		return
	}

	// Check that error mentions OpenSearch connection failure
	if len(result.Content) > 0 {
		found := false
		for _, content := range result.Content {
			if strings.Contains(content.Text, "OpenSearch connection failed") || strings.Contains(content.Text, "health check failed") {
				found = true
				break
			}
		}
		if !found {
			t.Error("Error message should mention OpenSearch connection failure")
		}
	}
}

func TestHybridSearchToolAdapter_HandleToolCall_ValidParameters(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]interface{}
	}{
		{
			name: "basic valid params",
			params: map[string]interface{}{
				"query": "test query",
			},
		},
		{
			name: "all valid params",
			params: map[string]interface{}{
				"query":            "comprehensive test query",
				"top_k":            10,
				"search_mode":      "hybrid",
				"bm25_weight":      0.7,
				"vector_weight":    0.3,
				"min_score":        0.1,
				"include_metadata": true,
				"filters": map[string]interface{}{
					"category": "test",
					"type":     "document",
				},
			},
		},
		{
			name: "different search modes",
			params: map[string]interface{}{
				"query":       "test query",
				"search_mode": "bm25",
			},
		},
		{
			name: "vector search mode",
			params: map[string]interface{}{
				"query":       "test query",
				"search_mode": "vector",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := createTestAdapter()
			ctx := context.Background()

			result, _ := adapter.HandleToolCall(ctx, tt.params)

			// Should get some result (even if it's an error due to missing backend)
			if result == nil {
				t.Error("HandleToolCall() should return result for valid params")
				return
			}

			// For valid parameters, we shouldn't get parameter validation errors
			if result.IsError && len(result.Content) > 0 {
				errorText := result.Content[0].Text
				if strings.Contains(errorText, "Invalid parameters") {
					t.Errorf("Should not get parameter validation error for valid params: %s", errorText)
				}
			}

			// Log result for debugging
			t.Logf("Test '%s' result: IsError=%v", tt.name, result.IsError)
		})
	}
}

func TestHybridSearchToolAdapter_SetAndGetDefaultConfig(t *testing.T) {
	adapter := createTestAdapter()

	// Test initial config
	originalConfig := adapter.GetDefaultConfig()
	if originalConfig == nil {
		t.Error("Should have default config")
		return
	}

	// Test setting new config
	newConfig := &mcpserver.HybridSearchConfig{
		DefaultIndexName:      "new-index",
		DefaultSize:           20,
		DefaultBM25Weight:     0.6,
		DefaultVectorWeight:   0.4,
		DefaultFusionMethod:   "rrf",
		DefaultUseJapaneseNLP: false,
		DefaultTimeoutSeconds: 60,
	}

	adapter.SetDefaultConfig(newConfig)

	// Verify config was updated
	updatedConfig := adapter.GetDefaultConfig()
	if updatedConfig == nil {
		t.Error("Should have updated config")
		return
	}

	if updatedConfig.DefaultIndexName != "new-index" {
		t.Errorf("Index name should be updated to 'new-index', got '%s'", updatedConfig.DefaultIndexName)
	}

	if updatedConfig.DefaultSize != 20 {
		t.Errorf("Size should be updated to 20, got %d", updatedConfig.DefaultSize)
	}

	if updatedConfig.DefaultBM25Weight != 0.6 {
		t.Errorf("BM25 weight should be updated to 0.6, got %f", updatedConfig.DefaultBM25Weight)
	}

	if updatedConfig.DefaultFusionMethod != "rrf" {
		t.Errorf("Fusion method should be updated to 'rrf', got '%s'", updatedConfig.DefaultFusionMethod)
	}
}

// Benchmark test for HandleToolCall
func BenchmarkHybridSearchToolAdapter_HandleToolCall(b *testing.B) {
	adapter := createTestAdapter()

	params := map[string]interface{}{
		"query": "benchmark query",
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = adapter.HandleToolCall(ctx, params)
	}
}

// Benchmark test for GetToolDefinition
func BenchmarkHybridSearchToolAdapter_GetToolDefinition(b *testing.B) {
	adapter := createTestAdapter()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = adapter.GetToolDefinition()
	}
}
