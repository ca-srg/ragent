package mcpserver

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/ca-srg/ragent/internal/types"
)

// ToolHandler represents a function that handles tool execution
type ToolHandler func(ctx context.Context, params map[string]interface{}) (*types.MCPToolCallResult, error)

// ToolInfo contains metadata about a registered tool
type ToolInfo struct {
	Definition types.MCPToolDefinition
	Handler    ToolHandler
}

// ToolRegistry manages MCP tools and their execution
type ToolRegistry struct {
	tools       map[string]*ToolInfo
	toolNameMap map[string]string // Maps internal names to configured names
	mutex       sync.RWMutex
	logger      *log.Logger
}

// NewToolRegistry creates a new tool registry
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools:       make(map[string]*ToolInfo),
		toolNameMap: make(map[string]string),
		mutex:       sync.RWMutex{},
		logger:      log.New(os.Stdout, "[ToolRegistry] ", log.LstdFlags),
	}
}

// RegisterTool registers a new tool in the registry
func (tr *ToolRegistry) RegisterTool(internalName string, definition types.MCPToolDefinition, handler ToolHandler) error {
	if internalName == "" {
		return fmt.Errorf("internal tool name cannot be empty")
	}
	if handler == nil {
		return fmt.Errorf("tool handler cannot be nil")
	}

	tr.mutex.Lock()
	defer tr.mutex.Unlock()

	// Check if tool with this internal name already exists
	if _, exists := tr.tools[internalName]; exists {
		return fmt.Errorf("tool with internal name '%s' already registered", internalName)
	}

	// Get configured tool name from environment variable or use default
	configuredName := tr.getConfiguredToolName(internalName)
	definition.Name = configuredName

	// Check if a tool with this configured name already exists
	for _, toolInfo := range tr.tools {
		if toolInfo.Definition.Name == configuredName {
			return fmt.Errorf("tool with name '%s' already registered", configuredName)
		}
	}

	// Register the tool
	tr.tools[internalName] = &ToolInfo{
		Definition: definition,
		Handler:    handler,
	}
	tr.toolNameMap[internalName] = configuredName

	tr.logger.Printf("Registered tool: %s (internal: %s)", configuredName, internalName)
	return nil
}

// UnregisterTool removes a tool from the registry
func (tr *ToolRegistry) UnregisterTool(internalName string) error {
	tr.mutex.Lock()
	defer tr.mutex.Unlock()

	toolInfo, exists := tr.tools[internalName]
	if !exists {
		return fmt.Errorf("tool with internal name '%s' not found", internalName)
	}

	configuredName := toolInfo.Definition.Name
	delete(tr.tools, internalName)
	delete(tr.toolNameMap, internalName)

	tr.logger.Printf("Unregistered tool: %s (internal: %s)", configuredName, internalName)
	return nil
}

// ExecuteTool executes a tool by name
func (tr *ToolRegistry) ExecuteTool(ctx context.Context, toolName string, params map[string]interface{}) (*types.MCPToolCallResult, error) {
	tr.mutex.RLock()
	defer tr.mutex.RUnlock()

	// Find tool by configured name
	var toolInfo *ToolInfo
	var internalName string
	for internal, info := range tr.tools {
		if info.Definition.Name == toolName {
			toolInfo = info
			internalName = internal
			break
		}
	}

	if toolInfo == nil {
		return nil, fmt.Errorf("tool '%s' not found", toolName)
	}

	tr.logger.Printf("Executing tool: %s (internal: %s)", toolName, internalName)

	type execResult struct {
		result *types.MCPToolCallResult
		err    error
	}

	resultCh := make(chan execResult, 1)

	go func() {
		res, err := toolInfo.Handler(ctx, params)
		resultCh <- execResult{result: res, err: err}
	}()

	select {
	case <-ctx.Done():
		err := ctx.Err()
		tr.logger.Printf("Tool execution timed out or was cancelled for %s: %v", toolName, err)
		return &types.MCPToolCallResult{
			Content: []types.MCPContent{
				{
					Type: "text",
					Text: fmt.Sprintf("Tool execution cancelled: %v", err),
				},
			},
			IsError: true,
		}, err
	case exec := <-resultCh:
		if exec.err != nil {
			tr.logger.Printf("Tool execution failed for %s: %v", toolName, exec.err)
			return &types.MCPToolCallResult{
				Content: []types.MCPContent{
					{
						Type: "text",
						Text: fmt.Sprintf("Tool execution failed: %v", exec.err),
					},
				},
				IsError: true,
			}, exec.err
		}

		tr.logger.Printf("Tool execution completed successfully: %s", toolName)
		return exec.result, nil
	}
}

// ListTools returns all registered tools
func (tr *ToolRegistry) ListTools() []types.MCPToolDefinition {
	tr.mutex.RLock()
	defer tr.mutex.RUnlock()

	tools := make([]types.MCPToolDefinition, 0, len(tr.tools))
	for _, toolInfo := range tr.tools {
		tools = append(tools, toolInfo.Definition)
	}

	return tools
}

// GetTool returns a specific tool definition by name
func (tr *ToolRegistry) GetTool(toolName string) (*types.MCPToolDefinition, error) {
	tr.mutex.RLock()
	defer tr.mutex.RUnlock()

	for _, toolInfo := range tr.tools {
		if toolInfo.Definition.Name == toolName {
			return &toolInfo.Definition, nil
		}
	}

	return nil, fmt.Errorf("tool '%s' not found", toolName)
}

// HasTool checks if a tool is registered
func (tr *ToolRegistry) HasTool(toolName string) bool {
	tr.mutex.RLock()
	defer tr.mutex.RUnlock()

	for _, toolInfo := range tr.tools {
		if toolInfo.Definition.Name == toolName {
			return true
		}
	}

	return false
}

// ToolCount returns the number of registered tools
func (tr *ToolRegistry) ToolCount() int {
	tr.mutex.RLock()
	defer tr.mutex.RUnlock()

	return len(tr.tools)
}

// getConfiguredToolName gets the configured name for a tool from environment variable
func (tr *ToolRegistry) getConfiguredToolName(internalName string) string {
	// Check for specific tool name configuration
	envVarNameUpper := fmt.Sprintf("MCP_TOOL_NAME_%s", strings.ToUpper(internalName))
	if configuredName := os.Getenv(envVarNameUpper); configuredName != "" {
		return configuredName
	}

	envVarName := fmt.Sprintf("MCP_TOOL_NAME_%s", internalName)
	if configuredName := os.Getenv(envVarName); configuredName != "" {
		return configuredName
	}

	// Check for general tool name prefix
	if prefix := os.Getenv("MCP_TOOL_PREFIX"); prefix != "" {
		return prefix + internalName
	}

	// Use internal name as default
	return internalName
}

// GetToolNameMapping returns the mapping of internal names to configured names
func (tr *ToolRegistry) GetToolNameMapping() map[string]string {
	tr.mutex.RLock()
	defer tr.mutex.RUnlock()

	mapping := make(map[string]string)
	for internal, configured := range tr.toolNameMap {
		mapping[internal] = configured
	}

	return mapping
}

// ValidateToolDefinition validates a tool definition
func ValidateToolDefinition(definition types.MCPToolDefinition) error {
	if definition.Name == "" {
		return fmt.Errorf("tool name cannot be empty")
	}
	if definition.Description == "" {
		return fmt.Errorf("tool description cannot be empty")
	}
	if definition.InputSchema == nil {
		return fmt.Errorf("tool input schema cannot be nil")
	}

	return nil
}

// CreateToolCallResult creates a successful tool call result
func CreateToolCallResult(content string) *types.MCPToolCallResult {
	return &types.MCPToolCallResult{
		Content: []types.MCPContent{
			{
				Type: "text",
				Text: content,
			},
		},
		IsError: false,
	}
}

// CreateToolCallErrorResult creates an error tool call result
func CreateToolCallErrorResult(errorMsg string) *types.MCPToolCallResult {
	return &types.MCPToolCallResult{
		Content: []types.MCPContent{
			{
				Type: "text",
				Text: errorMsg,
			},
		},
		IsError: true,
	}
}

// CreateToolCallResultWithMetadata creates a tool call result with metadata
func CreateToolCallResultWithMetadata(content string, metadata map[string]interface{}) *types.MCPToolCallResult {
	result := &types.MCPToolCallResult{
		Content: []types.MCPContent{
			{
				Type: "text",
				Text: content,
			},
		},
		IsError: false,
	}

	// Add metadata as additional content if provided
	for key, value := range metadata {
		result.Content = append(result.Content, types.MCPContent{
			Type: "text",
			Text: fmt.Sprintf("%s: %v", key, value),
		})
	}

	return result
}

// SetLogger sets a custom logger for the registry
func (tr *ToolRegistry) SetLogger(logger *log.Logger) {
	tr.mutex.Lock()
	defer tr.mutex.Unlock()
	tr.logger = logger
}

// GetRegisteredToolNames returns a list of all registered tool names (configured names)
func (tr *ToolRegistry) GetRegisteredToolNames() []string {
	tr.mutex.RLock()
	defer tr.mutex.RUnlock()

	names := make([]string, 0, len(tr.tools))
	for _, toolInfo := range tr.tools {
		names = append(names, toolInfo.Definition.Name)
	}

	return names
}
