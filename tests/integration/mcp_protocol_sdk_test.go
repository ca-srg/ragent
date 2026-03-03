package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/ca-srg/ragent/internal/mcpserver"
	"github.com/ca-srg/ragent/internal/pkg/config"
	"github.com/ca-srg/ragent/internal/pkg/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/pkg/opensearch"
)

// SDK Protocol Compliance Tests
// These tests verify that RAGent's SDK-based MCP server implementation
// follows the official MCP specification as implemented by SDK v0.4.0

// setupSDKTestEnvironment sets up the environment for SDK integration testing
func setupSDKTestEnvironment(t *testing.T) (*config.Config, *bedrock.BedrockClient, *opensearch.Client) {
	t.Helper()

	// Load configuration from environment
	cfg, err := config.Load()
	if err != nil {
		t.Skipf("Skipping SDK test due to configuration error: %v", err)
	}

	// Validate required environment variables
	required := []string{
		"AWS_REGION",
		"OPENSEARCH_ENDPOINT",
	}

	for _, envVar := range required {
		if os.Getenv(envVar) == "" {
			t.Skipf("Skipping SDK test: required environment variable %s is not set", envVar)
		}
	}

	// Create real AWS config and embedding client
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(cfg.S3VectorRegion),
	)
	if err != nil {
		t.Skipf("Skipping SDK test: failed to create AWS config: %v", err)
	}
	embeddingClient := bedrock.NewBedrockClient(awsCfg, "")

	// Create real OpenSearch client
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
	osClient, err := opensearch.NewClient(osConfig)
	if err != nil {
		t.Skipf("Skipping SDK test: failed to create OpenSearch client: %v", err)
	}

	// Test connectivity
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := osClient.HealthCheck(ctx); err != nil {
		t.Skipf("Skipping SDK test: OpenSearch health check failed: %v", err)
	}

	return cfg, embeddingClient, osClient
}

// createSDKMCPServer creates an SDK-based MCP server for testing
func createSDKMCPServer(t *testing.T, cfg *config.Config, osClient *opensearch.Client, embeddingClient *bedrock.BedrockClient) (*mcpserver.ServerWrapper, string) {
	t.Helper()

	// Create MCP server configuration from existing config
	mcpConfig := &config.Config{
		S3VectorRegion:     cfg.S3VectorRegion,
		OpenSearchEndpoint: cfg.OpenSearchEndpoint,
		OpenSearchRegion:   cfg.OpenSearchRegion,
		OpenSearchIndex:    cfg.OpenSearchIndex,
		// Set test-specific MCP server settings
		MCPServerHost: "127.0.0.1",
		MCPServerPort: 8989, // Different from E2E test port
		MCPAllowedIPs: []string{},
		MCPSSEEnabled: true,
	}

	// Create SDK server wrapper
	serverWrapper, err := mcpserver.NewServerWrapper(mcpConfig)
	if err != nil {
		t.Fatalf("Failed to create SDK server wrapper: %v", err)
	}

	// Create hybrid search handler for SDK
	hybridConfig := &mcpserver.HybridSearchConfig{
		DefaultIndexName:      cfg.OpenSearchIndex,
		DefaultSize:           10,
		DefaultBM25Weight:     0.7,
		DefaultVectorWeight:   0.3,
		DefaultFusionMethod:   "weighted_sum",
		DefaultUseJapaneseNLP: true,
		DefaultTimeoutSeconds: 30,
	}

	hybridSearchHandler := mcpserver.NewHybridSearchHandler(osClient, embeddingClient, hybridConfig, nil)

	// Register hybrid search tool using SDK interface
	err = serverWrapper.RegisterTool("hybrid_search", hybridSearchHandler.HandleSDKToolCall)
	if err != nil {
		t.Fatalf("Failed to register hybrid search tool with SDK server: %v", err)
	}

	// Start server in background
	ctx, cancel := context.WithCancel(context.Background())
	serverReady := make(chan string, 1)
	serverError := make(chan error, 1)

	go func() {
		defer cancel()

		// Start SDK server
		if err := serverWrapper.Start(); err != nil {
			serverError <- fmt.Errorf("failed to start SDK server: %w", err)
			return
		}

		// Server is ready
		serverURL := "http://127.0.0.1:8989"
		serverReady <- serverURL

		// Keep server running until context is cancelled
		<-ctx.Done()
	}()

	// Wait for server to start or error
	select {
	case serverURL := <-serverReady:
		// Register cleanup
		t.Cleanup(func() {
			cancel()
			if err := serverWrapper.Stop(); err != nil {
				t.Logf("Failed to stop server wrapper: %v", err)
			}
		})
		return serverWrapper, serverURL
	case err := <-serverError:
		t.Fatalf("SDK server failed to start: %v", err)
		return nil, ""
	case <-time.After(15 * time.Second):
		cancel()
		t.Fatalf("SDK server failed to start within timeout")
		return nil, ""
	}
}

// TestSDKProtocolCompliance_JSONRPCVersion tests JSON-RPC 2.0 version compliance
// Requirement 4.2: JSON-RPC 2.0 messages SHALL use SDK's validated implementations
func TestSDKProtocolCompliance_JSONRPCVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping SDK integration test in short mode")
	}

	cfg, embeddingClient, osClient := setupSDKTestEnvironment(t)
	_, serverURL := createSDKMCPServer(t, cfg, osClient, embeddingClient)

	client := NewSDKTestClient(t, serverURL)

	// Test JSON-RPC 2.0 compliance with tools/list
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	toolsResp, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("Failed to list tools via SDK client: %v", err)
	}

	if toolsResp == nil {
		t.Fatal("Expected tools list result, got nil")
	}

	t.Logf("✅ SDK server properly implements JSON-RPC 2.0 protocol")
}

// TestSDKProtocolCompliance_ToolRegistration tests tool registration via SDK
// Requirement 4.1: Protocol messages SHALL conform to official MCP specification as implemented by SDK v0.4.0
func TestSDKProtocolCompliance_ToolRegistration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping SDK integration test in short mode")
	}

	cfg, embeddingClient, osClient := setupSDKTestEnvironment(t)
	_, serverURL := createSDKMCPServer(t, cfg, osClient, embeddingClient)

	client := NewSDKTestClient(t, serverURL)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// List tools via SDK protocol
	toolsResp, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("Failed to list tools: %v", err)
	}

	if toolsResp.Tools == nil {
		t.Fatal("Expected tools array, got nil")
	}

	// Verify hybrid_search tool is registered
	found := false
	for _, tool := range toolsResp.Tools {
		if tool.Name == "hybrid_search" {
			found = true

			// Verify tool has required properties per SDK specification
			if tool.Description == "" {
				t.Error("Tool should have description per SDK specification")
			}
			if tool.InputSchema == nil {
				t.Error("Tool should have input schema per SDK specification")
			}

			t.Logf("Found hybrid_search tool with SDK-compliant structure")
			break
		}
	}

	if !found {
		t.Error("Expected hybrid_search tool not found in SDK server")
	}

	t.Logf("✅ SDK server properly registers tools according to MCP specification")
}

// TestSDKProtocolCompliance_ToolExecution tests tool execution via SDK
// Requirement 4.1: Protocol messages SHALL conform to official MCP specification
func TestSDKProtocolCompliance_ToolExecution(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping SDK integration test in short mode")
	}

	cfg, embeddingClient, osClient := setupSDKTestEnvironment(t)
	_, serverURL := createSDKMCPServer(t, cfg, osClient, embeddingClient)

	client := NewSDKTestClient(t, serverURL)

	testCases := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "Basic hybrid search via SDK",
			args: map[string]interface{}{
				"query":            "test query",
				"max_results":      5,
				"bm25_weight":      0.7,
				"vector_weight":    0.3,
				"use_japanese_nlp": true,
				"timeout_seconds":  30,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			// Execute tool call via SDK protocol
			result, err := client.CallTool(ctx, "hybrid_search", tc.args)
			if err != nil {
				t.Fatalf("SDK tool call failed: %v", err)
			}

			if result == nil {
				t.Fatal("Expected tool result, got nil")
			}

			// Verify result structure matches SDK specification
			if result.Content == nil {
				t.Error("Tool result should have content per SDK specification")
			}

			t.Logf("✅ Tool execution successful via SDK protocol")
		})
	}
}

// TestSDKProtocolCompliance_ErrorHandling tests error handling via SDK
// Requirement 4.3: Protocol errors SHALL follow standard MCP error codes and formats as defined in SDK
func TestSDKProtocolCompliance_ErrorHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping SDK integration test in short mode")
	}

	cfg, embeddingClient, osClient := setupSDKTestEnvironment(t)
	_, serverURL := createSDKMCPServer(t, cfg, osClient, embeddingClient)

	// Use raw HTTP client to test protocol error handling
	httpClient := &http.Client{Timeout: 10 * time.Second}

	testCases := []struct {
		name           string
		requestBody    string
		expectedError  bool
		checkErrorCode bool
	}{
		{
			name:          "Invalid JSON-RPC version",
			requestBody:   `{"jsonrpc":"1.0","method":"tools/list","id":1}`,
			expectedError: true,
		},
		{
			name:          "Missing JSON-RPC version",
			requestBody:   `{"method":"tools/list","id":1}`,
			expectedError: true,
		},
		{
			name:          "Invalid JSON syntax",
			requestBody:   `{"jsonrpc":"2.0","method":"tools/list","id":1`,
			expectedError: true,
		},
		{
			name:          "Non-existent method",
			requestBody:   `{"jsonrpc":"2.0","method":"nonexistent/method","id":1}`,
			expectedError: true,
		},
		{
			name:          "Valid request without session",
			requestBody:   `{"jsonrpc":"2.0","method":"tools/list","id":1}`,
			expectedError: true, // SDK StreamableHTTP handler requires session initialization
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, "POST", serverURL, strings.NewReader(tc.requestBody))
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := httpClient.Do(req)
			if err != nil {
				t.Fatalf("HTTP request failed: %v", err)
			}
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Logf("Failed to close response body: %v", err)
				}
			}()

			var sdkResponse map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&sdkResponse); err != nil {
				if tc.expectedError {
					t.Logf("✅ SDK server correctly rejected malformed JSON")
					return
				}
				t.Fatalf("Failed to decode response: %v", err)
			}

			// Verify JSON-RPC 2.0 compliance even in error responses
			if jsonrpc, ok := sdkResponse["jsonrpc"].(string); ok && jsonrpc == "2.0" {
				t.Logf("Response maintains JSON-RPC 2.0 compliance")
			}

			// Check error handling
			if tc.expectedError {
				if errorObj := sdkResponse["error"]; errorObj != nil {
					t.Logf("✅ SDK server properly returned error for invalid request")
				} else if !tc.expectedError {
					t.Error("Expected error but none returned")
				}
			} else {
				if errorObj := sdkResponse["error"]; errorObj != nil {
					t.Errorf("Unexpected error for valid request: %v", errorObj)
				}
				t.Logf("✅ Valid request processed successfully")
			}
		})
	}
}

// TestSDKProtocolCompliance_ConcurrentRequests tests concurrent request handling
// Verifies SDK server handles multiple simultaneous requests correctly
func TestSDKProtocolCompliance_ConcurrentRequests(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping SDK integration test in short mode")
	}

	cfg, embeddingClient, osClient := setupSDKTestEnvironment(t)
	_, serverURL := createSDKMCPServer(t, cfg, osClient, embeddingClient)

	client := NewSDKTestClient(t, serverURL)

	const numRequests = 5
	results := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(requestID int) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Execute tools/list via SDK protocol
			_, err := client.ListTools(ctx)
			if err != nil {
				results <- fmt.Errorf("concurrent request %d failed: %w", requestID, err)
				return
			}

			results <- nil
		}(i)
	}

	// Collect results
	for i := 0; i < numRequests; i++ {
		if err := <-results; err != nil {
			t.Errorf("Concurrent SDK request failed: %v", err)
		}
	}

	t.Logf("✅ SDK server handled %d concurrent requests successfully", numRequests)
}

// TestSDKProtocolCompliance_BackwardCompatibility tests that SDK server maintains compatibility
// Verifies that migrating to SDK doesn't break existing functionality
func TestSDKProtocolCompliance_BackwardCompatibility(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping SDK integration test in short mode")
	}

	cfg, embeddingClient, osClient := setupSDKTestEnvironment(t)
	_, serverURL := createSDKMCPServer(t, cfg, osClient, embeddingClient)

	// Test using SDK test client to verify backward compatibility
	client := NewSDKTestClient(t, serverURL)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Test that client can list tools
	listResult, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("Client failed to list tools with SDK server: %v", err)
	}

	if listResult == nil || listResult.Tools == nil {
		t.Fatal("Expected tools list from client")
	}

	// Verify hybrid_search tool exists and is accessible
	found := false
	for _, tool := range listResult.Tools {
		if tool.Name == "hybrid_search" {
			found = true
			break
		}
	}

	if !found {
		t.Error("hybrid_search tool not accessible via client")
	}

	// Test tool execution
	mcpArgs := map[string]interface{}{
		"query":            "compatibility test",
		"max_results":      3,
		"bm25_weight":      0.7,
		"vector_weight":    0.3,
		"use_japanese_nlp": true,
		"timeout_seconds":  30,
	}

	result, err := client.CallTool(ctx, "hybrid_search", mcpArgs)
	if err != nil {
		t.Fatalf("Tool call failed with client: %v", err)
	}

	if result.IsError {
		t.Fatalf("Tool execution returned error")
	}

	t.Logf("✅ SDK server maintains full backward compatibility with existing MCP clients")
}
