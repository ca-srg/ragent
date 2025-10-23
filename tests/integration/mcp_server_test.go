package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/ca-srg/ragent/internal/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/mcpserver"
	"github.com/ca-srg/ragent/internal/opensearch"
	"github.com/ca-srg/ragent/internal/types"
)

// Test server setup
func createTestMCPServer(t testing.TB) (*mcpserver.MCPServer, *mcpserver.HybridSearchToolAdapter) {
	// Minimal real clients that fail fast locally
	osCfg := &opensearch.Config{Endpoint: "http://127.0.0.1:1", Region: "us-east-1", InsecureSkipTLS: true}
	osClient, _ := opensearch.NewClient(osCfg)
	awsCfg, _ := awsconfig.LoadDefaultConfig(context.Background(), awsconfig.WithRegion("us-east-1"))
	brClient := bedrock.NewBedrockClient(awsCfg, "")

	config := &mcpserver.HybridSearchConfig{
		DefaultIndexName:      "test-index",
		DefaultSize:           10,
		DefaultBM25Weight:     0.5,
		DefaultVectorWeight:   0.5,
		DefaultFusionMethod:   "weighted_sum",
		DefaultUseJapaneseNLP: true,
		DefaultTimeoutSeconds: 30,
	}

	// Create hybrid search tool adapter
	hybridSearchTool := mcpserver.NewHybridSearchToolAdapter(osClient, brClient, config, nil)

	// Create MCP server
	serverConfig := mcpserver.DefaultMCPServerConfig()
	serverConfig.Host = "127.0.0.1"
	serverConfig.Port = 8890 // Fixed test port to simplify HTTP requests

	server := mcpserver.NewMCPServer(serverConfig)

	// Register the hybrid search tool
	toolRegistry := server.GetToolRegistry()
	err := toolRegistry.RegisterTool("hybrid_search", hybridSearchTool.GetToolDefinition(), hybridSearchTool.HandleToolCall)
	if err != nil {
		t.Fatalf("Failed to register hybrid search tool: %v", err)
	}

	return server, hybridSearchTool
}

// Helper function to create HTTP request
func createJSONRPCRequest(method string, params interface{}, id interface{}) *http.Request {
	request := types.LegacyMCPToolRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      id,
	}

	body, _ := json.Marshal(request)
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// (removed) parseJSONRPCResponse was unused

func TestMCPServer_JSONRPCProtocolCompliance(t *testing.T) {
	server, _ := createTestMCPServer(t)
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start test server: %v", err)
	}
	defer func() {
		if err := server.Stop(); err != nil {
			t.Logf("Failed to stop server: %v", err)
		}
	}()

	tests := []struct {
		name               string
		method             string
		body               string
		contentType        string
		expectedStatusCode int
		expectedErrorCode  *int
	}{
		{
			name:               "valid JSON-RPC 2.0 request",
			method:             "POST",
			body:               `{"jsonrpc":"2.0","method":"tools/list","id":1}`,
			contentType:        "application/json",
			expectedStatusCode: 200,
			expectedErrorCode:  nil,
		},
		{
			name:               "missing jsonrpc version",
			method:             "POST",
			body:               `{"method":"tools/list","id":1}`,
			contentType:        "application/json",
			expectedStatusCode: 200,
			expectedErrorCode:  &[]int{types.MCPErrorInvalidRequest}[0],
		},
		{
			name:               "wrong jsonrpc version",
			method:             "POST",
			body:               `{"jsonrpc":"1.0","method":"tools/list","id":1}`,
			contentType:        "application/json",
			expectedStatusCode: 200,
			expectedErrorCode:  &[]int{types.MCPErrorInvalidRequest}[0],
		},
		{
			name:               "missing method",
			method:             "POST",
			body:               `{"jsonrpc":"2.0","id":1}`,
			contentType:        "application/json",
			expectedStatusCode: 200,
			expectedErrorCode:  &[]int{types.MCPErrorInvalidRequest}[0],
		},
		{
			name:               "invalid JSON",
			method:             "POST",
			body:               `{"jsonrpc":"2.0","method":"tools/list","id":1`,
			contentType:        "application/json",
			expectedStatusCode: 200,
			expectedErrorCode:  &[]int{types.MCPErrorParseError}[0],
		},
		{
			name:               "wrong HTTP method",
			method:             "GET",
			body:               `{"jsonrpc":"2.0","method":"tools/list","id":1}`,
			contentType:        "application/json",
			expectedStatusCode: 200,
			expectedErrorCode:  &[]int{types.MCPErrorMethodNotFound}[0],
		},
		{
			name:               "wrong content type",
			method:             "POST",
			body:               `{"jsonrpc":"2.0","method":"tools/list","id":1}`,
			contentType:        "text/plain",
			expectedStatusCode: 200,
			expectedErrorCode:  &[]int{types.MCPErrorInvalidRequest}[0],
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := fmt.Sprintf("http://%s:%d", server.GetConfig().Host, server.GetConfig().Port)
			req, _ := http.NewRequest(tt.method, url, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", tt.contentType)
			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("HTTP request failed: %v", err)
			}
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Logf("Failed to close response body: %v", err)
				}
			}()

			if resp.StatusCode != tt.expectedStatusCode {
				t.Errorf("Expected status code %d, got %d", tt.expectedStatusCode, resp.StatusCode)
			}

			var response map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
				t.Fatalf("Failed to parse response: %v", err)
			}

			// Check for expected error
			if tt.expectedErrorCode != nil {
				errorObj, exists := response["error"]
				if !exists {
					t.Error("Expected error in response but found none")
					return
				}

				errorMap, ok := errorObj.(map[string]interface{})
				if !ok {
					t.Error("Error object is not a map")
					return
				}

				code, exists := errorMap["code"]
				if !exists {
					t.Error("Error object missing code field")
					return
				}

				codeFloat, ok := code.(float64)
				if !ok {
					t.Error("Error code is not a number")
					return
				}

				if int(codeFloat) != *tt.expectedErrorCode {
					t.Errorf("Expected error code %d, got %d", *tt.expectedErrorCode, int(codeFloat))
				}
			} else {
				// Should not have error for valid requests
				if _, exists := response["error"]; exists {
					t.Errorf("Unexpected error in response: %v", response["error"])
				}
			}

			// All responses should have jsonrpc field
			if jsonrpcVersion, exists := response["jsonrpc"]; !exists || jsonrpcVersion != "2.0" {
				t.Error("Response should have jsonrpc: 2.0")
			}
		})
	}
}

func TestMCPServer_ToolsList(t *testing.T) {
	server, _ := createTestMCPServer(t)
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start test server: %v", err)
	}
	defer func() {
		if err := server.Stop(); err != nil {
			t.Logf("Failed to stop server: %v", err)
		}
	}()

	req := createJSONRPCRequest("tools/list", nil, "test-1")
	url := fmt.Sprintf("http://%s:%d", server.GetConfig().Host, server.GetConfig().Port)
	client := &http.Client{Timeout: 5 * time.Second}
	httpReq, _ := http.NewRequest("POST", url, req.Body)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(httpReq)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("Failed to close response body: %v", err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var response types.MCPToolResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Check response structure
	if response.JSONRPC != "2.0" {
		t.Error("Response should have jsonrpc: 2.0")
	}

	if response.ID != "test-1" {
		t.Errorf("Expected ID 'test-1', got %v", response.ID)
	}

	if response.Error != nil {
		t.Errorf("Unexpected error: %v", response.Error)
	}

	// Check result structure
	if response.Result == nil {
		t.Error("Result should not be nil for tools/list")
		return
	}

	var listResult types.MCPToolListResult
	b, _ := json.Marshal(response.Result)
	if err := json.Unmarshal(b, &listResult); err != nil {
		t.Fatalf("Failed to parse tools list result: %v", err)
	}

	// Should have at least the hybrid_search tool
	if len(listResult.Tools) == 0 {
		t.Error("Expected at least one tool in the list")
	}

	// Check that hybrid_search tool is present
	found := false
	for _, tool := range listResult.Tools {
		if tool.Name == "hybrid_search" {
			found = true
			if tool.Description == "" {
				t.Error("hybrid_search tool should have a description")
			}
			if tool.InputSchema == nil {
				t.Error("hybrid_search tool should have input schema")
			}
		}
	}

	if !found {
		t.Error("hybrid_search tool should be in the tools list")
	}
}

func TestMCPServer_ToolCall_ValidRequest(t *testing.T) {
	server, _ := createTestMCPServer(t)

	params := types.MCPToolCallParams{
		Name: "hybrid_search",
		Arguments: map[string]interface{}{
			"query": "test query",
			"top_k": 5,
		},
	}

	req := createJSONRPCRequest("tools/call", params, "test-2")
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start test server: %v", err)
	}
	defer func() {
		if err := server.Stop(); err != nil {
			t.Logf("Failed to stop server: %v", err)
		}
	}()

	url := fmt.Sprintf("http://%s:%d", server.GetConfig().Host, server.GetConfig().Port)
	httpResp, err := http.Post(url, "application/json", req.Body)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer func() {
		if err := httpResp.Body.Close(); err != nil {
			t.Logf("Failed to close response body: %v", err)
		}
	}()

	if httpResp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", httpResp.StatusCode)
	}

	var response types.MCPToolResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Check response structure
	if response.JSONRPC != "2.0" {
		t.Error("Response should have jsonrpc: 2.0")
	}

	if response.ID != "test-2" {
		t.Errorf("Expected ID 'test-2', got %v", response.ID)
	}

	// Should get some result (even if it's an error due to mock limitations)
	if response.Result == nil && response.Error == nil {
		t.Error("Response should have either result or error")
	}

	// If there's a result, it should be from the hybrid search tool
	if response.Result != nil {
		// This validates that the tool call was routed correctly
		t.Log("Tool call executed successfully")
	}

	// If there's an error, it should be a tool execution error, not a protocol error
	if response.Error != nil {
		if response.Error.Code == types.MCPErrorMethodNotFound ||
			response.Error.Code == types.MCPErrorInvalidRequest ||
			response.Error.Code == types.MCPErrorParseError {
			t.Errorf("Should not get protocol error for valid tool call: %v", response.Error)
		}
	}
}

func TestMCPServer_ToolCall_InvalidRequests(t *testing.T) {
	server, _ := createTestMCPServer(t)

	tests := []struct {
		name              string
		params            interface{}
		expectedErrorCode int
	}{
		{
			name:              "missing tool name",
			params:            map[string]interface{}{"arguments": map[string]interface{}{"query": "test"}},
			expectedErrorCode: types.MCPErrorInvalidParams,
		},
		{
			name:              "empty tool name",
			params:            types.MCPToolCallParams{Name: "", Arguments: map[string]interface{}{"query": "test"}},
			expectedErrorCode: types.MCPErrorInvalidParams,
		},
		{
			name:              "non-existent tool",
			params:            types.MCPToolCallParams{Name: "non_existent_tool", Arguments: map[string]interface{}{"query": "test"}},
			expectedErrorCode: types.MCPErrorInternalError,
		},
		{
			name:              "invalid parameters format",
			params:            "invalid_params",
			expectedErrorCode: types.MCPErrorInvalidParams,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := createJSONRPCRequest("tools/call", tt.params, fmt.Sprintf("test-%s", tt.name))
			if err := server.Start(); err != nil {
				t.Fatalf("Failed to start test server: %v", err)
			}
			defer func() {
				if err := server.Stop(); err != nil {
					t.Logf("Failed to stop server: %v", err)
				}
			}()

			url := fmt.Sprintf("http://%s:%d", server.GetConfig().Host, server.GetConfig().Port)
			httpResp, err := http.Post(url, "application/json", req.Body)
			if err != nil {
				t.Fatalf("HTTP request failed: %v", err)
			}
			defer func() {
				if err := httpResp.Body.Close(); err != nil {
					t.Logf("Failed to close response body: %v", err)
				}
			}()

			if httpResp.StatusCode != http.StatusOK {
				t.Errorf("Expected status 200, got %d", httpResp.StatusCode)
			}

			var response types.MCPToolResponse
			if err := json.NewDecoder(httpResp.Body).Decode(&response); err != nil {
				t.Fatalf("Failed to parse response: %v", err)
			}

			if response.Error == nil {
				t.Error("Expected error for invalid tool call")
				return
			}

			if response.Error.Code != tt.expectedErrorCode {
				t.Errorf("Expected error code %d, got %d", tt.expectedErrorCode, response.Error.Code)
			}
		})
	}
}

func TestMCPServer_UnknownMethod(t *testing.T) {
	server, _ := createTestMCPServer(t)

	req := createJSONRPCRequest("unknown/method", nil, "test-unknown")
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start test server: %v", err)
	}
	defer func() {
		if err := server.Stop(); err != nil {
			t.Logf("Failed to stop server: %v", err)
		}
	}()
	url := fmt.Sprintf("http://%s:%d", server.GetConfig().Host, server.GetConfig().Port)
	resp, err := http.Post(url, "application/json", req.Body)
	if err != nil {
		t.Fatalf("HTTP request failed: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("Failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var response types.MCPToolResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.Error == nil {
		t.Error("Expected error for unknown method")
		return
	}

	if response.Error.Code != types.MCPErrorMethodNotFound {
		t.Errorf("Expected error code %d for unknown method, got %d", types.MCPErrorMethodNotFound, response.Error.Code)
	}

	if !strings.Contains(response.Error.Message, "unknown/method") {
		t.Error("Error message should mention the unknown method")
	}
}

func TestMCPServer_IPAuthenticationIntegration(t *testing.T) {
	server, _ := createTestMCPServer(t)

	// Create IP auth middleware with test IPs
	allowedIPs := []string{"127.0.0.1", "192.168.1.100"}
	ipAuth, _ := mcpserver.NewIPAuthMiddleware(allowedIPs, true)
	server.SetIPAuthMiddleware(ipAuth)

	tests := []struct {
		name           string
		remoteAddr     string
		expectedStatus int
		expectError    bool
	}{
		{
			name:           "allowed IP",
			remoteAddr:     "127.0.0.1:12345",
			expectedStatus: 200,
			expectError:    false,
		},
		{
			name:           "allowed IP 2",
			remoteAddr:     "192.168.1.100:54321",
			expectedStatus: 200,
			expectError:    false,
		},
		{
			name:           "disallowed IP",
			remoteAddr:     "10.0.0.1:12345",
			expectedStatus: 403,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := server.Start(); err != nil {
				t.Fatalf("Failed to start test server: %v", err)
			}
			defer func() {
				if err := server.Stop(); err != nil {
					t.Logf("Failed to stop server: %v", err)
				}
			}()

			reqHTTP := createJSONRPCRequest("tools/list", nil, "auth-test")
			url := fmt.Sprintf("http://%s:%d", server.GetConfig().Host, server.GetConfig().Port)
			httpReq, _ := http.NewRequest("POST", url, reqHTTP.Body)
			httpReq.Header.Set("Content-Type", "application/json")
			// Simulate client IP via X-Forwarded-For which middleware respects first
			clientIP := strings.Split(tt.remoteAddr, ":")[0]
			httpReq.Header.Set("X-Forwarded-For", clientIP)
			httpClient := &http.Client{Timeout: 5 * time.Second}
			resp, err := httpClient.Do(httpReq)
			if err != nil {
				t.Fatalf("HTTP request failed: %v", err)
			}
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Logf("Failed to close response body: %v", err)
				}
			}()

			if resp.StatusCode != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
			}

			if !tt.expectError && resp.StatusCode == 200 {
				var response map[string]interface{}
				if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
					t.Fatalf("Failed to parse JSON response: %v", err)
				}
				if response["jsonrpc"] != "2.0" {
					t.Error("Response should have jsonrpc: 2.0")
				}
			}
		})
	}
}

func TestMCPServer_ConcurrentRequests(t *testing.T) {
	server, _ := createTestMCPServer(t)
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start test server: %v", err)
	}
	defer func() {
		if err := server.Stop(); err != nil {
			t.Logf("Failed to stop server: %v", err)
		}
	}()

	const numRequests = 10
	results := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(id int) {
			req := createJSONRPCRequest("tools/list", nil, fmt.Sprintf("concurrent-%d", id))
			url := fmt.Sprintf("http://%s:%d", server.GetConfig().Host, server.GetConfig().Port)
			resp, err := http.Post(url, "application/json", req.Body)
			if err != nil {
				results <- fmt.Errorf("request %d: http error: %v", id, err)
				return
			}
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Logf("Failed to close response body: %v", err)
				}
			}()

			if resp.StatusCode != http.StatusOK {
				results <- fmt.Errorf("request %d: expected status 200, got %d", id, resp.StatusCode)
				return
			}

			var response types.MCPToolResponse
			if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
				results <- fmt.Errorf("request %d: failed to parse response: %v", id, err)
				return
			}

			if response.JSONRPC != "2.0" {
				results <- fmt.Errorf("request %d: invalid jsonrpc version", id)
				return
			}

			if response.ID != fmt.Sprintf("concurrent-%d", id) {
				results <- fmt.Errorf("request %d: wrong response ID", id)
				return
			}

			results <- nil
		}(i)
	}

	// Wait for all requests to complete
	for i := 0; i < numRequests; i++ {
		select {
		case err := <-results:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("Request timed out")
		}
	}
}

func TestMCPServer_HealthCheckEndpoint(t *testing.T) {
	server, _ := createTestMCPServer(t)

	// Test health check endpoint if it exists
	if err := server.Start(); err != nil {
		t.Fatalf("Failed to start test server: %v", err)
	}
	defer func() {
		if err := server.Stop(); err != nil {
			t.Logf("Failed to stop server: %v", err)
		}
	}()
	url := fmt.Sprintf("http://%s:%d/health", server.GetConfig().Host, server.GetConfig().Port)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("Health check request failed: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("Failed to close response body: %v", err)
		}
	}()
	if resp.StatusCode != 404 && resp.StatusCode != 200 {
		t.Logf("Health check endpoint returned status: %d", resp.StatusCode)
	}
}

// Benchmark test for MCP server performance
func BenchmarkMCPServer_ToolsList(b *testing.B) {
	server, _ := createTestMCPServer(b)

	// request prepared per iteration below

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := server.Start(); err != nil {
			b.Fatalf("Failed to start server: %v", err)
		}
		url := fmt.Sprintf("http://%s:%d", server.GetConfig().Host, server.GetConfig().Port)
		req := createJSONRPCRequest("tools/list", nil, "bench")
		resp, err := http.Post(url, "application/json", req.Body)
		if err == nil {
			if cerr := resp.Body.Close(); cerr != nil {
				b.Logf("Failed to close response body: %v", cerr)
			}
		}
		if err := server.Stop(); err != nil {
			b.Logf("Failed to stop server: %v", err)
		}
	}
}

func BenchmarkMCPServer_ToolCall(b *testing.B) {
	server, _ := createTestMCPServer(b)

	params := types.MCPToolCallParams{
		Name: "hybrid_search",
		Arguments: map[string]interface{}{
			"query": "benchmark query",
		},
	}

	// request prepared per iteration below

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := server.Start(); err != nil {
			b.Fatalf("Failed to start server: %v", err)
		}
		url := fmt.Sprintf("http://%s:%d", server.GetConfig().Host, server.GetConfig().Port)
		req := createJSONRPCRequest("tools/call", params, "bench")
		resp, err := http.Post(url, "application/json", req.Body)
		if err == nil {
			if cerr := resp.Body.Close(); cerr != nil {
				b.Logf("Failed to close response body: %v", cerr)
			}
		}
		if err := server.Stop(); err != nil {
			b.Logf("Failed to stop server: %v", err)
		}
	}
}
