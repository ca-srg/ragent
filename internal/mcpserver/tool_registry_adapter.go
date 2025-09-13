package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/ca-srg/ragent/internal/types"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolRegistryAdapter bridges RAGent's tool registration patterns with SDK tool management
// It maintains compatibility with existing ToolRegistry API while using SDK v0.4.0 internally
type ToolRegistryAdapter struct {
	server      *mcp.Server
	tools       map[string]*ToolInfo // Maintains existing tool storage
	toolNameMap map[string]string    // Maps internal names to configured names
	sdkTools    map[string]*mcp.Tool // SDK tool definitions
	mutex       sync.RWMutex
	logger      *log.Logger
}

// NewToolRegistryAdapter creates a new adapter that bridges RAGent and SDK tool management
func NewToolRegistryAdapter(server *mcp.Server) *ToolRegistryAdapter {
	return &ToolRegistryAdapter{
		server:      server,
		tools:       make(map[string]*ToolInfo),
		toolNameMap: make(map[string]string),
		sdkTools:    make(map[string]*mcp.Tool),
		logger:      log.Default(),
	}
}

// RegisterTool maintains existing API while registering with SDK server
func (tra *ToolRegistryAdapter) RegisterTool(name string, definition types.MCPToolDefinition, handler ToolHandler) error {
	return tra.RegisterToolWithConfig(name, name, definition, handler)
}

// RegisterToolWithConfig registers tool with name mapping, maintaining existing API
func (tra *ToolRegistryAdapter) RegisterToolWithConfig(internalName, configuredName string, definition types.MCPToolDefinition, handler ToolHandler) error {
	tra.mutex.Lock()
	defer tra.mutex.Unlock()

	// Validate tool definition using existing validation
	if err := ValidateToolDefinition(definition); err != nil {
		return fmt.Errorf("tool definition validation failed: %w", err)
	}

	// Create SDK tool definition from RAGent definition
	sdkTool := convertToSDKTool(configuredName, definition)

	// Create adapter handler that bridges RAGent ToolHandler to SDK handler
	sdkHandler := tra.createSDKHandler(handler)

	// Register with SDK server
	tra.server.AddTool(sdkTool, sdkHandler)

	// Store in existing maps for compatibility
	tra.tools[internalName] = &ToolInfo{
		Definition: definition,
		Handler:    handler,
	}
	tra.toolNameMap[internalName] = configuredName
	tra.sdkTools[internalName] = sdkTool

	if tra.logger != nil {
		tra.logger.Printf("Tool registered: %s -> %s", internalName, configuredName)
	}

	return nil
}

// UnregisterTool removes tool from both RAGent and SDK registries
func (tra *ToolRegistryAdapter) UnregisterTool(name string) error {
	tra.mutex.Lock()
	defer tra.mutex.Unlock()

	if _, exists := tra.tools[name]; !exists {
		return fmt.Errorf("tool %s not found", name)
	}

	// Remove from SDK server (SDK doesn't have direct unregister, track this limitation)
	// For now, we remove from our tracking but SDK server keeps the tool
	// This is a limitation we'll document

	delete(tra.tools, name)
	delete(tra.toolNameMap, name)
	delete(tra.sdkTools, name)

	if tra.logger != nil {
		tra.logger.Printf("Tool unregistered: %s", name)
	}

	return nil
}

// ExecuteTool executes tool using existing API signature
func (tra *ToolRegistryAdapter) ExecuteTool(ctx context.Context, toolName string, params map[string]interface{}) (*types.MCPToolCallResult, error) {
	tra.mutex.RLock()
	toolInfo, exists := tra.tools[toolName]
	tra.mutex.RUnlock()

	if !exists {
		return CreateToolCallErrorResult(fmt.Sprintf("Tool %s not found", toolName)), nil
	}

	// Execute using original handler for consistency
	return toolInfo.Handler(ctx, params)
}

// ListTools returns tool definitions in existing format
func (tra *ToolRegistryAdapter) ListTools() []types.MCPToolDefinition {
	tra.mutex.RLock()
	defer tra.mutex.RUnlock()

	tools := make([]types.MCPToolDefinition, 0, len(tra.tools))
	for _, toolInfo := range tra.tools {
		tools = append(tools, toolInfo.Definition)
	}
	return tools
}

// GetTool retrieves tool info maintaining existing API
func (tra *ToolRegistryAdapter) GetTool(name string) (*ToolInfo, bool) {
	tra.mutex.RLock()
	defer tra.mutex.RUnlock()

	toolInfo, exists := tra.tools[name]
	return toolInfo, exists
}

// HasTool checks if tool exists
func (tra *ToolRegistryAdapter) HasTool(name string) bool {
	tra.mutex.RLock()
	defer tra.mutex.RUnlock()

	_, exists := tra.tools[name]
	return exists
}

// ToolCount returns number of registered tools
func (tra *ToolRegistryAdapter) ToolCount() int {
	tra.mutex.RLock()
	defer tra.mutex.RUnlock()

	return len(tra.tools)
}

// getConfiguredToolName maintains existing name mapping logic
func (tra *ToolRegistryAdapter) getConfiguredToolName(internalName string) string {
	tra.mutex.RLock()
	defer tra.mutex.RUnlock()

	if configuredName, exists := tra.toolNameMap[internalName]; exists {
		return configuredName
	}
	return internalName
}

// GetToolNameMapping returns tool name mappings
func (tra *ToolRegistryAdapter) GetToolNameMapping() map[string]string {
	tra.mutex.RLock()
	defer tra.mutex.RUnlock()

	mapping := make(map[string]string)
	for internal, configured := range tra.toolNameMap {
		mapping[internal] = configured
	}
	return mapping
}

// SetLogger sets logger for the adapter
func (tra *ToolRegistryAdapter) SetLogger(logger *log.Logger) {
	tra.mutex.Lock()
	defer tra.mutex.Unlock()

	tra.logger = logger
}

// GetRegisteredToolNames returns list of registered tool names
func (tra *ToolRegistryAdapter) GetRegisteredToolNames() []string {
	tra.mutex.RLock()
	defer tra.mutex.RUnlock()

	names := make([]string, 0, len(tra.tools))
	for name := range tra.tools {
		names = append(names, name)
	}
	return names
}

// createSDKHandler creates SDK-compatible handler from RAGent ToolHandler
func (tra *ToolRegistryAdapter) createSDKHandler(ragentHandler ToolHandler) func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Convert SDK request to RAGent parameter format
		params := make(map[string]interface{})
		if req.Params.Arguments != nil {
			if err := json.Unmarshal(req.Params.Arguments, &params); err != nil {
				return nil, fmt.Errorf("failed to unmarshal tool arguments: %w", err)
			}
		}

		// Execute RAGent handler
		ragentResult, err := ragentHandler(ctx, params)
		if err != nil {
			return nil, err
		}

		// Convert RAGent result to SDK result
		return convertToSDKResult(ragentResult), nil
	}
}

// convertToSDKTool converts RAGent tool definition to SDK tool
func convertToSDKTool(name string, definition types.MCPToolDefinition) *mcp.Tool {
	// Convert RAGent's InputSchema (map[string]interface{}) to jsonschema.Schema
	var inputSchema *jsonschema.Schema
	if definition.InputSchema != nil {
		// Convert map to JSON then unmarshal into jsonschema.Schema
		schemaBytes, err := json.Marshal(definition.InputSchema)
		if err == nil {
			inputSchema = &jsonschema.Schema{}
			_ = json.Unmarshal(schemaBytes, inputSchema)
		}
	}

	return &mcp.Tool{
		Name:        name,
		Description: definition.Description,
		InputSchema: inputSchema,
	}
}

// convertToSDKResult converts RAGent tool result to SDK result
func convertToSDKResult(ragentResult *types.MCPToolCallResult) *mcp.CallToolResult {
	if ragentResult == nil {
		return &mcp.CallToolResult{}
	}

	// Convert content from RAGent format to SDK format
	var content []mcp.Content
	for _, c := range ragentResult.Content {
		switch c.Type {
		case "text":
			content = append(content, &mcp.TextContent{
				Text: c.Text,
			})
			// Note: RAGent's MCPContent currently only supports text content
			// Image support would require extending MCPContent struct
		}
	}

	result := &mcp.CallToolResult{
		Content: content,
		IsError: ragentResult.IsError,
	}

	// Note: SDK doesn't support meta field directly in v0.4.0
	// Meta information should be included in content if needed

	return result
}
