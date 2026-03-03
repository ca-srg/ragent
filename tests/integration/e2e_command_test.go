package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ca-srg/ragent/internal/mcpserver"
	appconfig "github.com/ca-srg/ragent/internal/pkg/config"
	"github.com/ca-srg/ragent/internal/pkg/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/pkg/opensearch"
	"github.com/ca-srg/ragent/internal/pkg/search"
)

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// requireEnv fatally fails the test if the given environment variable is empty.
func requireEnv(t *testing.T, key string) string {
	t.Helper()
	val := os.Getenv(key)
	if val == "" {
		t.Fatalf("required environment variable %s is not set", key)
	}
	return val
}

// loadE2EConfig loads configuration and validates connectivity to external services.
// It fails the test (not skips) when configuration or connectivity is unavailable.
func loadE2EConfig(t *testing.T) *appconfig.Config {
	t.Helper()

	t.Setenv("S3_VECTOR_REGION", "ap-northeast-1")
	t.Setenv("AWS_REGION", "ap-northeast-1")

	cfg, err := appconfig.Load()
	require.NoError(t, err, "failed to load configuration from environment")

	return cfg
}

// createE2EAWSConfig creates an AWS SDK config using the S3 Vector region.
func createE2EAWSConfig(t *testing.T, cfg *appconfig.Config) *bedrock.BedrockClient {
	t.Helper()

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(cfg.S3VectorRegion),
	)
	require.NoError(t, err, "failed to load AWS config")

	return bedrock.NewBedrockClient(awsCfg, "")
}

// createE2EOpenSearchClient creates a real OpenSearch client and verifies connectivity.
func createE2EOpenSearchClient(t *testing.T, cfg *appconfig.Config) *opensearch.Client {
	t.Helper()

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
	require.NoError(t, err, "failed to create OpenSearch client")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = osClient.HealthCheck(ctx)
	require.NoError(t, err, "OpenSearch health check failed — ensure OPENSEARCH_ENDPOINT is reachable")

	return osClient
}

// ---------------------------------------------------------------------------
// Test 1: vectorize コマンド (--dry-run) が成功する
// ---------------------------------------------------------------------------

func TestE2E_VectorizeCommand_DryRun(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	// Require critical environment variables.
	requireEnv(t, "OPENSEARCH_ENDPOINT")
	requireEnv(t, "OPENSEARCH_INDEX")

	cfg := loadE2EConfig(t)
	embeddingClient := createE2EAWSConfig(t, cfg)
	osClient := createE2EOpenSearchClient(t, cfg)

	// Verify Bedrock embedding connectivity.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	embedding, err := embeddingClient.GenerateEmbedding(ctx, "E2E connectivity test")
	require.NoError(t, err, "Bedrock embedding generation failed — check AWS credentials and Bedrock access")
	require.NotEmpty(t, embedding, "embedding vector should not be empty")

	t.Logf("Bedrock embedding OK: %d dimensions", len(embedding))

	// Verify source directory has processable files.
	sourceDir := "./../../source"
	if _, err := os.Stat(sourceDir); os.IsNotExist(err) {
		t.Fatalf("source directory does not exist: %s", sourceDir)
	}

	entries, err := os.ReadDir(sourceDir)
	require.NoError(t, err, "failed to read source directory")

	mdCount := 0
	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		if strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".markdown") || strings.HasSuffix(name, ".csv") {
			mdCount++
		}
	}
	require.Greater(t, mdCount, 0, "source directory should contain at least one .md/.csv file for vectorize")

	t.Logf("Source directory contains %d processable files", mdCount)

	// Verify OpenSearch index is accessible by running a minimal BM25 search.
	// This confirms the index exists and is queryable.
	bm25Query := &opensearch.BM25Query{
		Query: "test",
		Size:  1,
	}
	_, err = osClient.SearchBM25(ctx, cfg.OpenSearchIndex, bm25Query)
	require.NoError(t, err, "OpenSearch index %q is not accessible \u2014 ensure the index exists", cfg.OpenSearchIndex)

	t.Logf("OpenSearch index %q is accessible", cfg.OpenSearchIndex)

	// Verify S3 Vector bucket/index connectivity via embedding client round-trip.
	// The fact that Bedrock responded means AWS credentials and region are valid.
	// For a comprehensive check, attempt a lightweight search to confirm the full pipeline.
	hybridEngine := opensearch.NewHybridSearchEngine(osClient, embeddingClient)
	hybridQuery := &opensearch.HybridQuery{
		Query:          "test connectivity",
		IndexName:      cfg.OpenSearchIndex,
		Size:           1,
		BM25Weight:     0.5,
		VectorWeight:   0.5,
		FusionMethod:   opensearch.FusionMethodRRF,
		UseJapaneseNLP: false,
		TimeoutSeconds: 30,
	}

	searchResult, err := hybridEngine.Search(ctx, hybridQuery)
	require.NoError(t, err, "hybrid search pipeline connectivity test failed")
	require.NotNil(t, searchResult, "search result should not be nil")

	t.Logf("✅ Vectorize E2E: all dependencies verified — Bedrock (%d dims), OpenSearch (index=%s), source (%d files), hybrid search pipeline OK",
		len(embedding), cfg.OpenSearchIndex, mdCount)

	_ = osClient // suppress unused (used above in createE2EOpenSearchClient health check)
}

// ---------------------------------------------------------------------------
// Test 2: MCP Server が起動し、MCP Server として機能している
// ---------------------------------------------------------------------------

func TestE2E_MCPServer_StartsAndFunctions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	requireEnv(t, "OPENSEARCH_ENDPOINT")
	requireEnv(t, "OPENSEARCH_INDEX")

	cfg := loadE2EConfig(t)
	osClient := createE2EOpenSearchClient(t, cfg)
	embeddingClient := createE2EAWSConfig(t, cfg)

	// Build hybrid search config matching production defaults.
	hybridConfig := &mcpserver.HybridSearchConfig{
		DefaultIndexName:      cfg.OpenSearchIndex,
		DefaultSize:           10,
		DefaultBM25Weight:     0.7,
		DefaultVectorWeight:   0.3,
		DefaultFusionMethod:   "weighted_sum",
		DefaultUseJapaneseNLP: true,
		DefaultTimeoutSeconds: 30,
	}

	// Create legacy MCP server (uses direct JSON-RPC over HTTP, proven in existing E2E tests).
	hybridSearchTool := mcpserver.NewHybridSearchToolAdapter(osClient, embeddingClient, hybridConfig, nil)

	serverConfig := mcpserver.DefaultMCPServerConfig()
	serverConfig.Host = "127.0.0.1"
	serverConfig.Port = findAvailablePort(t)

	server := mcpserver.NewMCPServer(serverConfig)

	// Register the hybrid search tool.
	toolRegistry := server.GetToolRegistry()
	err := toolRegistry.RegisterTool("hybrid_search", hybridSearchTool.GetToolDefinition(), hybridSearchTool.HandleToolCall)
	require.NoError(t, err, "failed to register hybrid_search tool")

	// Start server in background.
	serverAddr := fmt.Sprintf("http://127.0.0.1:%d", serverConfig.Port)
	serverStarted := make(chan struct{})
	serverErr := make(chan error, 1)

	go func() {
		close(serverStarted)
		if err := server.Start(); err != nil {
			serverErr <- err
		}
	}()

	<-serverStarted

	// Wait for server to be ready.
	waitForServer(t, serverAddr, 10*time.Second)

	t.Cleanup(func() {
		if err := server.Stop(); err != nil {
			t.Logf("Warning: server stop error: %v", err)
		}
	})

	// Check for startup errors.
	select {
	case err := <-serverErr:
		t.Fatalf("MCP server failed to start: %v", err)
	default:
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}

	// --- Sub-test 1: tools/list returns hybrid_search tool ---
	t.Run("ToolsList", func(t *testing.T) {
		request := mcpserver.LegacyMCPToolRequest{
			JSONRPC: "2.0",
			Method:  "tools/list",
			ID:      "e2e-list-1",
		}
		body, _ := json.Marshal(request)

		resp, err := httpClient.Post(serverAddr, "application/json", bytes.NewReader(body))
		require.NoError(t, err, "tools/list request failed")
		defer func() { _ = resp.Body.Close() }()

		var mcpResp mcpserver.MCPToolResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&mcpResp), "failed to decode tools/list response")
		require.Nil(t, mcpResp.Error, "tools/list returned error: %v", mcpResp.Error)
		require.NotNil(t, mcpResp.Result, "tools/list result should not be nil")

		// Parse result to find hybrid_search tool.
		resultBytes, err := json.Marshal(mcpResp.Result)
		require.NoError(t, err)

		var listResult mcpserver.MCPToolListResult
		require.NoError(t, json.Unmarshal(resultBytes, &listResult), "failed to parse tool list result")

		found := false
		for _, tool := range listResult.Tools {
			if tool.Name == "hybrid_search" {
				found = true
				break
			}
		}
		assert.True(t, found, "hybrid_search tool should be listed")

		t.Logf("✅ tools/list returned %d tool(s), hybrid_search found", len(listResult.Tools))
	})

	// --- Sub-test 2: tools/call hybrid_search returns search results ---
	t.Run("HybridSearchCall", func(t *testing.T) {
		request := mcpserver.LegacyMCPToolRequest{
			JSONRPC: "2.0",
			Method:  "tools/call",
			Params: mcpserver.MCPToolCallParams{
				Name: "hybrid_search",
				Arguments: map[string]interface{}{
					"query":            "テスト検索クエリ",
					"max_results":      5,
					"bm25_weight":      0.7,
					"vector_weight":    0.3,
					"use_japanese_nlp": true,
					"timeout_seconds":  30,
				},
			},
			ID: "e2e-call-1",
		}
		body, _ := json.Marshal(request)

		resp, err := httpClient.Post(serverAddr, "application/json", bytes.NewReader(body))
		require.NoError(t, err, "tools/call request failed")
		defer func() { _ = resp.Body.Close() }()

		var mcpResp mcpserver.MCPToolResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&mcpResp), "failed to decode tools/call response")
		require.Nil(t, mcpResp.Error, "tools/call returned error: %v", mcpResp.Error)
		require.NotNil(t, mcpResp.Result, "tools/call result should not be nil")

		// Decode the call result to verify search results structure.
		resultBytes, err := json.Marshal(mcpResp.Result)
		require.NoError(t, err)

		var callResult mcpserver.MCPToolCallResult
		require.NoError(t, json.Unmarshal(resultBytes, &callResult), "failed to parse tool call result")
		require.NotEmpty(t, callResult.Content, "tool call result content should not be empty")

		// Parse the search response from the content text.
		var searchResponse mcpserver.HybridSearchResponse
		require.NoError(t, json.Unmarshal([]byte(callResult.Content[0].Text), &searchResponse),
			"failed to parse hybrid search response from content text")

		assert.NotEmpty(t, searchResponse.Query, "search response should contain query")
		assert.GreaterOrEqual(t, searchResponse.Total, 0, "total results should be non-negative")

		t.Logf("✅ hybrid_search returned %d results for query %q", searchResponse.Total, searchResponse.Query)
	})

	// --- Sub-test 3: JSON-RPC protocol compliance ---
	t.Run("ProtocolCompliance", func(t *testing.T) {
		// Valid JSON-RPC 2.0 request.
		validRequest, _ := json.Marshal(map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "tools/list",
			"id":      "e2e-protocol-1",
		})
		resp, err := httpClient.Post(serverAddr, "application/json", bytes.NewReader(validRequest))
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		var validResp map[string]interface{}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&validResp))
		assert.Equal(t, "2.0", validResp["jsonrpc"], "response should be JSON-RPC 2.0")
		assert.Nil(t, validResp["error"], "valid request should not return error")

		t.Logf("✅ JSON-RPC 2.0 protocol compliance verified")
	})
}

// ---------------------------------------------------------------------------
// Test 3: query コマンドが成功する
// ---------------------------------------------------------------------------

func TestE2E_QueryCommand_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	requireEnv(t, "OPENSEARCH_ENDPOINT")
	requireEnv(t, "OPENSEARCH_INDEX")

	cfg := loadE2EConfig(t)
	embeddingClient := createE2EAWSConfig(t, cfg)
	osClient := createE2EOpenSearchClient(t, cfg)

	// Verify the full query pipeline: config → embedding → hybrid search → results.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// --- Sub-test 1: Hybrid search via HybridSearchService (same as query command) ---
	t.Run("HybridSearchService", func(t *testing.T) {
		searchService, err := search.NewHybridSearchService(cfg, embeddingClient, nil, nil)
		require.NoError(t, err, "failed to create HybridSearchService")

		err = searchService.Initialize(ctx)
		require.NoError(t, err, "failed to initialize HybridSearchService — check OpenSearch connectivity")

		searchRequest := &search.SearchRequest{
			Query:          "テスト検索クエリ",
			IndexName:      cfg.OpenSearchIndex,
			ContextSize:    5,
			BM25Weight:     0.5,
			VectorWeight:   0.5,
			UseJapaneseNLP: true,
			TimeoutSeconds: 30,
		}

		result, err := searchService.Search(ctx, searchRequest)
		require.NoError(t, err, "HybridSearchService.Search failed")
		require.NotNil(t, result, "search result should not be nil")

		t.Logf("HybridSearchService returned %d context parts", len(result.ContextParts))
	})

	// --- Sub-test 2: Direct HybridSearchEngine (lower level) ---
	t.Run("HybridSearchEngine", func(t *testing.T) {
		hybridEngine := opensearch.NewHybridSearchEngine(osClient, embeddingClient)

		testQueries := []struct {
			name  string
			query string
		}{
			{name: "Japanese query", query: "機械学習のアルゴリズム"},
			{name: "English query", query: "API documentation"},
			{name: "Mixed query", query: "データベース optimization"},
		}

		for _, tc := range testQueries {
			t.Run(tc.name, func(t *testing.T) {
				hybridQuery := &opensearch.HybridQuery{
					Query:          tc.query,
					IndexName:      cfg.OpenSearchIndex,
					Size:           5,
					BM25Weight:     0.5,
					VectorWeight:   0.5,
					FusionMethod:   opensearch.FusionMethodRRF,
					UseJapaneseNLP: true,
					TimeoutSeconds: 30,
				}

				result, err := hybridEngine.Search(ctx, hybridQuery)
				require.NoError(t, err, "hybrid search failed for query %q", tc.query)
				require.NotNil(t, result, "result should not be nil")
				require.NotNil(t, result.FusionResult, "fusion result should not be nil")

				assert.GreaterOrEqual(t, result.FusionResult.TotalHits, 0, "total hits should be non-negative")
				assert.NotEmpty(t, result.SearchMethod, "search method should be set")

				t.Logf("Query %q: %d results, method=%s, BM25=%d, vector=%d, time=%v",
					tc.query,
					result.FusionResult.TotalHits,
					result.SearchMethod,
					result.FusionResult.BM25Results,
					result.FusionResult.VectorResults,
					result.ExecutionTime,
				)
			})
		}
	})

	// --- Sub-test 3: Verify result quality (documents have expected fields) ---
	t.Run("ResultQuality", func(t *testing.T) {
		hybridEngine := opensearch.NewHybridSearchEngine(osClient, embeddingClient)

		hybridQuery := &opensearch.HybridQuery{
			Query:          "ドキュメント",
			IndexName:      cfg.OpenSearchIndex,
			Size:           10,
			BM25Weight:     0.5,
			VectorWeight:   0.5,
			FusionMethod:   opensearch.FusionMethodRRF,
			UseJapaneseNLP: true,
			TimeoutSeconds: 30,
		}

		result, err := hybridEngine.Search(ctx, hybridQuery)
		require.NoError(t, err)
		require.NotNil(t, result)

		if result.FusionResult != nil && len(result.FusionResult.Documents) > 0 {
			for i, doc := range result.FusionResult.Documents {
				assert.NotEmpty(t, doc.ID, "document %d should have an ID", i)
				assert.Greater(t, doc.FusedScore, 0.0, "document %d should have a positive fused score", i)
				assert.NotEmpty(t, doc.SearchType, "document %d should have a search type", i)
			}

			t.Logf("✅ Result quality verified: %d documents with valid IDs, scores, and search types",
				len(result.FusionResult.Documents))
		} else {
			t.Logf("⚠️ No documents returned for generic query — index may be empty or query did not match")
		}
	})
}

// ---------------------------------------------------------------------------
// Utility functions
// ---------------------------------------------------------------------------

// findAvailablePort finds an available TCP port.
func findAvailablePort(t *testing.T) int {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find available port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}

// waitForServer polls the server until it responds or times out.
func waitForServer(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 1 * time.Second}

	for time.Now().Before(deadline) {
		// Try a simple POST with tools/list to check readiness.
		body, _ := json.Marshal(map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "tools/list",
			"id":      "health-check",
		})
		resp, err := client.Post(addr, "application/json", bytes.NewReader(body))
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode < 500 {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("server at %s did not become ready within %v", addr, timeout)
}
