package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ca-srg/ragent/internal/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/metrics"
	"github.com/ca-srg/ragent/internal/opensearch"
	"github.com/ca-srg/ragent/internal/slacksearch"
	"github.com/ca-srg/ragent/internal/types"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// HybridSearchHandler wraps HybridSearchToolAdapter for SDK compatibility
// Maintains identical search functionality while providing SDK tool handler interface
type HybridSearchHandler struct {
	adapter *HybridSearchToolAdapter
}

// NewHybridSearchHandler creates a new SDK-compatible hybrid search handler
func NewHybridSearchHandler(osClient *opensearch.Client, embeddingClient *bedrock.BedrockClient, config *HybridSearchConfig, slackService *slacksearch.SlackSearchService) *HybridSearchHandler {
	adapter := NewHybridSearchToolAdapter(osClient, embeddingClient, config, slackService)

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
// Converts SDK request to RAGent format, executes search, and converts response back.
// If the client provides a progressToken, progress notifications are sent during execution.
func (hsh *HybridSearchHandler) HandleSDKToolCall(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Record MCP tool invocation for statistics
	metrics.RecordInvocation(metrics.ModeMCP)

	toolName := ""
	rawArguments := ""
	if req != nil && req.Params != nil {
		toolName = req.Params.Name
		if req.Params.Arguments != nil {
			rawArguments = string(req.Params.Arguments)
		}
	}

	ctx, span := mcpTracer.Start(ctx, "mcpserver.hybrid_search")
	defer span.End()

	metricAttrs := make([]attribute.KeyValue, 0, 8)
	start := time.Now()
	errType := ""
	defer func() {
		recordMCPMetrics(ctx, metricAttrs, time.Since(start), errType)
	}()

	if toolName != "" {
		span.SetAttributes(attribute.String("mcp.tool.name", toolName))
		metricAttrs = append(metricAttrs, attribute.String("mcp.tool.name", toolName))
	}
	if rawArguments != "" {
		span.SetAttributes(attribute.String("mcp.request.arguments", truncateForAttribute(rawArguments)))
	}

	if method := getAuthMethodFromContext(ctx); method != "" {
		span.SetAttributes(attribute.String("mcp.auth.method", method))
		metricAttrs = append(metricAttrs, attribute.String("mcp.auth.method", method))
	}
	if clientIP := getClientIPFromContext(ctx); clientIP != "" {
		span.SetAttributes(attribute.String("mcp.client.ip", clientIP))
		metricAttrs = append(metricAttrs, attribute.String("mcp.client.ip", clientIP))
	}

	// Convert SDK request parameters to RAGent format
	params := make(map[string]interface{})
	if req.Params.Arguments != nil {
		if err := json.Unmarshal(req.Params.Arguments, &params); err != nil {
			errType = "invalid_arguments"
			span.RecordError(err)
			span.SetStatus(codes.Error, "invalid_arguments")
			return nil, fmt.Errorf("failed to unmarshal tool arguments: %w", err)
		}
	}
	annotateSpanWithRequest(span, params)
	if mode, ok := params["search_mode"].(string); ok && mode != "" {
		metricAttrs = append(metricAttrs, attribute.String("mcp.search.mode", mode))
	}
	if topK := extractInt(params["top_k"]); topK > 0 {
		metricAttrs = append(metricAttrs, attribute.Int("mcp.search.top_k", topK))
	}
	if enableSlack, ok := extractBoolValue(params["enable_slack_search"]); ok {
		metricAttrs = append(metricAttrs, attribute.Bool("mcp.search.enable_slack", enableSlack))
	}
	if channelFilters := extractStringSliceLen(params["slack_channels"]); channelFilters > 0 {
		metricAttrs = append(metricAttrs, attribute.Int("mcp.search.slack_channel_filters", channelFilters))
	}

	// Create progress callback if client requested progress notifications
	var progressFn ProgressCallback
	if req != nil && req.Params != nil {
		if token := req.Params.GetProgressToken(); token != nil {
			span.SetAttributes(attribute.Bool("mcp.progress.enabled", true))
			progressFn = func(progress, total float64, message string) {
				if req.Session == nil {
					return
				}
				notifyParams := &mcp.ProgressNotificationParams{
					ProgressToken: token,
					Progress:      progress,
					Total:         total,
					Message:       message,
				}
				// Ignore error - progress notifications are best-effort
				_ = req.Session.NotifyProgress(ctx, notifyParams)
			}
		}
	}

	// Execute using existing adapter with progress callback
	ragentResult, err := hsh.adapter.HandleToolCallWithProgress(ctx, params, progressFn)
	if err != nil {
		errType = "tool_call_failed"
		span.RecordError(err)
		span.SetStatus(codes.Error, "tool_call_failed")
		return nil, err
	}
	hybridResponse := annotateSpanWithResult(span, ragentResult)
	if hybridResponse != nil {
		metricAttrs = append(metricAttrs,
			attribute.Int("mcp.results.total", hybridResponse.Total),
			attribute.String("mcp.search.method", hybridResponse.SearchMethod),
		)
		if hybridResponse.FallbackReason != "" {
			metricAttrs = append(metricAttrs, attribute.String("mcp.search.fallback_reason", hybridResponse.FallbackReason))
		}
		if len(hybridResponse.SlackResults) > 0 {
			metricAttrs = append(metricAttrs, attribute.Int("mcp.search.slack_results", len(hybridResponse.SlackResults)))
		}
	}
	if ragentResult != nil {
		metricAttrs = append(metricAttrs, attribute.Bool("mcp.result.is_error", ragentResult.IsError))
		if ragentResult.IsError && errType == "" {
			errType = "tool_result_error"
		}
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

func getAuthMethodFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if method, ok := ctx.Value(authMethodContextKey).(string); ok {
		return method
	}
	if method, ok := ctx.Value(authMethodContextKey).(AuthMethod); ok {
		return string(method)
	}
	return ""
}

func getClientIPFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if clientIP, ok := ctx.Value(clientIPContextKey).(string); ok {
		return clientIP
	}
	return ""
}

func annotateSpanWithRequest(span trace.Span, params map[string]interface{}) {
	if span == nil || params == nil {
		return
	}

	if query, ok := params["query"].(string); ok && query != "" {
		span.SetAttributes(attribute.String("mcp.query", truncateForAttribute(query)))
	}

	if mode, ok := params["search_mode"].(string); ok && mode != "" {
		span.SetAttributes(attribute.String("mcp.search.mode", mode))
	}

	if topK := extractInt(params["top_k"]); topK > 0 {
		span.SetAttributes(attribute.Int("mcp.search.top_k", topK))
	}

	if enableSlack, ok := extractBoolValue(params["enable_slack_search"]); ok {
		span.SetAttributes(attribute.Bool("mcp.search.enable_slack", enableSlack))
	}
	if channelFilters := extractStringSliceLen(params["slack_channels"]); channelFilters > 0 {
		span.SetAttributes(attribute.Int("mcp.search.slack_channel_filters", channelFilters))
	}

	if weight := extractFloat(params["bm25_weight"]); weight >= 0 {
		span.SetAttributes(attribute.Float64("mcp.search.bm25_weight", weight))
	}
	if weight := extractFloat(params["vector_weight"]); weight >= 0 {
		span.SetAttributes(attribute.Float64("mcp.search.vector_weight", weight))
	}
}

func annotateSpanWithResult(span trace.Span, result *types.MCPToolCallResult) *types.HybridSearchResponse {
	if span == nil || result == nil {
		return nil
	}

	span.SetAttributes(attribute.Bool("mcp.result.is_error", result.IsError))
	if len(result.Content) == 0 || result.Content[0].Text == "" {
		return nil
	}

	var response types.HybridSearchResponse
	if err := json.Unmarshal([]byte(result.Content[0].Text), &response); err != nil {
		return nil
	}

	span.SetAttributes(
		attribute.Int("mcp.search.total", response.Total),
		attribute.String("mcp.search.method", response.SearchMethod),
	)

	if response.SearchMode != "" {
		span.SetAttributes(attribute.String("mcp.search.mode", response.SearchMode))
	}
	if response.FallbackReason != "" {
		span.SetAttributes(attribute.String("mcp.search.fallback_reason", response.FallbackReason))
	}
	span.SetAttributes(attribute.Int("mcp.search.results.returned", len(response.Results)))
	span.SetAttributes(attribute.Bool("mcp.search.url_detected", response.URLDetected))
	if len(response.SlackResults) > 0 {
		span.SetAttributes(attribute.Int("mcp.search.slack_results", len(response.SlackResults)))
	}
	if len(response.SearchSources) > 0 {
		span.SetAttributes(attribute.String("mcp.search.sources", strings.Join(response.SearchSources, ",")))
	}
	return &response
}

func truncateForAttribute(input string) string {
	const maxAttributeLength = 120
	trimmed := strings.TrimSpace(input)
	if len([]rune(trimmed)) <= maxAttributeLength {
		return trimmed
	}
	runes := []rune(trimmed)
	return string(runes[:maxAttributeLength]) + "…"
}

func extractInt(value interface{}) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		if parsed, err := v.Int64(); err == nil {
			return int(parsed)
		}
	case string:
		if parsed, err := strconv.Atoi(v); err == nil {
			return parsed
		}
	}
	return 0
}

func extractFloat(value interface{}) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case json.Number:
		if parsed, err := v.Float64(); err == nil {
			return parsed
		}
	case string:
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			return parsed
		}
	}
	return -1
}

func extractBoolValue(value interface{}) (bool, bool) {
	switch v := value.(type) {
	case bool:
		return v, true
	case string:
		if parsed, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			return parsed, true
		}
	case json.Number:
		if parsed, err := strconv.ParseBool(v.String()); err == nil {
			return parsed, true
		}
	}
	return false, false
}

func extractStringSliceLen(value interface{}) int {
	switch v := value.(type) {
	case []string:
		return len(v)
	case []interface{}:
		return len(v)
	case string:
		if strings.TrimSpace(v) == "" {
			return 0
		}
		return len(strings.Split(v, ","))
	default:
		return 0
	}
}
