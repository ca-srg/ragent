package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ca-srg/ragent/internal/metrics"
	"github.com/ca-srg/ragent/internal/slacksearch"
	"github.com/ca-srg/ragent/internal/types"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// SlackSearchHandler wraps SlackSearchToolAdapter for SDK compatibility
// This handler is used in --only-slack mode for MCP server
type SlackSearchHandler struct {
	adapter *SlackSearchToolAdapter
}

// NewSlackSearchHandler creates a new SDK-compatible slack search handler
func NewSlackSearchHandler(slackService *slacksearch.SlackSearchService, config *SlackSearchConfig) *SlackSearchHandler {
	adapter := NewSlackSearchToolAdapter(slackService, config)

	return &SlackSearchHandler{
		adapter: adapter,
	}
}

// NewSlackSearchHandlerFromAdapter creates handler from existing adapter
func NewSlackSearchHandlerFromAdapter(adapter *SlackSearchToolAdapter) *SlackSearchHandler {
	return &SlackSearchHandler{
		adapter: adapter,
	}
}

// HandleSDKToolCall implements SDK tool handler interface
func (ssh *SlackSearchHandler) HandleSDKToolCall(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

	ctx, span := mcpTracer.Start(ctx, "mcpserver.slack_search")
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

	// Convert SDK request parameters to internal format
	params := make(map[string]interface{})
	if req.Params.Arguments != nil {
		if err := json.Unmarshal(req.Params.Arguments, &params); err != nil {
			errType = "invalid_arguments"
			span.RecordError(err)
			span.SetStatus(codes.Error, "invalid_arguments")
			return nil, fmt.Errorf("failed to unmarshal tool arguments: %w", err)
		}
	}

	// Annotate span with request parameters
	if query, ok := params["query"].(string); ok && query != "" {
		span.SetAttributes(attribute.String("mcp.query", truncateForAttribute(query)))
	}
	if channels := extractStringSliceLen(params["channels"]); channels > 0 {
		span.SetAttributes(attribute.Int("mcp.search.channel_filters", channels))
		metricAttrs = append(metricAttrs, attribute.Int("mcp.search.channel_filters", channels))
	}
	if maxResults := extractInt(params["max_results"]); maxResults > 0 {
		span.SetAttributes(attribute.Int("mcp.search.max_results", maxResults))
		metricAttrs = append(metricAttrs, attribute.Int("mcp.search.max_results", maxResults))
	}

	// Create progress callback if client requested progress notifications
	var progressFn SlackSearchProgressCallback
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
				_ = req.Session.NotifyProgress(ctx, notifyParams)
			}
		}
	}

	// Execute using adapter with progress callback
	result, err := ssh.adapter.HandleToolCallWithProgress(ctx, params, progressFn)
	if err != nil {
		errType = "tool_call_failed"
		span.RecordError(err)
		span.SetStatus(codes.Error, "tool_call_failed")
		return nil, err
	}

	// Annotate span with result
	annotateSlackSearchSpanWithResult(span, result)
	if result != nil {
		metricAttrs = append(metricAttrs, attribute.Bool("mcp.result.is_error", result.IsError))
		if result.IsError && errType == "" {
			errType = "tool_result_error"
		}
	}

	span.SetStatus(codes.Ok, "slack_search_completed")

	// Convert result to SDK format
	return convertRAGentResultToSDK(result), nil
}

// GetSDKToolDefinition returns SDK-compatible tool definition
func (ssh *SlackSearchHandler) GetSDKToolDefinition() *mcp.Tool {
	ragentDef := ssh.adapter.GetToolDefinition()
	return convertRAGentToolToSDK(ragentDef)
}

// GetAdapter returns the underlying SlackSearchToolAdapter
func (ssh *SlackSearchHandler) GetAdapter() *SlackSearchToolAdapter {
	return ssh.adapter
}

// SetDefaultConfig sets default configuration for the handler
func (ssh *SlackSearchHandler) SetDefaultConfig(config *SlackSearchConfig) {
	ssh.adapter.SetDefaultConfig(config)
}

// GetDefaultConfig gets default configuration from the handler
func (ssh *SlackSearchHandler) GetDefaultConfig() *SlackSearchConfig {
	return ssh.adapter.GetDefaultConfig()
}

// annotateSlackSearchSpanWithResult adds result attributes to span
func annotateSlackSearchSpanWithResult(span interface{ SetAttributes(...attribute.KeyValue) }, result *types.MCPToolCallResult) {
	if span == nil || result == nil {
		return
	}

	span.SetAttributes(attribute.Bool("mcp.result.is_error", result.IsError))

	if len(result.Content) == 0 || result.Content[0].Text == "" {
		return
	}

	var response types.SlackSearchResponse
	if err := json.Unmarshal([]byte(result.Content[0].Text), &response); err != nil {
		return
	}

	span.SetAttributes(
		attribute.Int("mcp.search.total", response.Total),
		attribute.Int("mcp.search.results.returned", len(response.Results)),
	)

	if response.Metadata != nil {
		span.SetAttributes(
			attribute.Int64("mcp.search.execution_ms", response.Metadata.ExecutionTimeMs),
			attribute.Int("mcp.search.iteration_count", response.Metadata.IterationCount),
			attribute.Bool("mcp.search.is_sufficient", response.Metadata.IsSufficient),
		)
	}
}

// BuildSlackSearchToolDefinition creates enriched tool definition for MCP clients
func BuildSlackSearchToolDefinition(base *mcp.Tool, toolName string, defaults *SlackSearchConfig) *mcp.Tool {
	if defaults == nil {
		defaults = &SlackSearchConfig{
			DefaultMaxResults:     20,
			DefaultTimeoutSeconds: 60,
		}
	}

	var toolCopy mcp.Tool
	if base != nil {
		toolCopy = *base
	}
	toolCopy.Name = toolName

	toolCopy.Description = fmt.Sprintf(
		"Slack ワークスペースの会話を検索します。クエリに関連するメッセージとスレッドを最大 %d 件返します。チームの議論、アナウンス、共同作業の会話から情報を見つけるために使用してください。\n\nEnglish: Search Slack workspace conversations. Returns up to %d messages and threads relevant to the query. Use this tool to find information from team discussions, announcements, and collaborative conversations.",
		defaults.DefaultMaxResults,
		defaults.DefaultMaxResults,
	)

	var schema *jsonschema.Schema
	if base != nil && base.InputSchema != nil {
		schema = base.InputSchema.CloneSchemas()
	} else {
		schema = &jsonschema.Schema{}
	}

	schema.Type = "object"
	schema.Title = "Slack Search Parameters / Slack 検索パラメータ"
	schema.Description = "Slack 検索ツールに渡すパラメータ一覧です。最低限 `query` を指定してください。"
	schema.Required = []string{"query"}

	if schema.Properties == nil {
		schema.Properties = make(map[string]*jsonschema.Schema)
	}

	toRaw := func(v any) json.RawMessage {
		data, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		return data
	}

	// Query property
	queryProp := &jsonschema.Schema{
		Type:        "string",
		Title:       "Query / クエリ",
		Description: "検索対象の質問やキーワードを自然文で入力します。",
	}
	minQueryLen := 1
	queryProp.MinLength = &minQueryLen
	queryProp.Examples = []any{
		"先週のリリースについて教えて",
		"デプロイ手順を確認したい",
	}

	// Channels property
	channelsProp := &jsonschema.Schema{
		Type:        "array",
		Title:       "Channels / チャンネル",
		Description: "検索対象のチャンネル名（# なし）を指定します。省略時は全チャンネルを検索します。",
	}
	channelsProp.Items = &jsonschema.Schema{
		Type:      "string",
		MinLength: &minQueryLen,
	}
	channelsProp.Examples = []any{
		[]string{"general", "engineering"},
	}

	// Max results property
	maxResultsProp := &jsonschema.Schema{
		Type:        "integer",
		Title:       "Max Results / 最大結果数",
		Description: fmt.Sprintf("返却するメッセージの最大数（1〜100、デフォルト: %d）", defaults.DefaultMaxResults),
	}
	minResults := float64(1)
	maxResults := float64(100)
	maxResultsProp.Minimum = &minResults
	maxResultsProp.Maximum = &maxResults
	maxResultsProp.Default = toRaw(defaults.DefaultMaxResults)
	maxResultsProp.Examples = []any{10, defaults.DefaultMaxResults, 50}

	schema.Properties["query"] = queryProp
	schema.Properties["channels"] = channelsProp
	schema.Properties["max_results"] = maxResultsProp

	schema.Examples = []any{
		map[string]any{
			"query": "本番環境の障害対応手順",
		},
		map[string]any{
			"query":       "リリースノート",
			"channels":    []string{"release-notes", "announcements"},
			"max_results": 30,
		},
	}

	toolCopy.InputSchema = schema
	return &toolCopy
}
