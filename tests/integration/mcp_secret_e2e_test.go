package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ca-srg/ragent/internal/mcpserver"
	"github.com/ca-srg/ragent/internal/pkg/config"
	"github.com/ca-srg/ragent/internal/pkg/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/pkg/opensearch"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	secretE2ETestPort     = 8991
	secretE2ETestOIDCPort = 8992
)

// createSDKE2EServerWithOIDC creates an SDK-based MCP server with OIDC context injection for secret access testing.
// It builds a custom HTTP handler chain that wraps the SDK handler with the OIDC context injector,
// simulating authenticated access for E2E tests.
func createSDKE2EServerWithOIDC(
	t *testing.T,
	cfg *config.Config,
	osClient *opensearch.Client,
	embeddingClient *bedrock.BedrockClient,
) (string, func()) {
	t.Helper()

	mcpConfig := &config.Config{
		S3VectorRegion:     cfg.S3VectorRegion,
		OpenSearchEndpoint: cfg.OpenSearchEndpoint,
		OpenSearchRegion:   cfg.OpenSearchRegion,
		MCPServerHost:      "127.0.0.1",
		MCPServerPort:      secretE2ETestOIDCPort,
		OpenSearchIndex:    cfg.OpenSearchIndex,
		MCPSSEEnabled:      true,
		MCPIPAuthEnabled:   false,
	}

	serverWrapper, err := mcpserver.NewServerWrapper(mcpConfig)
	if err != nil {
		t.Fatalf("Failed to create SDK server wrapper: %v", err)
	}

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

	if err := serverWrapper.RegisterTool("hybrid_search", hybridSearchHandler.HandleSDKToolCall); err != nil {
		t.Fatalf("Failed to register hybrid search tool: %v", err)
	}

	sdkServer := serverWrapper.GetSDKServer()
	oidcInjector := mcpserver.NewTestOIDCContextInjector("e2e-test-user", "e2e@test.example.com")

	serverMux := http.NewServeMux()
	baseGetServer := func(_ *http.Request) *mcp.Server { return sdkServer }
	streamable := mcp.NewStreamableHTTPHandler(baseGetServer, nil)
	serverMux.Handle("/", streamable)

	handler := oidcInjector(serverMux)

	serverAddr := fmt.Sprintf("127.0.0.1:%d", secretE2ETestOIDCPort)
	httpServer := &http.Server{
		Addr:         serverAddr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	serverReady := make(chan struct{}, 1)
	serverError := make(chan error, 1)

	go func() {
		ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", serverAddr)
		if err != nil {
			serverError <- fmt.Errorf("failed to listen on %s: %w", serverAddr, err)
			return
		}
		close(serverReady)
		if err := httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			serverError <- err
		}
	}()

	select {
	case <-serverReady:
	case err := <-serverError:
		t.Fatalf("OIDC-injected server failed to start: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatalf("OIDC-injected server failed to start within timeout")
	}

	serverURL := fmt.Sprintf("http://%s", serverAddr)
	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			t.Logf("Failed to shutdown OIDC server: %v", err)
		}
	}

	return serverURL, cleanup
}

func indexSecretTestDocuments(
	t *testing.T,
	osClient *opensearch.Client,
	embeddingClient *bedrock.BedrockClient,
	indexName string,
	testID string,
) (secretDocID, publicDocID string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	secretContent := "ragent-secret-e2e-canary-" + testID
	publicContent := "ragent-public-e2e-canary-" + testID

	secretEmbed, err := embeddingClient.GenerateEmbedding(ctx, secretContent)
	if err != nil {
		t.Fatalf("Failed to generate embedding for secret doc: %v", err)
	}

	publicEmbed, err := embeddingClient.GenerateEmbedding(ctx, publicContent)
	if err != nil {
		t.Fatalf("Failed to generate embedding for public doc: %v", err)
	}

	secretDocID = "secret-e2e-" + testID
	publicDocID = "public-e2e-" + testID

	secretDoc := map[string]interface{}{
		"content":        secretContent,
		"title":          "Secret E2E Test Document " + testID,
		"category":       "e2e-test",
		"secret":         true,
		"embedding":      secretEmbed,
		"knn_vector":     secretEmbed,
		"content_vector": secretEmbed,
	}

	publicDoc := map[string]interface{}{
		"content":        publicContent,
		"title":          "Public E2E Test Document " + testID,
		"category":       "e2e-test",
		"secret":         false,
		"embedding":      publicEmbed,
		"knn_vector":     publicEmbed,
		"content_vector": publicEmbed,
	}

	if err := osClient.IndexDocument(ctx, indexName, secretDocID, secretDoc); err != nil {
		t.Fatalf("Failed to index secret document: %v", err)
	}
	if err := osClient.IndexDocument(ctx, indexName, publicDocID, publicDoc); err != nil {
		t.Fatalf("Failed to index public document: %v", err)
	}

	time.Sleep(2 * time.Second)

	t.Logf("Indexed test documents: secret=%s, public=%s", secretDocID, publicDocID)
	return secretDocID, publicDocID
}

func deleteTestDocuments(t *testing.T, osClient *opensearch.Client, indexName string, docIDs ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, id := range docIDs {
		if err := osClient.DeleteDocument(ctx, indexName, id); err != nil {
			t.Logf("Warning: failed to delete test document %s: %v", id, err)
		}
	}
}

func searchResultContainsDoc(response *mcpserver.HybridSearchResponse, contentSubstring string) bool {
	if response == nil {
		return false
	}
	for _, result := range response.Results {
		if strings.Contains(result.Content, contentSubstring) || strings.Contains(result.Title, contentSubstring) {
			return true
		}
	}
	return false
}

func parseHybridSearchResponse(t *testing.T, result *mcp.CallToolResult) *mcpserver.HybridSearchResponse {
	t.Helper()

	if result == nil || len(result.Content) == 0 {
		t.Fatal("Empty tool call result")
	}

	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatal("First content is not TextContent")
	}

	var response mcpserver.HybridSearchResponse
	if err := json.Unmarshal([]byte(tc.Text), &response); err != nil {
		t.Fatalf("Failed to parse HybridSearchResponse: %v", err)
	}

	return &response
}

// TestE2E_MCPServer_SecretMetadata_AccessDenied verifies that secret documents
// are excluded from search results when the MCP server has no OIDC authentication context.
// (IP-auth only → ExcludeSecret=true → secret docs filtered out)
func TestE2E_MCPServer_SecretMetadata_AccessDenied(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	cfg, embeddingClient, osClient := setupE2EEnvironment(t)

	testID := fmt.Sprintf("%d", time.Now().UnixNano())
	secretDocID, publicDocID := indexSecretTestDocuments(t, osClient, embeddingClient, cfg.OpenSearchIndex, testID)
	t.Cleanup(func() {
		deleteTestDocuments(t, osClient, cfg.OpenSearchIndex, secretDocID, publicDocID)
	})

	mcpConfig := &config.Config{
		S3VectorRegion:     cfg.S3VectorRegion,
		OpenSearchEndpoint: cfg.OpenSearchEndpoint,
		OpenSearchRegion:   cfg.OpenSearchRegion,
		MCPServerHost:      "127.0.0.1",
		MCPServerPort:      secretE2ETestPort,
		OpenSearchIndex:    cfg.OpenSearchIndex,
		MCPSSEEnabled:      true,
		MCPIPAuthEnabled:   false,
	}

	serverWrapper, err := mcpserver.NewServerWrapper(mcpConfig)
	if err != nil {
		t.Fatalf("Failed to create SDK server wrapper: %v", err)
	}

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
	if err := serverWrapper.RegisterTool("hybrid_search", hybridSearchHandler.HandleSDKToolCall); err != nil {
		t.Fatalf("Failed to register hybrid search tool: %v", err)
	}

	if err := serverWrapper.Start(); err != nil {
		t.Fatalf("Failed to start SDK server: %v", err)
	}
	t.Cleanup(func() {
		if err := serverWrapper.Stop(); err != nil {
			t.Logf("Failed to stop server: %v", err)
		}
	})

	time.Sleep(200 * time.Millisecond)

	serverURL := fmt.Sprintf("http://127.0.0.1:%d", secretE2ETestPort)
	client := NewSDKTestClient(t, serverURL)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	secretCanary := "ragent-secret-e2e-canary-" + testID
	publicCanary := "ragent-public-e2e-canary-" + testID

	result, err := client.CallTool(ctx, "hybrid_search", map[string]interface{}{
		"query":            secretCanary,
		"max_results":      20,
		"bm25_weight":      0.9,
		"vector_weight":    0.1,
		"use_japanese_nlp": false,
		"timeout_seconds":  30,
	})
	if err != nil {
		t.Fatalf("Secret search failed: %v", err)
	}
	if result.IsError {
		t.Fatalf("Secret search returned error: %v", result.Content)
	}

	secretResponse := parseHybridSearchResponse(t, result)

	if searchResultContainsDoc(secretResponse, secretCanary) {
		t.Errorf("Secret document should NOT be returned without OIDC auth, but was found in results")
	}
	t.Logf("Secret canary query returned %d results (secret doc correctly excluded)", secretResponse.Total)

	publicResult, err := client.CallTool(ctx, "hybrid_search", map[string]interface{}{
		"query":            publicCanary,
		"max_results":      20,
		"bm25_weight":      0.9,
		"vector_weight":    0.1,
		"use_japanese_nlp": false,
		"timeout_seconds":  30,
	})
	if err != nil {
		t.Fatalf("Public search failed: %v", err)
	}
	if publicResult.IsError {
		t.Fatalf("Public search returned error: %v", publicResult.Content)
	}

	publicResponse := parseHybridSearchResponse(t, publicResult)

	if !searchResultContainsDoc(publicResponse, publicCanary) {
		t.Errorf("Public document should be returned without OIDC auth, but was NOT found in results")
	}
	t.Logf("Public canary query returned %d results (public doc correctly included)", publicResponse.Total)
}

// TestE2E_MCPServer_SecretMetadata_AccessAllowed verifies that secret documents
// are included in search results when the MCP server has OIDC authentication context.
// (OIDC-authenticated → ExcludeSecret=false → secret docs accessible)
func TestE2E_MCPServer_SecretMetadata_AccessAllowed(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	cfg, embeddingClient, osClient := setupE2EEnvironment(t)

	testID := fmt.Sprintf("%d", time.Now().UnixNano())
	secretDocID, publicDocID := indexSecretTestDocuments(t, osClient, embeddingClient, cfg.OpenSearchIndex, testID)
	t.Cleanup(func() {
		deleteTestDocuments(t, osClient, cfg.OpenSearchIndex, secretDocID, publicDocID)
	})

	serverURL, cleanup := createSDKE2EServerWithOIDC(t, cfg, osClient, embeddingClient)
	t.Cleanup(cleanup)

	client := NewSDKTestClient(t, serverURL)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	secretCanary := "ragent-secret-e2e-canary-" + testID

	result, err := client.CallTool(ctx, "hybrid_search", map[string]interface{}{
		"query":            secretCanary,
		"max_results":      20,
		"bm25_weight":      0.9,
		"vector_weight":    0.1,
		"use_japanese_nlp": false,
		"timeout_seconds":  30,
	})
	if err != nil {
		t.Fatalf("Secret search with OIDC failed: %v", err)
	}
	if result.IsError {
		t.Fatalf("Secret search with OIDC returned error: %v", result.Content)
	}

	secretResponse := parseHybridSearchResponse(t, result)

	if !searchResultContainsDoc(secretResponse, secretCanary) {
		t.Errorf("Secret document should be returned with OIDC auth, but was NOT found in results")
	}
	t.Logf("Secret canary query with OIDC returned %d results (secret doc correctly included)", secretResponse.Total)

	publicCanary := "ragent-public-e2e-canary-" + testID
	publicResult, err := client.CallTool(ctx, "hybrid_search", map[string]interface{}{
		"query":            publicCanary,
		"max_results":      20,
		"bm25_weight":      0.9,
		"vector_weight":    0.1,
		"use_japanese_nlp": false,
		"timeout_seconds":  30,
	})
	if err != nil {
		t.Fatalf("Public search with OIDC failed: %v", err)
	}
	if publicResult.IsError {
		t.Fatalf("Public search with OIDC returned error: %v", publicResult.Content)
	}

	publicResponse := parseHybridSearchResponse(t, publicResult)

	if !searchResultContainsDoc(publicResponse, publicCanary) {
		t.Errorf("Public document should also be returned with OIDC auth, but was NOT found in results")
	}
	t.Logf("Public canary query with OIDC returned %d results (public doc correctly included)", publicResponse.Total)
}

func createSDKE2EServerWithoutOIDC(
	t *testing.T,
	cfg *config.Config,
	osClient *opensearch.Client,
	embeddingClient *bedrock.BedrockClient,
) (string, func()) {
	t.Helper()

	mcpConfig := &config.Config{
		S3VectorRegion:     cfg.S3VectorRegion,
		OpenSearchEndpoint: cfg.OpenSearchEndpoint,
		OpenSearchRegion:   cfg.OpenSearchRegion,
		MCPServerHost:      "127.0.0.1",
		MCPServerPort:      secretE2ETestPort,
		OpenSearchIndex:    cfg.OpenSearchIndex,
		MCPSSEEnabled:      true,
		MCPIPAuthEnabled:   false,
	}

	serverWrapper, err := mcpserver.NewServerWrapper(mcpConfig)
	if err != nil {
		t.Fatalf("Failed to create SDK server wrapper: %v", err)
	}

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
	if err := serverWrapper.RegisterTool("hybrid_search", hybridSearchHandler.HandleSDKToolCall); err != nil {
		t.Fatalf("Failed to register hybrid search tool: %v", err)
	}

	if err := serverWrapper.Start(); err != nil {
		t.Fatalf("Failed to start SDK server: %v", err)
	}

	cleanup := func() {
		if err := serverWrapper.Stop(); err != nil {
			t.Logf("Failed to stop SDK server: %v", err)
		}
	}

	serverURL := fmt.Sprintf("http://127.0.0.1:%d", secretE2ETestPort)
	return serverURL, cleanup
}

// TestE2E_MCPServer_SecretMetadata_SecretFilterIgnored verifies that setting filters.secret
// does not override the effective secret access policy.
func TestE2E_MCPServer_SecretMetadata_SecretFilterIgnored(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	cfg, embeddingClient, osClient := setupE2EEnvironment(t)

	testID := fmt.Sprintf("%d", time.Now().UnixNano())
	secretDocID, publicDocID := indexSecretTestDocuments(t, osClient, embeddingClient, cfg.OpenSearchIndex, testID)
	t.Cleanup(func() {
		deleteTestDocuments(t, osClient, cfg.OpenSearchIndex, secretDocID, publicDocID)
	})

	secretCanary := "ragent-secret-e2e-canary-" + testID
	publicCanary := "ragent-public-e2e-canary-" + testID

	t.Run("without OIDC auth, filters.secret must not bypass policy", func(t *testing.T) {
		serverURL, cleanup := createSDKE2EServerWithoutOIDC(t, cfg, osClient, embeddingClient)
		defer cleanup()

		client := NewSDKTestClient(t, serverURL)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		maliciousFilters := map[string]interface{}{
			"secret":   "true",
			"category": "e2e-test",
		}

		secretResult, err := client.CallTool(ctx, "hybrid_search", map[string]interface{}{
			"query":            secretCanary,
			"max_results":      20,
			"bm25_weight":      0.9,
			"vector_weight":    0.1,
			"use_japanese_nlp": false,
			"filters":          maliciousFilters,
			"timeout_seconds":  30,
		})
		if err != nil {
			t.Fatalf("Secret search with filters.secret failed: %v", err)
		}
		if secretResult.IsError {
			t.Fatalf("Secret search with filters.secret returned error: %v", secretResult.Content)
		}
		secretResponse := parseHybridSearchResponse(t, secretResult)

		if searchResultContainsDoc(secretResponse, secretCanary) {
			t.Errorf("filters.secret should not permit secret document without OIDC auth")
		}

		publicResult, err := client.CallTool(ctx, "hybrid_search", map[string]interface{}{
			"query":            publicCanary,
			"max_results":      20,
			"bm25_weight":      0.9,
			"vector_weight":    0.1,
			"use_japanese_nlp": false,
			"filters":          maliciousFilters,
			"timeout_seconds":  30,
		})
		if err != nil {
			t.Fatalf("Public search with filters.secret failed: %v", err)
		}
		if publicResult.IsError {
			t.Fatalf("Public search with filters.secret returned error: %v", publicResult.Content)
		}
		publicResponse := parseHybridSearchResponse(t, publicResult)

		if !searchResultContainsDoc(publicResponse, publicCanary) {
			t.Errorf("Public document should still be accessible without OIDC auth even when filters.secret is set")
		}
		t.Logf("filters.secret ignored without OIDC: secretResults=%d publicResults=%d", secretResponse.Total, publicResponse.Total)
	})

	t.Run("with OIDC auth, filters.secret must be ignored but does not block access", func(t *testing.T) {
		serverURL, cleanup := createSDKE2EServerWithOIDC(t, cfg, osClient, embeddingClient)
		defer cleanup()

		client := NewSDKTestClient(t, serverURL)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		permissiveFilters := map[string]interface{}{
			"secret": "true",
		}

		secretResult, err := client.CallTool(ctx, "hybrid_search", map[string]interface{}{
			"query":            secretCanary,
			"max_results":      20,
			"bm25_weight":      0.9,
			"vector_weight":    0.1,
			"use_japanese_nlp": false,
			"filters":          permissiveFilters,
			"timeout_seconds":  30,
		})
		if err != nil {
			t.Fatalf("OIDC secret search with filters.secret failed: %v", err)
		}
		if secretResult.IsError {
			t.Fatalf("OIDC secret search with filters.secret returned error: %v", secretResult.Content)
		}
		secretResponse := parseHybridSearchResponse(t, secretResult)

		if !searchResultContainsDoc(secretResponse, secretCanary) {
			t.Errorf("Secret document should remain accessible with OIDC auth, even with filters.secret set")
		}
		t.Logf("OIDC mode with filters.secret includes secret: results=%d", secretResponse.Total)
	})
}
