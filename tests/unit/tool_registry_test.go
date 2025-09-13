package unit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ca-srg/ragent/internal/mcpserver"
	"github.com/ca-srg/ragent/internal/types"
)

// Mock tool handler for testing
func mockToolHandler(ctx context.Context, params map[string]interface{}) (*types.MCPToolCallResult, error) {
	return &types.MCPToolCallResult{
		Content: []types.MCPContent{{Type: "text", Text: "mock result"}},
		IsError: false,
	}, nil
}

// Error tool handler for testing error scenarios
func errorToolHandler(ctx context.Context, params map[string]interface{}) (*types.MCPToolCallResult, error) {
	return nil, errors.New("mock error")
}

// Slow tool handler for testing timeouts
func slowToolHandler(ctx context.Context, params map[string]interface{}) (*types.MCPToolCallResult, error) {
	time.Sleep(100 * time.Millisecond)
	return &types.MCPToolCallResult{
		Content: []types.MCPContent{{Type: "text", Text: "slow result"}},
		IsError: false,
	}, nil
}

func TestNewToolRegistry(t *testing.T) {
	registry := mcpserver.NewToolRegistry()
	if registry == nil {
		t.Fatal("NewToolRegistry() returned nil")
	}

	if registry.ToolCount() != 0 {
		t.Errorf("Expected 0 tools in new registry, got %d", registry.ToolCount())
	}
}

func TestToolRegistry_RegisterTool(t *testing.T) {
	registry := mcpserver.NewToolRegistry()

	definition := types.MCPToolDefinition{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: nil,
	}

	tests := []struct {
		name         string
		internalName string
		definition   types.MCPToolDefinition
		handler      mcpserver.ToolHandler
		expectError  bool
	}{
		{
			name:         "valid tool registration",
			internalName: "test_tool",
			definition:   definition,
			handler:      mockToolHandler,
			expectError:  false,
		},
		{
			name:         "empty internal name",
			internalName: "",
			definition:   definition,
			handler:      mockToolHandler,
			expectError:  true,
		},
		{
			name:         "nil handler",
			internalName: "nil_handler_tool",
			definition:   definition,
			handler:      nil,
			expectError:  true,
		},
		{
			name:         "duplicate internal name",
			internalName: "test_tool", // Already registered above
			definition:   definition,
			handler:      mockToolHandler,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := registry.RegisterTool(tt.internalName, tt.definition, tt.handler)
			if (err != nil) != tt.expectError {
				t.Errorf("RegisterTool() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

func TestToolRegistry_RegisterToolWithEnvVarNaming(t *testing.T) {
	// Test tool name configuration via environment variable
	if err := os.Setenv("MCP_TOOL_NAME_TEST_TOOL", "custom_test_tool"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	defer func() {
		if err := os.Unsetenv("MCP_TOOL_NAME_TEST_TOOL"); err != nil {
			t.Fatalf("failed to unset env: %v", err)
		}
	}()

	registry := mcpserver.NewToolRegistry()

	definition := types.MCPToolDefinition{
		Name:        "test_tool", // This should be overridden
		Description: "A test tool",
		InputSchema: nil,
	}

	err := registry.RegisterTool("test_tool", definition, mockToolHandler)
	if err != nil {
		t.Fatalf("Failed to register tool: %v", err)
	}

	// Check if the tool was registered with the custom name
	tools := registry.ListTools()
	if len(tools) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(tools))
	}

	if tools[0].Name != "custom_test_tool" {
		t.Errorf("Expected tool name 'custom_test_tool', got '%s'", tools[0].Name)
	}

	// Verify we can find the tool by its configured name
	if !registry.HasTool("custom_test_tool") {
		t.Error("Tool should be findable by its configured name")
	}
}

func TestToolRegistry_RegisterToolWithPrefix(t *testing.T) {
	// Test tool prefix configuration
	if err := os.Setenv("MCP_TOOL_PREFIX", "prefix_"); err != nil {
		t.Fatalf("failed to set env: %v", err)
	}
	defer func() {
		if err := os.Unsetenv("MCP_TOOL_PREFIX"); err != nil {
			t.Fatalf("failed to unset env: %v", err)
		}
	}()

	registry := mcpserver.NewToolRegistry()

	definition := types.MCPToolDefinition{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: nil,
	}

	err := registry.RegisterTool("test_tool", definition, mockToolHandler)
	if err != nil {
		t.Fatalf("Failed to register tool: %v", err)
	}

	tools := registry.ListTools()
	if len(tools) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(tools))
	}

	if tools[0].Name != "prefix_test_tool" {
		t.Errorf("Expected tool name 'prefix_test_tool', got '%s'", tools[0].Name)
	}
}

func TestToolRegistry_UnregisterTool(t *testing.T) {
	registry := mcpserver.NewToolRegistry()

	definition := types.MCPToolDefinition{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: nil,
	}

	// Register a tool first
	err := registry.RegisterTool("test_tool", definition, mockToolHandler)
	if err != nil {
		t.Fatalf("Failed to register tool: %v", err)
	}

	if registry.ToolCount() != 1 {
		t.Errorf("Expected 1 tool after registration, got %d", registry.ToolCount())
	}

	// Unregister the tool
	err = registry.UnregisterTool("test_tool")
	if err != nil {
		t.Errorf("Failed to unregister tool: %v", err)
	}

	if registry.ToolCount() != 0 {
		t.Errorf("Expected 0 tools after unregistration, got %d", registry.ToolCount())
	}

	// Try to unregister non-existent tool
	err = registry.UnregisterTool("non_existent")
	if err == nil {
		t.Error("Expected error when unregistering non-existent tool")
	}
}

func TestToolRegistry_ExecuteTool(t *testing.T) {
	registry := mcpserver.NewToolRegistry()

	// Register test tools
	definition := types.MCPToolDefinition{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: nil,
	}

	errorDefinition := types.MCPToolDefinition{
		Name:        "error_tool",
		Description: "An error tool",
		InputSchema: nil,
	}

	if err := registry.RegisterTool("test_tool", definition, mockToolHandler); err != nil {
		t.Fatalf("failed to register tool: %v", err)
	}
	if err := registry.RegisterTool("error_tool", errorDefinition, errorToolHandler); err != nil {
		t.Fatalf("failed to register error tool: %v", err)
	}

	tests := []struct {
		name        string
		toolName    string
		params      map[string]interface{}
		expectError bool
	}{
		{
			name:        "execute existing tool",
			toolName:    "test_tool",
			params:      map[string]interface{}{"param1": "value1"},
			expectError: false,
		},
		{
			name:        "execute non-existent tool",
			toolName:    "non_existent",
			params:      map[string]interface{}{},
			expectError: true,
		},
		{
			name:        "execute tool that returns error",
			toolName:    "error_tool",
			params:      map[string]interface{}{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result, err := registry.ExecuteTool(ctx, tt.toolName, tt.params)

			if (err != nil) != tt.expectError {
				t.Errorf("ExecuteTool() error = %v, expectError %v", err, tt.expectError)
			}

			if !tt.expectError && result == nil {
				t.Error("Expected result for successful execution")
			}
		})
	}
}

func TestToolRegistry_ListTools(t *testing.T) {
	registry := mcpserver.NewToolRegistry()

	// Empty registry
	tools := registry.ListTools()
	if len(tools) != 0 {
		t.Errorf("Expected 0 tools in empty registry, got %d", len(tools))
	}

	// Register some tools
	definitions := []types.MCPToolDefinition{
		{Name: "tool1", Description: "Tool 1", InputSchema: nil},
		{Name: "tool2", Description: "Tool 2", InputSchema: nil},
		{Name: "tool3", Description: "Tool 3", InputSchema: nil},
	}

	for i, def := range definitions {
		if err := registry.RegisterTool(def.Name, def, mockToolHandler); err != nil {
			t.Fatalf("failed to register tool %s: %v", def.Name, err)
		}

		tools = registry.ListTools()
		if len(tools) != i+1 {
			t.Errorf("Expected %d tools after registering %d, got %d", i+1, i+1, len(tools))
		}
	}

	// Verify all tools are listed
	tools = registry.ListTools()
	if len(tools) != 3 {
		t.Errorf("Expected 3 tools, got %d", len(tools))
	}

	// Check that all registered tools are in the list
	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}

	for _, def := range definitions {
		if !toolNames[def.Name] {
			t.Errorf("Tool %s not found in list", def.Name)
		}
	}
}

func TestToolRegistry_GetTool(t *testing.T) {
	registry := mcpserver.NewToolRegistry()

	definition := types.MCPToolDefinition{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: nil,
	}

	if err := registry.RegisterTool("test_tool", definition, mockToolHandler); err != nil {
		t.Fatalf("failed to register tool: %v", err)
	}

	// Get existing tool
	tool, err := registry.GetTool("test_tool")
	if err != nil {
		t.Errorf("Failed to get existing tool: %v", err)
	}
	if tool == nil {
		t.Error("Got nil tool for existing tool")
	}
	if tool != nil && tool.Name != "test_tool" {
		t.Errorf("Expected tool name 'test_tool', got '%s'", tool.Name)
	}

	// Get non-existent tool
	_, err = registry.GetTool("non_existent")
	if err == nil {
		t.Error("Expected error when getting non-existent tool")
	}
}

func TestToolRegistry_HasTool(t *testing.T) {
	registry := mcpserver.NewToolRegistry()

	definition := types.MCPToolDefinition{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: nil,
	}

	// Tool doesn't exist yet
	if registry.HasTool("test_tool") {
		t.Error("HasTool should return false for non-existent tool")
	}

	// Register tool
	if err := registry.RegisterTool("test_tool", definition, mockToolHandler); err != nil {
		t.Fatalf("failed to register tool: %v", err)
	}

	// Tool should exist now
	if !registry.HasTool("test_tool") {
		t.Error("HasTool should return true for existing tool")
	}

	// Non-existent tool
	if registry.HasTool("non_existent") {
		t.Error("HasTool should return false for non-existent tool")
	}
}

func TestToolRegistry_ConcurrentAccess(t *testing.T) {
	registry := mcpserver.NewToolRegistry()

	// Number of concurrent operations
	numGoroutines := 10
	numOperationsPerGoroutine := 100

	var wg sync.WaitGroup

	// Concurrent tool registration
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperationsPerGoroutine; j++ {
				toolName := fmt.Sprintf("tool_%d_%d", id, j)
				definition := types.MCPToolDefinition{
					Name:        toolName,
					Description: "Concurrent test tool",
					InputSchema: nil,
				}
				if err := registry.RegisterTool(toolName, definition, mockToolHandler); err != nil {
					t.Logf("register tool failed (%s): %v", toolName, err)
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify all tools were registered
	expectedCount := numGoroutines * numOperationsPerGoroutine
	if registry.ToolCount() != expectedCount {
		t.Errorf("Expected %d tools after concurrent registration, got %d", expectedCount, registry.ToolCount())
	}

	// Concurrent tool execution
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ { // Fewer executions than registrations
				toolName := fmt.Sprintf("tool_%d_%d", id, j)
				ctx := context.Background()
				if _, err := registry.ExecuteTool(ctx, toolName, map[string]interface{}{}); err != nil {
					t.Logf("execute tool failed (%s): %v", toolName, err)
				}
			}
		}(i)
	}

	wg.Wait()

	// Concurrent tool listing and checking
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				registry.ListTools()
				registry.ToolCount()
				registry.HasTool("tool_0_0")
				registry.GetRegisteredToolNames()
			}
		}()
	}

	wg.Wait()

	// Test should complete without data races or panics
}

func TestToolRegistry_ConcurrentRegistrationAndUnregistration(t *testing.T) {
	registry := mcpserver.NewToolRegistry()

	// Pre-register some tools
	for i := 0; i < 50; i++ {
		toolName := fmt.Sprintf("tool_%d", i)
		definition := types.MCPToolDefinition{
			Name:        toolName,
			Description: "Test tool",
			InputSchema: nil,
		}
		if err := registry.RegisterTool(toolName, definition, mockToolHandler); err != nil {
			t.Logf("register tool failed (%s): %v", toolName, err)
		}
	}

	var wg sync.WaitGroup
	numGoroutines := 10

	// Concurrent registration and unregistration
	wg.Add(numGoroutines * 2)

	// Registration goroutines
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				toolName := fmt.Sprintf("new_tool_%d_%d", id, j)
				definition := types.MCPToolDefinition{
					Name:        toolName,
					Description: "New test tool",
					InputSchema: nil,
				}
				if err := registry.RegisterTool(toolName, definition, mockToolHandler); err != nil {
					t.Logf("register tool failed (%s): %v", toolName, err)
				}
			}
		}(i)
	}

	// Unregistration goroutines
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 5; j++ { // Unregister fewer than we have
				toolName := fmt.Sprintf("tool_%d", id*5+j)
				if err := registry.UnregisterTool(toolName); err != nil {
					t.Logf("unregister tool failed (%s): %v", toolName, err)
				}
			}
		}(i)
	}

	wg.Wait()

	// Should have some tools remaining
	if registry.ToolCount() == 0 {
		t.Error("Expected some tools to remain after concurrent operations")
	}
}

func TestToolRegistry_ExecuteToolTimeout(t *testing.T) {
	registry := mcpserver.NewToolRegistry()

	definition := types.MCPToolDefinition{
		Name:        "slow_tool",
		Description: "A slow tool",
		InputSchema: nil,
	}

	if err := registry.RegisterTool("slow_tool", definition, slowToolHandler); err != nil {
		t.Fatalf("failed to register slow_tool: %v", err)
	}

	// Test with timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := registry.ExecuteTool(ctx, "slow_tool", map[string]interface{}{})
	if err == nil {
		t.Error("Expected timeout error for slow tool execution")
	}
}

func TestToolRegistry_ValidateToolDefinition(t *testing.T) {
	tests := []struct {
		name        string
		definition  types.MCPToolDefinition
		expectError bool
	}{
		{
			name: "valid definition",
			definition: types.MCPToolDefinition{
				Name:        "valid_tool",
				Description: "A valid tool",
				InputSchema: nil,
			},
			expectError: false,
		},
		{
			name: "empty name",
			definition: types.MCPToolDefinition{
				Name:        "",
				Description: "Tool with empty name",
				InputSchema: nil,
			},
			expectError: true,
		},
		{
			name: "empty description",
			definition: types.MCPToolDefinition{
				Name:        "tool_no_desc",
				Description: "",
				InputSchema: nil,
			},
			expectError: true,
		},
		{
			name: "nil input schema",
			definition: types.MCPToolDefinition{
				Name:        "tool_no_schema",
				Description: "Tool without schema",
				InputSchema: nil,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mcpserver.ValidateToolDefinition(tt.definition)
			if (err != nil) != tt.expectError {
				t.Errorf("ValidateToolDefinition() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

func TestToolRegistry_HelperFunctions(t *testing.T) {
	// Test CreateToolCallResult
	result := mcpserver.CreateToolCallResult("test content")
	if result == nil {
		t.Fatal("CreateToolCallResult returned nil")
	}
	if result.IsError {
		t.Error("CreateToolCallResult should not create error result")
	}
	if len(result.Content) != 1 || result.Content[0].Text != "test content" {
		t.Error("CreateToolCallResult content mismatch")
	}

	// Test CreateToolCallErrorResult
	errorResult := mcpserver.CreateToolCallErrorResult("test error")
	if errorResult == nil {
		t.Fatal("CreateToolCallErrorResult returned nil")
	}
	if !errorResult.IsError {
		t.Error("CreateToolCallErrorResult should create error result")
	}
	if len(errorResult.Content) != 1 || errorResult.Content[0].Text != "test error" {
		t.Error("CreateToolCallErrorResult content mismatch")
	}

	// Test CreateToolCallResultWithMetadata
	metadata := map[string]interface{}{"key1": "value1", "key2": 42}
	metadataResult := mcpserver.CreateToolCallResultWithMetadata("content with metadata", metadata)
	if metadataResult == nil {
		t.Fatal("CreateToolCallResultWithMetadata returned nil")
	}
	if metadataResult.IsError {
		t.Error("CreateToolCallResultWithMetadata should not create error result")
	}
	if len(metadataResult.Content) < 1 {
		t.Error("CreateToolCallResultWithMetadata should have content")
	}
}

// Benchmark tests
func BenchmarkToolRegistry_RegisterTool(b *testing.B) {
	registry := mcpserver.NewToolRegistry()
	definition := types.MCPToolDefinition{
		Name:        "benchmark_tool",
		Description: "Benchmark tool",
		InputSchema: nil,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		toolName := fmt.Sprintf("tool_%d", i)
		definition.Name = toolName
		if err := registry.RegisterTool(toolName, definition, mockToolHandler); err != nil {
			b.Fatalf("failed to register tool: %v", err)
		}
	}
}

func BenchmarkToolRegistry_ExecuteTool(b *testing.B) {
	registry := mcpserver.NewToolRegistry()
	definition := types.MCPToolDefinition{
		Name:        "benchmark_tool",
		Description: "Benchmark tool",
		InputSchema: nil,
	}

	if err := registry.RegisterTool("benchmark_tool", definition, mockToolHandler); err != nil {
		b.Fatalf("failed to register benchmark_tool: %v", err)
	}

	ctx := context.Background()
	params := map[string]interface{}{"param": "value"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := registry.ExecuteTool(ctx, "benchmark_tool", params); err != nil {
			b.Logf("execute tool failed: %v", err)
		}
	}
}

func BenchmarkToolRegistry_ListTools(b *testing.B) {
	registry := mcpserver.NewToolRegistry()

	// Pre-register 100 tools
	for i := 0; i < 100; i++ {
		toolName := fmt.Sprintf("tool_%d", i)
		definition := types.MCPToolDefinition{
			Name:        toolName,
			Description: "Benchmark tool",
			InputSchema: nil,
		}
		if err := registry.RegisterTool(toolName, definition, mockToolHandler); err != nil {
			b.Fatalf("failed to register tool: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		registry.ListTools()
	}
}
