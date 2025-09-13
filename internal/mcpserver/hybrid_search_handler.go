package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ca-srg/ragent/internal/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/opensearch"
	"github.com/ca-srg/ragent/internal/types"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// HybridSearchHandler wraps HybridSearchToolAdapter for SDK compatibility
// Maintains identical search functionality while providing SDK tool handler interface
type HybridSearchHandler struct {
	adapter *HybridSearchToolAdapter
}

// NewHybridSearchHandler creates a new SDK-compatible hybrid search handler
func NewHybridSearchHandler(osClient *opensearch.Client, embeddingClient *bedrock.BedrockClient, config *HybridSearchConfig) *HybridSearchHandler {
	adapter := NewHybridSearchToolAdapter(osClient, embeddingClient, config)

	return &HybridSearchHandler{
		adapter: adapter,
	}
}

// NewHybridSearchHandlerFromAdapter creates handler from existing adapter
func NewHybridSearchHandlerFromAdapter(adapter *HybridSearchToolAdapter) *HybridSearchHandler {
	return &HybridSearchHandler{
		adapter: adapter,
	}
}

// HandleSDKToolCall implements SDK tool handler interface
// Converts SDK request to RAGent format, executes search, and converts response back
func (hsh *HybridSearchHandler) HandleSDKToolCall(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Convert SDK request parameters to RAGent format
	params := make(map[string]interface{})
	if req.Params.Arguments != nil {
		if err := json.Unmarshal(req.Params.Arguments, &params); err != nil {
			return nil, fmt.Errorf("failed to unmarshal tool arguments: %w", err)
		}
	}

	// Execute using existing adapter
	ragentResult, err := hsh.adapter.HandleToolCall(ctx, params)
	if err != nil {
		return nil, err
	}

	// Convert RAGent result to SDK format
	return convertRAGentResultToSDK(ragentResult), nil
}

// GetSDKToolDefinition returns SDK-compatible tool definition
func (hsh *HybridSearchHandler) GetSDKToolDefinition() *mcp.Tool {
	ragentDef := hsh.adapter.GetToolDefinition()
	return convertRAGentToolToSDK(ragentDef)
}

// GetAdapter returns the underlying HybridSearchToolAdapter for backward compatibility
func (hsh *HybridSearchHandler) GetAdapter() *HybridSearchToolAdapter {
	return hsh.adapter
}

// SetDefaultConfig sets default configuration for the handler
func (hsh *HybridSearchHandler) SetDefaultConfig(config *HybridSearchConfig) {
	hsh.adapter.SetDefaultConfig(config)
}

// GetDefaultConfig gets default configuration from the handler
func (hsh *HybridSearchHandler) GetDefaultConfig() *HybridSearchConfig {
	return hsh.adapter.GetDefaultConfig()
}

// convertRAGentResultToSDK converts RAGent tool result to SDK format
func convertRAGentResultToSDK(ragentResult *types.MCPToolCallResult) *mcp.CallToolResult {
	if ragentResult == nil {
		return &mcp.CallToolResult{}
	}

	// Convert content from legacy RAGent format to SDK format
	var content []mcp.Content
	for _, c := range ragentResult.Content {
		// Convert legacy MCPContent (with Type field) to SDK TextContent
		switch c.Type {
		case "text":
			content = append(content, &mcp.TextContent{
				Text: c.Text,
			})
		// Add other content types as needed in the future
		default:
			// Default to text content
			content = append(content, &mcp.TextContent{
				Text: c.Text,
			})
		}
	}

	return &mcp.CallToolResult{
		Content: content,
		IsError: ragentResult.IsError,
	}
}

// convertRAGentToolToSDK converts RAGent tool definition to SDK format
func convertRAGentToolToSDK(ragentDef types.MCPToolDefinition) *mcp.Tool {
	// Convert RAGent's InputSchema (map[string]interface{}) to jsonschema.Schema
	var inputSchema *jsonschema.Schema
	if ragentDef.InputSchema != nil {
		// Convert map to JSON then unmarshal into jsonschema.Schema
		schemaBytes, err := json.Marshal(ragentDef.InputSchema)
		if err == nil {
			inputSchema = &jsonschema.Schema{}
			_ = json.Unmarshal(schemaBytes, inputSchema)
		}
	}

	return &mcp.Tool{
		Name:        ragentDef.Name,
		Description: ragentDef.Description,
		InputSchema: inputSchema,
	}
}
