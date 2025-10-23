package types

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCP SDK Type Aliases - Using SDK types instead of custom implementations
// These aliases maintain backward compatibility while migrating to SDK types

// MCPToolDefinition is now an alias to the SDK Tool type
type MCPToolDefinition = mcp.Tool

// MCPToolCallResult represents the result of a tool call (legacy compatibility)
type MCPToolCallResult struct {
	Content []MCPContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

// Legacy MCPContent with Type field for backward compatibility
type MCPContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// MCPToolRequest is now an alias to the SDK CallToolRequest type
type MCPToolRequest = mcp.CallToolRequest

// Standard error codes (maintained for compatibility)
const (
	MCPErrorParseError     = -32700
	MCPErrorInvalidRequest = -32600
	MCPErrorMethodNotFound = -32601
	MCPErrorInvalidParams  = -32602
	MCPErrorInternalError  = -32603
)

// Legacy types for backward compatibility with existing server implementations
// These will be deprecated in favor of SDK types

// MCPToolResponse represents an MCP tool response message (legacy compatibility)
type MCPToolResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
}

// MCPError represents an MCP protocol error (legacy compatibility)
type MCPError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Legacy request structure for backward compatibility
type LegacyMCPToolRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// MCPToolListResult contains the list of available tools (legacy compatibility)
type MCPToolListResult struct {
	Tools []MCPToolDefinition `json:"tools"`
}

// MCPToolCallParams represents parameters for a tool call (legacy compatibility)
type MCPToolCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// Hybrid search tool specific types

// HybridSearchRequest represents parameters for hybrid search tool
type HybridSearchRequest struct {
	Query             string            `json:"query"`
	TopK              int               `json:"top_k,omitempty"`
	Filters           map[string]string `json:"filters,omitempty"`
	SearchMode        string            `json:"search_mode,omitempty"`      // "hybrid", "s3vector", "opensearch"
	BM25Weight        float64           `json:"bm25_weight,omitempty"`      // Weight for BM25 scoring in hybrid mode
	VectorWeight      float64           `json:"vector_weight,omitempty"`    // Weight for vector scoring in hybrid mode
	MinScore          float64           `json:"min_score,omitempty"`        // Minimum score threshold
	IncludeMetadata   bool              `json:"include_metadata,omitempty"` // Include document metadata in results
	EnableSlackSearch bool              `json:"enable_slack_search,omitempty"`
	SlackChannels     []string          `json:"slack_channels,omitempty"`
}

// HybridSearchResponse represents the hybrid search tool response
type HybridSearchResponse struct {
	Query          string                    `json:"query"`
	Total          int                       `json:"total"`
	SearchMode     string                    `json:"search_mode"`
	SearchMethod   string                    `json:"search_method"`
	URLDetected    bool                      `json:"url_detected,omitempty"`
	FallbackReason string                    `json:"fallback_reason,omitempty"`
	Results        []HybridSearchResultItem  `json:"results"`
	Metadata       *HybridSearchMetadata     `json:"metadata,omitempty"`
	SlackResults   []HybridSearchSlackResult `json:"slack_results,omitempty"`
	SearchSources  []string                  `json:"search_sources,omitempty"`
}

// HybridSearchResultItem represents a single search result
type HybridSearchResultItem struct {
	ID        string                 `json:"id"`
	Title     string                 `json:"title"`
	Content   string                 `json:"content"`
	Score     float64                `json:"score"`  // Fused score after hybrid result combination
	Source    string                 `json:"source"` // "s3vector", "opensearch", "hybrid"
	Path      string                 `json:"path"`   // File path
	Category  string                 `json:"category,omitempty"`
	Tags      []string               `json:"tags,omitempty"`
	CreatedAt string                 `json:"created_at,omitempty"`
	UpdatedAt string                 `json:"updated_at,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// HybridSearchMetadata contains metadata about the search execution
type HybridSearchMetadata struct {
	S3VectorResults   int     `json:"s3_vector_results,omitempty"`
	OpenSearchResults int     `json:"opensearch_results,omitempty"`
	ExecutionTimeMs   int64   `json:"execution_time_ms,omitempty"`
	BM25Weight        float64 `json:"bm25_weight,omitempty"`
	VectorWeight      float64 `json:"vector_weight,omitempty"`
}

// HybridSearchSlackResult represents a Slack conversation snippet in the response
type HybridSearchSlackResult struct {
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
	User      string `json:"user"`
	Channel   string `json:"channel"`
	Permalink string `json:"permalink,omitempty"`
}

// InitializeRequest represents parameters for initialize tool
type InitializeRequest struct {
	ConfigPath string `json:"config_path,omitempty"`
}

// InitializeResponse represents the initialize tool response
type InitializeResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Config  struct {
		S3VectorEnabled    bool   `json:"s3_vector_enabled"`
		OpenSearchEnabled  bool   `json:"opensearch_enabled"`
		S3VectorIndex      string `json:"s3_vector_index,omitempty"`
		OpenSearchIndex    string `json:"opensearch_index,omitempty"`
		OpenSearchEndpoint string `json:"opensearch_endpoint,omitempty"`
	} `json:"config"`
}

// Helper functions for creating MCP messages (legacy compatibility)

// NewMCPToolResponse creates a new MCP tool response (legacy compatibility)
func NewMCPToolResponse(id interface{}, result interface{}) *MCPToolResponse {
	return &MCPToolResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
}

// NewMCPErrorResponse creates a new MCP error response (legacy compatibility)
func NewMCPErrorResponse(id interface{}, code int, message string, data interface{}) *MCPToolResponse {
	return &MCPToolResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &MCPError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}

// NewMCPContent creates a new MCP text content item using SDK types
func NewMCPContent(contentType, text string) *mcp.TextContent {
	// Note: SDK TextContent doesn't have a Type field as it's implicit
	// contentType parameter is kept for backward compatibility but not used
	return &mcp.TextContent{
		Text: text,
	}
}
