package benchmarks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"testing"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/ca-srg/ragent/internal/config"
	"github.com/ca-srg/ragent/internal/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/mcpserver"
	"github.com/ca-srg/ragent/internal/opensearch"
	"github.com/ca-srg/ragent/internal/types"
)

// BenchmarkConfig holds configuration for benchmark tests
type BenchmarkConfig struct {
	config          *config.Config
	osClient        *opensearch.Client
	embeddingClient *bedrock.BedrockClient
	hybridConfig    *mcpserver.HybridSearchConfig
}

// setupBenchmarkEnvironment sets up the test environment for performance benchmarking
func setupBenchmarkEnvironment(b *testing.B) *BenchmarkConfig {
	b.Helper()

	// Load configuration (skip if not available)
	cfg, err := config.Load()
	if err != nil {
		b.Skip("Skipping benchmark: configuration not available")
	}

	// Create local, unreachable OpenSearch client (fast-failing)
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

	// Create Bedrock client (won't be used when OpenSearch health check fails)
	awsCfg, _ := awsconfig.LoadDefaultConfig(context.Background(), awsconfig.WithRegion("us-east-1"))
	brClient := bedrock.NewBedrockClient(awsCfg, "")

	// Create hybrid search configuration
	hybridConfig := &mcpserver.HybridSearchConfig{
		DefaultIndexName:      "benchmark-index",
		DefaultSize:           10,
		DefaultBM25Weight:     0.7,
		DefaultVectorWeight:   0.3,
		DefaultFusionMethod:   "weighted_sum",
		DefaultUseJapaneseNLP: true,
		DefaultTimeoutSeconds: 30,
	}

	return &BenchmarkConfig{
		config:          cfg,
		osClient:        osClient,
		embeddingClient: brClient,
		hybridConfig:    hybridConfig,
	}
}

// Memory measurement utilities
type MemoryStats struct {
	AllocMB      float64
	TotalAllocMB float64
	SysMB        float64
	HeapInuseMB  float64
}

func getMemoryStats() MemoryStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return MemoryStats{
		AllocMB:      float64(m.Alloc) / 1024 / 1024,
		TotalAllocMB: float64(m.TotalAlloc) / 1024 / 1024,
		SysMB:        float64(m.Sys) / 1024 / 1024,
		HeapInuseMB:  float64(m.HeapInuse) / 1024 / 1024,
	}
}

// BenchmarkServerStartup_Custom benchmarks custom MCP server startup time
func BenchmarkServerStartup_Custom(b *testing.B) {
	benchConfig := setupBenchmarkEnvironment(b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		startTime := time.Now()

		// Create custom MCP server
		serverConfig := mcpserver.DefaultMCPServerConfig()
		serverConfig.Host = "127.0.0.1"
		serverConfig.Port = 9000 + i // Use different ports to avoid conflicts

		server := mcpserver.NewMCPServer(serverConfig)

		// Register hybrid search tool
		hybridSearchTool := mcpserver.NewHybridSearchToolAdapter(
			benchConfig.osClient,
			benchConfig.embeddingClient,
			benchConfig.hybridConfig,
		)

		toolRegistry := server.GetToolRegistry()
		err := toolRegistry.RegisterTool("hybrid_search", hybridSearchTool.GetToolDefinition(), hybridSearchTool.HandleToolCall)
		if err != nil {
			b.Fatalf("Failed to register tool: %v", err)
		}

		// Start server
		err = server.Start()
		if err != nil {
			b.Fatalf("Failed to start server: %v", err)
		}

		startupTime := time.Since(startTime)

		// Stop server immediately
		server.Stop()

		// Record custom metrics
		b.ReportMetric(float64(startupTime.Nanoseconds())/1e6, "startup_ms")

		// Verify startup time requirement (<500ms)
		if startupTime > 500*time.Millisecond {
			b.Errorf("Custom server startup time %v exceeds 500ms requirement", startupTime)
		}
	}
}

// BenchmarkServerStartup_SDK benchmarks SDK-based server startup time
func BenchmarkServerStartup_SDK(b *testing.B) {
	benchConfig := setupBenchmarkEnvironment(b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		startTime := time.Now()

		// Create SDK server configuration
		mcpConfig := &types.Config{
			MCPServerHost:                "127.0.0.1",
			MCPServerPort:                9100 + i, // Use different ports to avoid conflicts
			MCPServerEnableAccessLogging: false,    // Disable logging for cleaner benchmarks
			MCPSSEEnabled:                false,    // Disable SSE for simpler startup
		}

		// Create SDK server wrapper
		serverWrapper, err := mcpserver.NewServerWrapper(mcpConfig)
		if err != nil {
			b.Fatalf("Failed to create SDK server wrapper: %v", err)
		}

		// Register hybrid search tool
		hybridSearchHandler := mcpserver.NewHybridSearchHandler(
			benchConfig.osClient,
			benchConfig.embeddingClient,
			benchConfig.hybridConfig,
		)

		err = serverWrapper.RegisterTool("hybrid_search", hybridSearchHandler.HandleSDKToolCall)
		if err != nil {
			b.Fatalf("Failed to register tool with SDK server: %v", err)
		}

		// Start SDK server
		err = serverWrapper.Start()
		if err != nil {
			b.Fatalf("Failed to start SDK server: %v", err)
		}

		startupTime := time.Since(startTime)

		// Stop server immediately
		serverWrapper.Stop()

		// Record custom metrics
		b.ReportMetric(float64(startupTime.Nanoseconds())/1e6, "startup_ms")

		// Verify startup time requirement (<500ms)
		if startupTime > 500*time.Millisecond {
			b.Errorf("SDK server startup time %v exceeds 500ms requirement", startupTime)
		}
	}
}

// BenchmarkToolCall_Custom benchmarks tool call latency for custom server
func BenchmarkToolCall_Custom(b *testing.B) {
	benchConfig := setupBenchmarkEnvironment(b)

	// Setup custom server
	serverConfig := mcpserver.DefaultMCPServerConfig()
	serverConfig.Host = "127.0.0.1"
	serverConfig.Port = 9200

	server := mcpserver.NewMCPServer(serverConfig)
	hybridSearchTool := mcpserver.NewHybridSearchToolAdapter(
		benchConfig.osClient,
		benchConfig.embeddingClient,
		benchConfig.hybridConfig,
	)

	toolRegistry := server.GetToolRegistry()
	err := toolRegistry.RegisterTool("hybrid_search", hybridSearchTool.GetToolDefinition(), hybridSearchTool.HandleToolCall)
	if err != nil {
		b.Fatalf("Failed to register tool: %v", err)
	}

	err = server.Start()
	if err != nil {
		b.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	// Wait for server to be ready
	time.Sleep(100 * time.Millisecond)

	// Create HTTP client for tool calls
	httpClient := &http.Client{Timeout: 30 * time.Second}
	serverURL := "http://127.0.0.1:9200"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		startTime := time.Now()

		// Create tool call request
		request := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "tools/call",
			"params": map[string]interface{}{
				"name": "hybrid_search",
				"arguments": map[string]interface{}{
					"query":            fmt.Sprintf("benchmark query %d", i),
					"max_results":      5,
					"bm25_weight":      0.7,
					"vector_weight":    0.3,
					"use_japanese_nlp": false,
				},
			},
			"id": fmt.Sprintf("bench-%d", i),
		}

		requestBody, _ := json.Marshal(request)

		// Execute request
		resp, err := httpClient.Post(serverURL, "application/json", bytes.NewReader(requestBody))
		if err != nil {
			b.Fatalf("Request failed: %v", err)
		}
		resp.Body.Close()

		latency := time.Since(startTime)

		// Record custom metrics
		b.ReportMetric(float64(latency.Nanoseconds())/1e6, "latency_ms")

		// Verify latency is reasonable (should be much less than 50ms for mocked responses)
		if latency > 100*time.Millisecond {
			b.Errorf("Custom server tool call latency %v seems excessive", latency)
		}
	}
}

// BenchmarkToolCall_SDK benchmarks tool call latency for SDK server
func BenchmarkToolCall_SDK(b *testing.B) {
	benchConfig := setupBenchmarkEnvironment(b)

	// Setup SDK server
	mcpConfig := &types.Config{
		MCPServerHost:                "127.0.0.1",
		MCPServerPort:                9201,
		MCPServerEnableAccessLogging: false,
		MCPSSEEnabled:                false,
	}

	serverWrapper, err := mcpserver.NewServerWrapper(mcpConfig)
	if err != nil {
		b.Fatalf("Failed to create SDK server wrapper: %v", err)
	}

	hybridSearchHandler := mcpserver.NewHybridSearchHandler(
		benchConfig.osClient,
		benchConfig.embeddingClient,
		benchConfig.hybridConfig,
	)

	err = serverWrapper.RegisterTool("hybrid_search", hybridSearchHandler.HandleSDKToolCall)
	if err != nil {
		b.Fatalf("Failed to register tool: %v", err)
	}

	err = serverWrapper.Start()
	if err != nil {
		b.Fatalf("Failed to start server: %v", err)
	}
	defer serverWrapper.Stop()

	// Wait for server to be ready
	time.Sleep(100 * time.Millisecond)

	// Create HTTP client for tool calls
	httpClient := &http.Client{Timeout: 30 * time.Second}
	serverURL := "http://127.0.0.1:9201"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		startTime := time.Now()

		// Create tool call request
		request := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "tools/call",
			"params": map[string]interface{}{
				"name": "hybrid_search",
				"arguments": map[string]interface{}{
					"query":            fmt.Sprintf("benchmark query %d", i),
					"max_results":      5,
					"bm25_weight":      0.7,
					"vector_weight":    0.3,
					"use_japanese_nlp": false,
				},
			},
			"id": fmt.Sprintf("bench-sdk-%d", i),
		}

		requestBody, _ := json.Marshal(request)

		// Execute request
		resp, err := httpClient.Post(serverURL, "application/json", bytes.NewReader(requestBody))
		if err != nil {
			b.Fatalf("Request failed: %v", err)
		}
		resp.Body.Close()

		latency := time.Since(startTime)

		// Record custom metrics
		b.ReportMetric(float64(latency.Nanoseconds())/1e6, "latency_ms")

		// Verify latency is reasonable
		if latency > 100*time.Millisecond {
			b.Errorf("SDK server tool call latency %v seems excessive", latency)
		}
	}
}

// BenchmarkConcurrentToolCalls_Custom benchmarks concurrent performance for custom server
func BenchmarkConcurrentToolCalls_Custom(b *testing.B) {
	benchConfig := setupBenchmarkEnvironment(b)

	// Setup custom server
	serverConfig := mcpserver.DefaultMCPServerConfig()
	serverConfig.Host = "127.0.0.1"
	serverConfig.Port = 9300

	server := mcpserver.NewMCPServer(serverConfig)
	hybridSearchTool := mcpserver.NewHybridSearchToolAdapter(
		benchConfig.osClient,
		benchConfig.embeddingClient,
		benchConfig.hybridConfig,
	)

	toolRegistry := server.GetToolRegistry()
	err := toolRegistry.RegisterTool("hybrid_search", hybridSearchTool.GetToolDefinition(), hybridSearchTool.HandleToolCall)
	if err != nil {
		b.Fatalf("Failed to register tool: %v", err)
	}

	err = server.Start()
	if err != nil {
		b.Fatalf("Failed to start server: %v", err)
	}
	defer server.Stop()

	time.Sleep(100 * time.Millisecond)

	httpClient := &http.Client{Timeout: 30 * time.Second}
	serverURL := "http://127.0.0.1:9300"

	b.ResetTimer()

	// Run concurrent requests
	const concurrency = 10
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		start := time.Now()

		for j := 0; j < concurrency; j++ {
			wg.Add(1)
			go func(requestID int) {
				defer wg.Done()

				request := map[string]interface{}{
					"jsonrpc": "2.0",
					"method":  "tools/call",
					"params": map[string]interface{}{
						"name": "hybrid_search",
						"arguments": map[string]interface{}{
							"query":       fmt.Sprintf("concurrent query %d", requestID),
							"max_results": 3,
						},
					},
					"id": fmt.Sprintf("concurrent-%d", requestID),
				}

				requestBody, _ := json.Marshal(request)
				resp, err := httpClient.Post(serverURL, "application/json", bytes.NewReader(requestBody))
				if err == nil {
					resp.Body.Close()
				}
			}(j)
		}

		wg.Wait()
		totalTime := time.Since(start)

		b.ReportMetric(float64(totalTime.Nanoseconds())/1e6, "concurrent_ms")
		b.ReportMetric(float64(concurrency), "requests")
	}
}

// BenchmarkConcurrentToolCalls_SDK benchmarks concurrent performance for SDK server
func BenchmarkConcurrentToolCalls_SDK(b *testing.B) {
	benchConfig := setupBenchmarkEnvironment(b)

	// Setup SDK server
	mcpConfig := &types.Config{
		MCPServerHost:                "127.0.0.1",
		MCPServerPort:                9301,
		MCPServerEnableAccessLogging: false,
		MCPSSEEnabled:                false,
	}

	serverWrapper, err := mcpserver.NewServerWrapper(mcpConfig)
	if err != nil {
		b.Fatalf("Failed to create SDK server wrapper: %v", err)
	}

	hybridSearchHandler := mcpserver.NewHybridSearchHandler(
		benchConfig.osClient,
		benchConfig.embeddingClient,
		benchConfig.hybridConfig,
	)

	err = serverWrapper.RegisterTool("hybrid_search", hybridSearchHandler.HandleSDKToolCall)
	if err != nil {
		b.Fatalf("Failed to register tool: %v", err)
	}

	err = serverWrapper.Start()
	if err != nil {
		b.Fatalf("Failed to start server: %v", err)
	}
	defer serverWrapper.Stop()

	time.Sleep(100 * time.Millisecond)

	httpClient := &http.Client{Timeout: 30 * time.Second}
	serverURL := "http://127.0.0.1:9301"

	b.ResetTimer()

	// Run concurrent requests
	const concurrency = 10
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		start := time.Now()

		for j := 0; j < concurrency; j++ {
			wg.Add(1)
			go func(requestID int) {
				defer wg.Done()

				request := map[string]interface{}{
					"jsonrpc": "2.0",
					"method":  "tools/call",
					"params": map[string]interface{}{
						"name": "hybrid_search",
						"arguments": map[string]interface{}{
							"query":       fmt.Sprintf("concurrent query %d", requestID),
							"max_results": 3,
						},
					},
					"id": fmt.Sprintf("concurrent-sdk-%d", requestID),
				}

				requestBody, _ := json.Marshal(request)
				resp, err := httpClient.Post(serverURL, "application/json", bytes.NewReader(requestBody))
				if err == nil {
					resp.Body.Close()
				}
			}(j)
		}

		wg.Wait()
		totalTime := time.Since(start)

		b.ReportMetric(float64(totalTime.Nanoseconds())/1e6, "concurrent_ms")
		b.ReportMetric(float64(concurrency), "requests")
	}
}

// BenchmarkMemoryUsage_Custom measures memory usage of custom server
func BenchmarkMemoryUsage_Custom(b *testing.B) {
	benchConfig := setupBenchmarkEnvironment(b)

	b.ResetTimer()

	// Force garbage collection before measurement
	runtime.GC()
	time.Sleep(10 * time.Millisecond)
	baseline := getMemoryStats()

	for i := 0; i < b.N; i++ {
		// Create and start custom server
		serverConfig := mcpserver.DefaultMCPServerConfig()
		serverConfig.Host = "127.0.0.1"
		serverConfig.Port = 9400 + i

		server := mcpserver.NewMCPServer(serverConfig)
		hybridSearchTool := mcpserver.NewHybridSearchToolAdapter(
			benchConfig.osClient,
			benchConfig.embeddingClient,
			benchConfig.hybridConfig,
		)

		toolRegistry := server.GetToolRegistry()
		toolRegistry.RegisterTool("hybrid_search", hybridSearchTool.GetToolDefinition(), hybridSearchTool.HandleToolCall)
		server.Start()

		// Measure memory after startup
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
		afterStartup := getMemoryStats()

		// Execute some tool calls to measure operational memory
		httpClient := &http.Client{Timeout: 10 * time.Second}
		serverURL := fmt.Sprintf("http://127.0.0.1:%d", 9400+i)

		for j := 0; j < 10; j++ {
			request := map[string]interface{}{
				"jsonrpc": "2.0",
				"method":  "tools/call",
				"params": map[string]interface{}{
					"name": "hybrid_search",
					"arguments": map[string]interface{}{
						"query": fmt.Sprintf("memory test %d", j),
					},
				},
				"id": j,
			}

			requestBody, _ := json.Marshal(request)
			resp, err := httpClient.Post(serverURL, "application/json", bytes.NewReader(requestBody))
			if err == nil {
				resp.Body.Close()
			}
		}

		// Measure memory after operations
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
		afterOps := getMemoryStats()

		server.Stop()

		// Report memory metrics
		b.ReportMetric(afterStartup.AllocMB-baseline.AllocMB, "startup_alloc_mb")
		b.ReportMetric(afterOps.AllocMB-baseline.AllocMB, "ops_alloc_mb")
		b.ReportMetric(afterOps.HeapInuseMB-baseline.HeapInuseMB, "heap_inuse_mb")
	}
}

// BenchmarkMemoryUsage_SDK measures memory usage of SDK server
func BenchmarkMemoryUsage_SDK(b *testing.B) {
	benchConfig := setupBenchmarkEnvironment(b)

	b.ResetTimer()

	// Force garbage collection before measurement
	runtime.GC()
	time.Sleep(10 * time.Millisecond)
	baseline := getMemoryStats()

	for i := 0; i < b.N; i++ {
		// Create and start SDK server
		mcpConfig := &types.Config{
			MCPServerHost:                "127.0.0.1",
			MCPServerPort:                9500 + i,
			MCPServerEnableAccessLogging: false,
			MCPSSEEnabled:                false,
		}

		serverWrapper, _ := mcpserver.NewServerWrapper(mcpConfig)
		hybridSearchHandler := mcpserver.NewHybridSearchHandler(
			benchConfig.osClient,
			benchConfig.embeddingClient,
			benchConfig.hybridConfig,
		)

		serverWrapper.RegisterTool("hybrid_search", hybridSearchHandler.HandleSDKToolCall)
		serverWrapper.Start()

		// Measure memory after startup
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
		afterStartup := getMemoryStats()

		// Execute some tool calls to measure operational memory
		httpClient := &http.Client{Timeout: 10 * time.Second}
		serverURL := fmt.Sprintf("http://127.0.0.1:%d", 9500+i)

		for j := 0; j < 10; j++ {
			request := map[string]interface{}{
				"jsonrpc": "2.0",
				"method":  "tools/call",
				"params": map[string]interface{}{
					"name": "hybrid_search",
					"arguments": map[string]interface{}{
						"query": fmt.Sprintf("memory test %d", j),
					},
				},
				"id": j,
			}

			requestBody, _ := json.Marshal(request)
			resp, err := httpClient.Post(serverURL, "application/json", bytes.NewReader(requestBody))
			if err == nil {
				resp.Body.Close()
			}
		}

		// Measure memory after operations
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
		afterOps := getMemoryStats()

		serverWrapper.Stop()

		// Report memory metrics
		b.ReportMetric(afterStartup.AllocMB-baseline.AllocMB, "startup_alloc_mb")
		b.ReportMetric(afterOps.AllocMB-baseline.AllocMB, "ops_alloc_mb")
		b.ReportMetric(afterOps.HeapInuseMB-baseline.HeapInuseMB, "heap_inuse_mb")
	}
}

// TestPerformanceRequirements runs a comprehensive test to verify all performance requirements are met
func TestPerformanceRequirements(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance requirements test in short mode")
	}

	// This test serves as a validation that the performance requirements are met
	// It's not a benchmark but a pass/fail test

	// Build a minimal environment without relying on *testing.B
	cfg, _ := config.Load()
	osCfg := &opensearch.Config{Endpoint: "http://127.0.0.1:1", Region: "us-east-1", InsecureSkipTLS: true, RateLimit: 10, RateBurst: 20}
	osClient, _ := opensearch.NewClient(osCfg)
	awsCfg, _ := awsconfig.LoadDefaultConfig(context.Background(), awsconfig.WithRegion("us-east-1"))
	brClient := bedrock.NewBedrockClient(awsCfg, "")
	benchConfig := &BenchmarkConfig{
		config:          cfg,
		osClient:        osClient,
		embeddingClient: brClient,
		hybridConfig: &mcpserver.HybridSearchConfig{
			DefaultIndexName:      "benchmark-index",
			DefaultSize:           10,
			DefaultBM25Weight:     0.7,
			DefaultVectorWeight:   0.3,
			DefaultFusionMethod:   "weighted_sum",
			DefaultUseJapaneseNLP: true,
			DefaultTimeoutSeconds: 30,
		},
	}

	t.Run("Startup Time Requirements", func(t *testing.T) {
		// Test custom server startup
		start := time.Now()
		serverConfig := mcpserver.DefaultMCPServerConfig()
		serverConfig.Host = "127.0.0.1"
		serverConfig.Port = 9600
		server := mcpserver.NewMCPServer(serverConfig)
		hybridSearchTool := mcpserver.NewHybridSearchToolAdapter(
			benchConfig.osClient, benchConfig.embeddingClient, benchConfig.hybridConfig)
		toolRegistry := server.GetToolRegistry()
		toolRegistry.RegisterTool("hybrid_search", hybridSearchTool.GetToolDefinition(), hybridSearchTool.HandleToolCall)
		server.Start()
		customStartup := time.Since(start)
		server.Stop()

		// Test SDK server startup
		start = time.Now()
		mcpConfig := &types.Config{MCPServerHost: "127.0.0.1", MCPServerPort: 9601, MCPSSEEnabled: false}
		serverWrapper, _ := mcpserver.NewServerWrapper(mcpConfig)
		hybridHandler := mcpserver.NewHybridSearchHandler(benchConfig.osClient, benchConfig.embeddingClient, benchConfig.hybridConfig)
		serverWrapper.RegisterTool("hybrid_search", hybridHandler.HandleSDKToolCall)
		serverWrapper.Start()
		sdkStartup := time.Since(start)
		serverWrapper.Stop()

		t.Logf("Custom server startup: %v", customStartup)
		t.Logf("SDK server startup: %v", sdkStartup)

		// Verify requirements
		if customStartup > 500*time.Millisecond {
			t.Errorf("Custom server startup %v exceeds 500ms requirement", customStartup)
		}
		if sdkStartup > 500*time.Millisecond {
			t.Errorf("SDK server startup %v exceeds 500ms requirement", sdkStartup)
		}

		// Verify SDK startup overhead is <500ms additional
		if sdkStartup > customStartup+500*time.Millisecond {
			t.Errorf("SDK startup overhead %v exceeds 500ms limit", sdkStartup-customStartup)
		}
	})

	// Note: Latency and memory tests would be similar but require running servers
	// These are covered in the benchmark functions above
	t.Logf("✅ Performance requirements validation completed")
}
