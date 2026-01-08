package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/ca-srg/ragent/internal/slacksearch"
	"github.com/ca-srg/ragent/internal/types"
	"github.com/google/jsonschema-go/jsonschema"
)

// SlackSearchToolAdapter adapts Slack search functionality to MCP tool interface
// This adapter is used in --only-slack mode where OpenSearch is not available
type SlackSearchToolAdapter struct {
	slackService  *slacksearch.SlackSearchService
	defaultConfig *SlackSearchConfig
	logger        *log.Logger
}

// SlackSearchConfig contains configuration for slack-only search
type SlackSearchConfig struct {
	DefaultMaxResults     int
	DefaultTimeoutSeconds int
}

// NewSlackSearchToolAdapter creates a new slack search tool adapter
func NewSlackSearchToolAdapter(slackService *slacksearch.SlackSearchService, config *SlackSearchConfig) *SlackSearchToolAdapter {
	if config == nil {
		config = &SlackSearchConfig{
			DefaultMaxResults:     20,
			DefaultTimeoutSeconds: 60,
		}
	}

	return &SlackSearchToolAdapter{
		slackService:  slackService,
		defaultConfig: config,
		logger:        log.New(log.Writer(), "[SlackSearchTool] ", log.LstdFlags),
	}
}

// GetToolDefinition returns the MCP tool definition for slack search
func (sta *SlackSearchToolAdapter) GetToolDefinition() types.MCPToolDefinition {
	schemaMap := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Search query text for Slack conversations",
			},
			"channels": map[string]interface{}{
				"type":        "array",
				"description": "Optional Slack channel names (without '#') to scope the search",
				"items": map[string]interface{}{
					"type":      "string",
					"minLength": 1,
				},
			},
			"max_results": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of results to return (1-100)",
				"minimum":     1,
				"maximum":     100,
				"default":     20,
			},
		},
		"required": []string{"query"},
	}

	var inputSchema *jsonschema.Schema
	schemaBytes, err := json.Marshal(schemaMap)
	if err == nil {
		inputSchema = &jsonschema.Schema{}
		_ = json.Unmarshal(schemaBytes, inputSchema)
	}

	return types.MCPToolDefinition{
		Name:        "slack_search",
		Description: "Search Slack workspace conversations. Returns messages and threads relevant to the query. Use this tool to find information from team discussions, announcements, and collaborative conversations.",
		InputSchema: inputSchema,
	}
}

// SlackSearchProgressCallback is a function that sends progress notifications during tool execution.
type SlackSearchProgressCallback func(progress, total float64, message string)

// HandleToolCall executes the slack search tool
func (sta *SlackSearchToolAdapter) HandleToolCall(ctx context.Context, params map[string]interface{}) (*types.MCPToolCallResult, error) {
	return sta.HandleToolCallWithProgress(ctx, params, nil)
}

// HandleToolCallWithProgress executes the slack search tool with optional progress notifications.
func (sta *SlackSearchToolAdapter) HandleToolCallWithProgress(ctx context.Context, params map[string]interface{}, progressFn SlackSearchProgressCallback) (*types.MCPToolCallResult, error) {
	sta.logger.Printf("Executing slack search tool with params: %+v", params)

	sendProgress := func(progress, total float64, message string) {
		if progressFn != nil {
			progressFn(progress, total, message)
		}
	}

	sendProgress(0.0, 1.0, "Validating parameters...")

	// Parse parameters
	request, err := sta.parseParams(params)
	if err != nil {
		return CreateToolCallErrorResult(fmt.Sprintf("Invalid parameters: %v", err)), err
	}

	if sta.slackService == nil {
		err := fmt.Errorf("slack search service not configured")
		return CreateToolCallErrorResult(err.Error()), err
	}

	sendProgress(0.1, 1.0, "Starting Slack search...")

	// Set up progress handler for Slack search iterations
	if progressFn != nil {
		sta.slackService.SetProgressHandler(func(iteration, maxIterations int) {
			slackProgress := 0.1 + (float64(iteration)/float64(maxIterations))*0.8
			sendProgress(slackProgress, 1.0, fmt.Sprintf("Slack search iteration %d/%d", iteration, maxIterations))
		})
	}

	// Execute search with timeout
	timeout := sta.defaultConfig.DefaultTimeoutSeconds
	if timeout <= 0 {
		timeout = 60
	}
	searchCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	startTime := time.Now()
	result, err := sta.slackService.Search(searchCtx, request.Query, request.Channels)

	// Clear progress handler
	if progressFn != nil {
		sta.slackService.SetProgressHandler(nil)
	}

	if err != nil {
		sta.logger.Printf("Slack search failed: %v", err)
		return CreateToolCallErrorResult(fmt.Sprintf("Slack search failed: %v", err)), err
	}

	sendProgress(0.95, 1.0, "Preparing response...")

	// Convert to MCP response
	response := sta.convertToMCPResponse(request, result, time.Since(startTime))
	responseJSON, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return CreateToolCallErrorResult(fmt.Sprintf("Failed to serialize response: %v", err)), err
	}

	sta.logger.Printf("Slack search completed - found %d results in %v", response.Total, time.Since(startTime))

	sendProgress(1.0, 1.0, "Search completed")

	return CreateToolCallResult(string(responseJSON)), nil
}

// parseParams extracts and validates parameters from MCP tool call
func (sta *SlackSearchToolAdapter) parseParams(params map[string]interface{}) (*types.SlackSearchRequest, error) {
	request := &types.SlackSearchRequest{
		MaxResults: sta.defaultConfig.DefaultMaxResults,
	}

	// Required query parameter
	if queryInterface, ok := params["query"]; ok {
		if query, ok := queryInterface.(string); ok {
			request.Query = strings.TrimSpace(query)
		} else {
			return nil, fmt.Errorf("query must be a string")
		}
	} else {
		return nil, fmt.Errorf("query parameter is required")
	}

	if request.Query == "" {
		return nil, fmt.Errorf("query cannot be empty")
	}

	// Optional channels parameter
	if channelsInterface, ok := params["channels"]; ok {
		channels := parseSlackChannelsParam(channelsInterface)
		if len(channels) > 0 {
			request.Channels = channels
		}
	}

	// Optional max_results parameter
	if maxResultsInterface, ok := params["max_results"]; ok {
		maxResults := parseIntParam(maxResultsInterface, sta.defaultConfig.DefaultMaxResults)
		if maxResults < 1 {
			maxResults = 1
		} else if maxResults > 100 {
			maxResults = 100
		}
		request.MaxResults = maxResults
	}

	return request, nil
}

// convertToMCPResponse converts SlackSearchResult to MCP response format
func (sta *SlackSearchToolAdapter) convertToMCPResponse(request *types.SlackSearchRequest, result *slacksearch.SlackSearchResult, execTime time.Duration) *types.SlackSearchResponse {
	response := &types.SlackSearchResponse{
		Query:   request.Query,
		Total:   0,
		Results: make([]types.SlackSearchResultItem, 0),
	}

	if result == nil {
		return response
	}

	response.Total = result.TotalMatches

	// Convert enriched messages to result items
	for _, msg := range result.EnrichedMessages {
		item := types.SlackSearchResultItem{
			Channel:   msg.OriginalMessage.Channel,
			Timestamp: msg.OriginalMessage.Timestamp,
			User:      selectUser(msg.OriginalMessage.User, msg.OriginalMessage.Username),
			Text:      strings.TrimSpace(msg.OriginalMessage.Text),
			Permalink: msg.Permalink,
		}

		// Add thread replies
		if len(msg.ThreadMessages) > 0 {
			item.ThreadReplies = make([]types.SlackThreadReplyItem, 0, len(msg.ThreadMessages))
			for _, reply := range msg.ThreadMessages {
				item.ThreadReplies = append(item.ThreadReplies, types.SlackThreadReplyItem{
					Timestamp: reply.Timestamp,
					User:      selectUser(reply.User, reply.Username),
					Text:      strings.TrimSpace(reply.Text),
				})
			}
		}

		response.Results = append(response.Results, item)
	}

	// Add metadata
	response.Metadata = &types.SlackSearchMetadata{
		ExecutionTimeMs: execTime.Milliseconds(),
		IterationCount:  result.IterationCount,
		QueriesUsed:     result.Queries,
		IsSufficient:    result.IsSufficient,
		MissingInfo:     result.MissingInfo,
	}

	return response
}

// SetDefaultConfig updates the default configuration
func (sta *SlackSearchToolAdapter) SetDefaultConfig(config *SlackSearchConfig) {
	sta.defaultConfig = config
}

// GetDefaultConfig returns the current default configuration
func (sta *SlackSearchToolAdapter) GetDefaultConfig() *SlackSearchConfig {
	return sta.defaultConfig
}

// Helper functions

func parseSlackChannelsParam(value interface{}) []string {
	switch v := value.(type) {
	case []string:
		return sanitizeChannelNames(v)
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if str, ok := item.(string); ok {
				result = append(result, str)
			}
		}
		return sanitizeChannelNames(result)
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return nil
		}
		parts := strings.Split(trimmed, ",")
		return sanitizeChannelNames(parts)
	default:
		return nil
	}
}

func sanitizeChannelNames(channels []string) []string {
	result := make([]string, 0, len(channels))
	for _, ch := range channels {
		ch = strings.TrimSpace(ch)
		ch = strings.TrimPrefix(ch, "#")
		if ch != "" {
			result = append(result, ch)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func parseIntParam(value interface{}, fallback int) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return int(n)
		}
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func selectUser(userID, username string) string {
	if strings.TrimSpace(username) != "" {
		return username
	}
	return userID
}
