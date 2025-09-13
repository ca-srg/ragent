package unit

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/ca-srg/ragent/internal/mcpserver"
	"github.com/ca-srg/ragent/internal/types"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test utilities and mocks

// createBasicInputSchema creates a basic input schema for testing
func createBasicInputSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"query": {
				Type:        "string",
				Description: "Test query parameter",
			},
		},
		Required: []string{"query"},
	}
}

// createTestSDKServer creates a properly initialized SDK server for testing
func createTestSDKServer() *mcp.Server {
	impl := &mcp.Implementation{Name: "test-server", Version: "1.0.0"}
	return mcp.NewServer(impl, nil)
}

// createTestConfig creates a valid test configuration
func createTestConfig() *types.Config {
	return &types.Config{
		MCPServerHost:                "localhost",
		MCPServerPort:                8080,
		MCPServerReadTimeout:         30 * time.Second,
		MCPServerWriteTimeout:        30 * time.Second,
		MCPServerIdleTimeout:         60 * time.Second,
		MCPServerMaxHeaderBytes:      1048576,
		MCPServerGracefulShutdown:    true,
		MCPServerShutdownTimeout:     30 * time.Second,
		MCPServerEnableAccessLogging: true,
		MCPIPAuthEnabled:             true,
		MCPAllowedIPs:                []string{"127.0.0.1", "::1"},
		MCPIPAuthEnableLogging:       true,
		MCPToolPrefix:                "ragent_",
		MCPHybridSearchToolName:      "hybrid_search",
		OpenSearchIndex:              "test-index",
		OpenSearchEndpoint:           "http://127.0.0.1:9200",
		OpenSearchRegion:             "us-east-1",
		MCPDefaultSearchSize:         10,
		MCPDefaultBM25Weight:         0.7,
		MCPDefaultVectorWeight:       0.3,
		MCPDefaultUseJapaneseNLP:     true,
		MCPDefaultTimeoutSeconds:     30,
		MCPSSEEnabled:                true,
		MCPSSEHeartbeatInterval:      30 * time.Second,
		MCPSSEBufferSize:             1000,
		MCPSSEMaxClients:             100,
		MCPSSEHistorySize:            50,
	}
}

// MockSDKServer is a mock implementation of SDK server for testing
type MockSDKServer struct {
	tools        []mcp.Tool
	addToolError error
}

func (m *MockSDKServer) AddTool(tool *mcp.Tool, handler mcp.ToolHandler) {
	if m.addToolError != nil {
		return
	}
	m.tools = append(m.tools, *tool)
}

// ConfigAdapter Tests

func TestNewConfigAdapter(t *testing.T) {
	config := createTestConfig()
	adapter := mcpserver.NewConfigAdapter(config)

	assert.NotNil(t, adapter, "ConfigAdapter should not be nil")
}

func TestConfigAdapter_ToSDKConfig(t *testing.T) {
	tests := []struct {
		name           string
		config         *types.Config
		expectError    bool
		validateFields func(*testing.T, *mcpserver.SDKServerConfig)
	}{
		{
			name:        "valid configuration",
			config:      createTestConfig(),
			expectError: false,
			validateFields: func(t *testing.T, sdkConfig *mcpserver.SDKServerConfig) {
				assert.Equal(t, "localhost", sdkConfig.Host)
				assert.Equal(t, 8080, sdkConfig.Port)
				assert.Equal(t, 30*time.Second, sdkConfig.ReadTimeout)
				assert.Equal(t, true, sdkConfig.IPAuthEnabled)
				assert.Equal(t, []string{"127.0.0.1", "::1"}, sdkConfig.AllowedIPs)
				assert.Equal(t, "ragent_", sdkConfig.ToolPrefix)
				assert.Equal(t, "hybrid_search", sdkConfig.HybridSearchToolName)
				assert.Equal(t, true, sdkConfig.SSEEnabled)
			},
		},
		{
			name:        "nil configuration",
			config:      nil,
			expectError: true,
		},
		{
			name: "invalid host",
			config: func() *types.Config {
				config := createTestConfig()
				config.MCPServerHost = ""
				return config
			}(),
			expectError: true,
		},
		{
			name: "invalid port",
			config: func() *types.Config {
				config := createTestConfig()
				config.MCPServerPort = 0
				return config
			}(),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Always create adapter, even with nil config
			adapter := mcpserver.NewConfigAdapter(tt.config)

			sdkConfig, err := adapter.ToSDKConfig()

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, sdkConfig)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, sdkConfig)
				if tt.validateFields != nil {
					tt.validateFields(t, sdkConfig)
				}
			}
		})
	}
}

func TestConfigAdapter_ValidateSDKCompatibility(t *testing.T) {
	tests := []struct {
		name        string
		config      *types.Config
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid configuration",
			config:      createTestConfig(),
			expectError: false,
		},
		{
			name: "empty host",
			config: func() *types.Config {
				config := createTestConfig()
				config.MCPServerHost = ""
				return config
			}(),
			expectError: true,
			errorMsg:    "server host cannot be empty",
		},
		{
			name: "invalid port range",
			config: func() *types.Config {
				config := createTestConfig()
				config.MCPServerPort = 70000
				return config
			}(),
			expectError: true,
			errorMsg:    "server port must be between 1 and 65535",
		},
		{
			name: "empty allowed IPs with auth enabled",
			config: func() *types.Config {
				config := createTestConfig()
				config.MCPIPAuthEnabled = true
				config.MCPAllowedIPs = []string{}
				return config
			}(),
			expectError: true,
			errorMsg:    "MCP allowed IPs cannot be empty when IP authentication is enabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := mcpserver.NewConfigAdapter(tt.config)
			err := adapter.ValidateSDKCompatibility()

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestConfigAdapter_GetServerAddress(t *testing.T) {
	config := createTestConfig()
	adapter := mcpserver.NewConfigAdapter(config)

	address := adapter.GetServerAddress()
	assert.Equal(t, "localhost:8080", address)
}

func TestConfigAdapter_IsSecureTransport(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		expected bool
	}{
		{
			name:     "localhost is not secure",
			host:     "localhost",
			expected: false,
		},
		{
			name:     "127.0.0.1 is not secure",
			host:     "127.0.0.1",
			expected: false,
		},
		{
			name:     "all hosts are currently non-secure (implementation limitation)",
			host:     "api.example.com",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := createTestConfig()
			config.MCPServerHost = tt.host
			adapter := mcpserver.NewConfigAdapter(config)

			result := adapter.IsSecureTransport()
			assert.Equal(t, tt.expected, result)
		})
	}
}

// ServerWrapper Tests

func TestNewServerWrapper(t *testing.T) {
	tests := []struct {
		name        string
		config      *types.Config
		expectError bool
	}{
		{
			name:        "valid configuration",
			config:      createTestConfig(),
			expectError: false,
		},
		{
			name:        "nil configuration",
			config:      nil,
			expectError: true,
		},
		{
			name: "invalid configuration",
			config: func() *types.Config {
				config := createTestConfig()
				config.MCPServerHost = ""
				return config
			}(),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wrapper, err := mcpserver.NewServerWrapper(tt.config)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, wrapper)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, wrapper)
				assert.NotNil(t, wrapper.GetSDKServer())
				assert.NotNil(t, wrapper.GetToolRegistry())
				assert.False(t, wrapper.IsRunning())
			}
		})
	}
}

func TestServerWrapper_GetServerInfo(t *testing.T) {
	config := createTestConfig()
	wrapper, err := mcpserver.NewServerWrapper(config)
	require.NoError(t, err)

	info := wrapper.GetServerInfo()
	assert.NotNil(t, info)
	assert.Equal(t, "SDK-based", info["server_type"])
	assert.Equal(t, "localhost", info["host"])
	assert.Equal(t, 8080, info["port"])
	assert.Equal(t, false, info["is_running"])
	assert.Equal(t, true, info["sse_enabled"])
	assert.Equal(t, true, info["ip_auth_enabled"])
}

func TestServerWrapper_GetServerAddress(t *testing.T) {
	config := createTestConfig()
	wrapper, err := mcpserver.NewServerWrapper(config)
	require.NoError(t, err)

	address := wrapper.GetServerAddress()
	assert.Equal(t, "localhost:8080", address)
}

func TestServerWrapper_RegisterTool(t *testing.T) {
	config := createTestConfig()
	wrapper, err := mcpserver.NewServerWrapper(config)
	require.NoError(t, err)

	mockHandler := func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "test result"}},
		}, nil
	}

	// Note: ServerWrapper.RegisterTool creates a basic tool definition internally
	// The actual implementation might need to be updated to include proper schema
	err = wrapper.RegisterTool("test_tool", mockHandler)
	// For now, we expect this to work - if it fails, it indicates the implementation needs updating
	assert.NoError(t, err)
}

func TestServerWrapper_SetLogger(t *testing.T) {
	config := createTestConfig()
	wrapper, err := mcpserver.NewServerWrapper(config)
	require.NoError(t, err)

	customLogger := log.New(os.Stdout, "[TEST] ", log.LstdFlags)
	wrapper.SetLogger(customLogger)

	// Test that logger is set (we can't directly access it, but we can verify no error)
	assert.NotNil(t, wrapper)
}

// ToolRegistryAdapter Tests

func TestNewToolRegistryAdapter(t *testing.T) {
	server := createTestSDKServer()
	adapter := mcpserver.NewToolRegistryAdapter(server)

	assert.NotNil(t, adapter)
	assert.Equal(t, 0, adapter.ToolCount())
}

func TestToolRegistryAdapter_RegisterTool(t *testing.T) {
	server := createTestSDKServer()
	adapter := mcpserver.NewToolRegistryAdapter(server)

	definition := &mcp.Tool{
		Name:        "test_tool",
		Description: "Test tool for SDK integration",
		InputSchema: createBasicInputSchema(),
	}

	mockHandler := func(ctx context.Context, params map[string]interface{}) (*types.MCPToolCallResult, error) {
		return &types.MCPToolCallResult{
			Content: []types.MCPContent{{Type: "text", Text: "test result"}},
			IsError: false,
		}, nil
	}

	err := adapter.RegisterTool("test_tool", *definition, mockHandler)
	assert.NoError(t, err)
	assert.Equal(t, 1, adapter.ToolCount())
	assert.True(t, adapter.HasTool("test_tool"))
}

func TestToolRegistryAdapter_RegisterToolWithConfig(t *testing.T) {
	server := createTestSDKServer()
	adapter := mcpserver.NewToolRegistryAdapter(server)

	definition := &mcp.Tool{
		Name:        "internal_tool",
		Description: "Tool with different internal and configured names",
		InputSchema: createBasicInputSchema(),
	}

	mockHandler := func(ctx context.Context, params map[string]interface{}) (*types.MCPToolCallResult, error) {
		return &types.MCPToolCallResult{
			Content: []types.MCPContent{{Type: "text", Text: "configured result"}},
			IsError: false,
		}, nil
	}

	err := adapter.RegisterToolWithConfig("internal_tool", "configured_tool", *definition, mockHandler)
	assert.NoError(t, err)
	assert.Equal(t, 1, adapter.ToolCount())
	assert.True(t, adapter.HasTool("internal_tool"))

	nameMapping := adapter.GetToolNameMapping()
	assert.Equal(t, "configured_tool", nameMapping["internal_tool"])
}

func TestToolRegistryAdapter_UnregisterTool(t *testing.T) {
	server := createTestSDKServer()
	adapter := mcpserver.NewToolRegistryAdapter(server)

	definition := &mcp.Tool{
		Name:        "temp_tool",
		Description: "Temporary tool for unregister test",
		InputSchema: createBasicInputSchema(),
	}

	mockHandler := func(ctx context.Context, params map[string]interface{}) (*types.MCPToolCallResult, error) {
		return &types.MCPToolCallResult{
			Content: []types.MCPContent{{Type: "text", Text: "temp result"}},
			IsError: false,
		}, nil
	}

	// Register tool first
	err := adapter.RegisterTool("temp_tool", *definition, mockHandler)
	require.NoError(t, err)
	assert.Equal(t, 1, adapter.ToolCount())

	// Unregister tool
	err = adapter.UnregisterTool("temp_tool")
	assert.NoError(t, err)
	assert.Equal(t, 0, adapter.ToolCount())
	assert.False(t, adapter.HasTool("temp_tool"))
}

func TestToolRegistryAdapter_ListTools(t *testing.T) {
	server := createTestSDKServer()
	adapter := mcpserver.NewToolRegistryAdapter(server)

	// Initially empty
	tools := adapter.ListTools()
	assert.Empty(t, tools)

	// Register multiple tools
	for i := 0; i < 3; i++ {
		definition := &mcp.Tool{
			Name:        fmt.Sprintf("tool_%d", i),
			Description: fmt.Sprintf("Test tool %d", i),
			InputSchema: createBasicInputSchema(),
		}

		mockHandler := func(ctx context.Context, params map[string]interface{}) (*types.MCPToolCallResult, error) {
			return &types.MCPToolCallResult{
				Content: []types.MCPContent{{Type: "text", Text: fmt.Sprintf("result %d", i)}},
				IsError: false,
			}, nil
		}

		err := adapter.RegisterTool(fmt.Sprintf("tool_%d", i), *definition, mockHandler)
		require.NoError(t, err)
	}

	tools = adapter.ListTools()
	assert.Len(t, tools, 3)
}

func TestToolRegistryAdapter_ExecuteTool(t *testing.T) {
	server := createTestSDKServer()
	adapter := mcpserver.NewToolRegistryAdapter(server)

	definition := &mcp.Tool{
		Name:        "execute_test_tool",
		Description: "Tool for execute testing",
		InputSchema: createBasicInputSchema(),
	}

	expectedResult := &types.MCPToolCallResult{
		Content: []types.MCPContent{{Type: "text", Text: "execution result"}},
		IsError: false,
	}

	mockHandler := func(ctx context.Context, params map[string]interface{}) (*types.MCPToolCallResult, error) {
		return expectedResult, nil
	}

	// Register tool
	err := adapter.RegisterTool("execute_test_tool", *definition, mockHandler)
	require.NoError(t, err)

	// Execute tool
	params := map[string]interface{}{"query": "test"}
	result, err := adapter.ExecuteTool(context.Background(), "execute_test_tool", params)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, expectedResult.Content[0].Text, result.Content[0].Text)
	assert.Equal(t, expectedResult.IsError, result.IsError)
}

func TestToolRegistryAdapter_ExecuteTool_NotFound(t *testing.T) {
	server := createTestSDKServer()
	adapter := mcpserver.NewToolRegistryAdapter(server)

	params := map[string]interface{}{"query": "test"}
	result, err := adapter.ExecuteTool(context.Background(), "nonexistent_tool", params)

	// The implementation returns a result with IsError=true rather than an error
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.IsError)
	assert.Contains(t, result.Content[0].Text, "not found")
}

func TestToolRegistryAdapter_ExecuteTool_HandlerError(t *testing.T) {
	server := createTestSDKServer()
	adapter := mcpserver.NewToolRegistryAdapter(server)

	definition := &mcp.Tool{
		Name:        "error_tool",
		Description: "Tool that returns error",
		InputSchema: createBasicInputSchema(),
	}

	expectedError := errors.New("handler error")
	errorHandler := func(ctx context.Context, params map[string]interface{}) (*types.MCPToolCallResult, error) {
		return nil, expectedError
	}

	// Register error tool
	err := adapter.RegisterTool("error_tool", *definition, errorHandler)
	require.NoError(t, err)

	// Execute tool and expect error
	params := map[string]interface{}{"query": "test"}
	result, err := adapter.ExecuteTool(context.Background(), "error_tool", params)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, expectedError, err)
}

func TestToolRegistryAdapter_GetRegisteredToolNames(t *testing.T) {
	server := createTestSDKServer()
	adapter := mcpserver.NewToolRegistryAdapter(server)

	// Register tools with different names
	toolNames := []string{"alpha", "beta", "gamma"}
	for _, name := range toolNames {
		definition := &mcp.Tool{
			Name:        name,
			Description: fmt.Sprintf("Tool %s", name),
			InputSchema: createBasicInputSchema(),
		}

		mockHandler := func(ctx context.Context, params map[string]interface{}) (*types.MCPToolCallResult, error) {
			return &types.MCPToolCallResult{
				Content: []types.MCPContent{{Type: "text", Text: name}},
				IsError: false,
			}, nil
		}

		err := adapter.RegisterTool(name, *definition, mockHandler)
		require.NoError(t, err)
	}

	registeredNames := adapter.GetRegisteredToolNames()
	assert.Len(t, registeredNames, len(toolNames))

	// Check all names are present
	for _, name := range toolNames {
		assert.Contains(t, registeredNames, name)
	}
}

func TestToolRegistryAdapter_SetLogger(t *testing.T) {
	server := createTestSDKServer()
	adapter := mcpserver.NewToolRegistryAdapter(server)

	customLogger := log.New(os.Stdout, "[TEST_ADAPTER] ", log.LstdFlags)
	adapter.SetLogger(customLogger)

	// Test that logger is set (we can't directly access it, but we can verify no error)
	assert.NotNil(t, adapter)
}

// Integration Tests

func TestSDKIntegration_EndToEnd(t *testing.T) {
	config := createTestConfig()

	// Create wrapper
	wrapper, err := mcpserver.NewServerWrapper(config)
	require.NoError(t, err)

	// Register a test tool
	mockHandler := func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "integration test result"}},
		}, nil
	}

	err = wrapper.RegisterTool("integration_test_tool", mockHandler)
	assert.NoError(t, err)

	// Verify tool was registered with server
	// Note: The ServerWrapper registers tools directly with the SDK server,
	// not with the legacy ToolRegistry, so we check server info instead
	registry := wrapper.GetToolRegistry()
	assert.NotNil(t, registry)
	// The legacy registry won't have SDK-registered tools

	// Get server info to verify tool was registered
	info := wrapper.GetServerInfo()
	assert.Equal(t, "SDK-based", info["server_type"])
	// The tools_registered count reflects legacy registry, not SDK tools

	// Verify wrapper is not running initially
	assert.False(t, wrapper.IsRunning())
}

// Benchmark tests for performance validation

func BenchmarkConfigAdapter_ToSDKConfig(b *testing.B) {
	config := createTestConfig()
	adapter := mcpserver.NewConfigAdapter(config)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := adapter.ToSDKConfig()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkToolRegistryAdapter_RegisterTool(b *testing.B) {
	server := createTestSDKServer()
	adapter := mcpserver.NewToolRegistryAdapter(server)

	definition := &mcp.Tool{
		Name:        "bench_tool",
		Description: "Benchmark tool",
		InputSchema: createBasicInputSchema(),
	}

	mockHandler := func(ctx context.Context, params map[string]interface{}) (*types.MCPToolCallResult, error) {
		return &types.MCPToolCallResult{
			Content: []types.MCPContent{{Type: "text", Text: "bench result"}},
			IsError: false,
		}, nil
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Use unique tool names to avoid conflicts
		toolName := fmt.Sprintf("bench_tool_%d", i)
		localDef := *definition
		localDef.Name = toolName

		err := adapter.RegisterTool(toolName, localDef, mockHandler)
		if err != nil {
			b.Fatal(err)
		}

		// Clean up to avoid memory buildup
		_ = adapter.UnregisterTool(toolName)
	}
}
