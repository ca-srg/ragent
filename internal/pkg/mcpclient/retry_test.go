package mcpclient

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ca-srg/ragent/internal/pkg/embedding/bedrock"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryCompatibleToolRequiresQueryProperty(t *testing.T) {
	assert.True(t, queryCompatibleTool(&mcp.Tool{InputSchema: &jsonschema.Schema{
		Properties: map[string]*jsonschema.Schema{"query": {}},
		Required:   []string{"query"},
	}}, ServerConfig{}))

	assert.False(t, queryCompatibleTool(&mcp.Tool{InputSchema: &jsonschema.Schema{
		Properties: map[string]*jsonschema.Schema{"id": {}},
		Required:   []string{"id"},
	}}, ServerConfig{}))

	assert.True(t, queryCompatibleTool(&mcp.Tool{InputSchema: &jsonschema.Schema{
		Properties: map[string]*jsonschema.Schema{"query": {}, "team": {}},
		Required:   []string{"query", "team"},
	}}, ServerConfig{Arguments: map[string]any{"team": "ASICS"}}))
}

func TestQueryWithRetryClearsRecoveredWarnings(t *testing.T) {
	client := &fakeRetryClient{}
	planner := &fakeRetryPlanner{response: `{"calls":[{"server":"linear","tool":"get_issue","arguments":{"id":"ASICS-1"}}]}`}
	var logs []string

	result, err := QueryWithRetry(context.Background(), client, planner, "ASICS-1", func(format string, args ...any) {
		logs = append(logs, format)
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Errors)
	require.Len(t, result.Results, 1)
	assert.Equal(t, "issue details", result.Results[0].Text)
	require.Len(t, client.calls, 1)
	assert.Equal(t, "get_issue", client.calls[0].Tool)
	assert.Contains(t, logs, "MCP retrying tool: %s/%s")
}

type fakeRetryClient struct {
	calls []ToolCall
}

func (c *fakeRetryClient) Query(context.Context, string) (*QueryResult, error) {
	return &QueryResult{Errors: []string{"MCP linear/get_issue returned error: missing id"}}, nil
}

func (c *fakeRetryClient) AvailableTools() []ToolInfo {
	return []ToolInfo{{
		Server:      "linear",
		Name:        "get_issue",
		Description: "Get issue by id",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`),
	}}
}

func (c *fakeRetryClient) CallTool(_ context.Context, call ToolCall) (ToolResult, error) {
	c.calls = append(c.calls, call)
	return ToolResult{Server: call.Server, Tool: call.Tool, Text: "issue details"}, nil
}

type fakeRetryPlanner struct {
	response string
}

func (p *fakeRetryPlanner) GenerateChatResponse(context.Context, []bedrock.ChatMessage) (string, error) {
	return p.response, nil
}
