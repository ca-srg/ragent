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
	assert.Contains(t, logs, "MCP planned tool: %s/%s")
}

func TestQueryWithRetryFallsBackToErrorRetryWhenInitialPlanIsEmpty(t *testing.T) {
	client := &fakeRetryClient{}
	planner := &scriptedRetryPlanner{responses: []string{
		`{"calls":[]}`,
		`{"calls":[{"server":"linear","tool":"get_issue","arguments":{"id":"ASICS-1"}}]}`,
	}}
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
	require.Len(t, planner.requests, 2)
	assert.Contains(t, logs, "MCP retrying tool: %s/%s")
}

func TestQueryWithRetryRequiresPlannerWhenClientConfigured(t *testing.T) {
	client := &fakeRetryClient{}

	result, err := QueryWithRetry(context.Background(), client, nil, "ASICS-1", nil)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "MCP retry planner is required")
	assert.Empty(t, client.calls)
}

func TestQueryWithRetryTreatsTypedNilClientAsDisabled(t *testing.T) {
	var client *Manager

	result, err := QueryWithRetry(context.Background(), client, nil, "ASICS-1", nil)

	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestQueryWithRetryRejectsRecursiveInitialPlanning(t *testing.T) {
	client := &fakeInitialPlanClient{}
	planner := &scriptedRetryPlanner{responses: []string{
		`{"calls":[{"server":"linear","tool":"list_teams","arguments":{"query":"ML"}}]}`,
	}}

	result, err := QueryWithRetry(WithRecursionDepth(context.Background(), maxRecursionDepth), client, planner, "ML", nil)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "recursion depth exceeded")
	assert.Empty(t, client.calls)
	assert.Empty(t, planner.requests)
}

func TestRequireRetryPlannerAllowsDisabledMCPClient(t *testing.T) {
	err := RequireRetryPlanner(nil, nil)

	require.NoError(t, err)
}

func TestRequireRetryPlannerAllowsTypedNilMCPClient(t *testing.T) {
	var client *Manager

	err := RequireRetryPlanner(client, nil)

	require.NoError(t, err)
}

func TestRequireRetryPlannerRejectsConfiguredMCPClientWithoutPlanner(t *testing.T) {
	err := RequireRetryPlanner(&fakeRetryClient{}, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "MCP retry planner is required")
}

func TestRequireRetryPlannerRejectsTypedNilPlanner(t *testing.T) {
	var planner *fakeRetryPlanner

	err := RequireRetryPlanner(&fakeRetryClient{}, planner)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "MCP retry planner is required")
}

func TestQueryWithRetryPlansInitialDependentMCPCalls(t *testing.T) {
	client := &fakeInitialPlanClient{}
	planner := &scriptedRetryPlanner{responses: []string{
		`{"calls":[{"server":"linear","tool":"list_teams","arguments":{"query":"ML"},"reason":"find the team first"}]}`,
		`{"calls":[{"server":"linear","tool":"list_projects","arguments":{"teamId":"team-1"},"reason":"list projects for the resolved team"}]}`,
		`{"calls":[]}`,
	}}

	result, err := QueryWithRetry(context.Background(), client, planner, "ML チームの project を確認", nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Errors)
	require.Len(t, result.Results, 2)
	assert.Equal(t, "list_teams", result.Results[0].Tool)
	assert.Contains(t, result.Results[0].Text, "team-1")
	assert.Equal(t, "list_projects", result.Results[1].Tool)
	assert.Contains(t, result.Results[1].Text, "charge_daily")
	assert.Equal(t, 0, client.queryCalls, "initial planning should replace blind query-compatible auto calls")
	require.Len(t, client.calls, 2)
	assert.Equal(t, "list_teams", client.calls[0].Tool)
	assert.Equal(t, "ML", client.calls[0].Arguments["query"])
	assert.Equal(t, "list_projects", client.calls[1].Tool)
	assert.Equal(t, "team-1", client.calls[1].Arguments["teamId"])
	require.Len(t, planner.requests, 3)
	assert.Contains(t, planner.requests[1], "team-1", "second planning round should see first tool result")
}

func TestQueryWithRetrySkipsInitialPlanningForEmptyQuery(t *testing.T) {
	client := &fakeInitialPlanClient{}
	planner := &scriptedRetryPlanner{responses: []string{
		`{"calls":[{"server":"linear","tool":"list_teams","arguments":{"query":"ML"}}]}`,
	}}

	result, err := QueryWithRetry(context.Background(), client, planner, "   ", nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 1, client.queryCalls)
	assert.Empty(t, client.calls)
	assert.Empty(t, planner.requests)
}

func TestQueryWithRetryFallsBackWhenInitialPlanningProducesNoResults(t *testing.T) {
	client := &fakeNoResultInitialPlanClient{}
	planner := &scriptedRetryPlanner{responses: []string{
		`{"calls":[{"server":"linear","tool":"list_teams","arguments":{"query":"ML"}}]}`,
	}}

	result, err := QueryWithRetry(context.Background(), client, planner, "ML チーム", nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Results, 1)
	assert.Equal(t, "fallback query context", result.Results[0].Text)
	assert.Equal(t, 1, client.queryCalls)
	require.Len(t, client.calls, 1)
}

func TestQueryWithRetryDoesNotExecuteNonReadOnlyPlannedTool(t *testing.T) {
	client := &fakeMutatingPlanClient{}
	planner := &scriptedRetryPlanner{responses: []string{
		`{"calls":[{"server":"linear","tool":"create_issue","arguments":{"title":"do not create"}}]}`,
	}}

	result, err := QueryWithRetry(context.Background(), client, planner, "create issue", nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.Results, 1)
	assert.Equal(t, "fallback query context", result.Results[0].Text)
	assert.Equal(t, 1, client.queryCalls)
	assert.Empty(t, client.calls)
}

func TestQueryWithRetryLimitsInitialPlanningCallsAcrossRounds(t *testing.T) {
	client := &fakeInitialPlanClient{}
	planner := &scriptedRetryPlanner{responses: []string{
		`{"calls":[{"server":"linear","tool":"list_teams","arguments":{"query":"ML"}}]}`,
		`{"calls":[{"server":"linear","tool":"list_projects","arguments":{"teamId":"team-1"}}]}`,
		`{"calls":[{"server":"linear","tool":"list_teams","arguments":{"query":"CBA"}}]}`,
		`{"calls":[{"server":"linear","tool":"list_projects","arguments":{"teamId":"team-2"}}]}`,
	}}

	result, err := QueryWithRetry(context.Background(), client, planner, "projects", nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, client.calls, 3)
	assert.Len(t, result.Results, 3)
	assert.Equal(t, 3, len(planner.requests), "planning should stop once the total planned call budget is exhausted")
}

func TestQueryWithRetryRespectsMaxToolsForInitialPlanningCalls(t *testing.T) {
	client := &fakeInitialPlanClient{maxTools: 2}
	planner := &scriptedRetryPlanner{responses: []string{
		`{"calls":[{"server":"linear","tool":"list_teams","arguments":{"query":"ML"}}]}`,
		`{"calls":[{"server":"linear","tool":"list_projects","arguments":{"teamId":"team-1"}}]}`,
		`{"calls":[{"server":"linear","tool":"list_teams","arguments":{"query":"CBA"}}]}`,
	}}

	result, err := QueryWithRetry(context.Background(), client, planner, "projects", nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, client.calls, 2)
	assert.Len(t, result.Results, 2)
	assert.Equal(t, 2, len(planner.requests), "initial planning should stop once maxTools is exhausted")
}

func TestInitialPlanningCallLimitUsesManagerMaxTools(t *testing.T) {
	assert.Equal(t, 2, initialPlanningCallLimit(&Manager{maxTools: 2}))
	assert.Equal(t, maxInitialPlanningCalls, initialPlanningCallLimit(&Manager{maxTools: 8}))
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
		ReadOnly:    true,
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

type fakeInitialPlanClient struct {
	queryCalls int
	calls      []ToolCall
	maxTools   int
}

func (c *fakeInitialPlanClient) Query(context.Context, string) (*QueryResult, error) {
	c.queryCalls++
	return &QueryResult{}, nil
}

func (c *fakeInitialPlanClient) AvailableTools() []ToolInfo {
	return []ToolInfo{
		{
			Server:      "linear",
			Name:        "list_teams",
			Description: "List Linear teams",
			ReadOnly:    true,
			InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`),
		},
		{
			Server:      "linear",
			Name:        "list_projects",
			Description: "List Linear projects",
			ReadOnly:    true,
			InputSchema: json.RawMessage(`{"type":"object","properties":{"teamId":{"type":"string"}},"required":["teamId"]}`),
		},
	}
}

func (c *fakeInitialPlanClient) CallTool(_ context.Context, call ToolCall) (ToolResult, error) {
	c.calls = append(c.calls, call)
	switch call.Tool {
	case "list_teams":
		return ToolResult{Server: call.Server, Tool: call.Tool, Text: `{"nodes":[{"id":"team-1","name":"ML"}]}`}, nil
	case "list_projects":
		return ToolResult{Server: call.Server, Tool: call.Tool, Text: `{"nodes":[{"id":"proj-1","name":"charge_daily 決済分類見直し"}]}`}, nil
	default:
		return ToolResult{Server: call.Server, Tool: call.Tool, Text: `{}`}, nil
	}
}

func (c *fakeInitialPlanClient) maxToolCalls() int {
	return c.maxTools
}

type fakeNoResultInitialPlanClient struct {
	queryCalls int
	calls      []ToolCall
}

func (c *fakeNoResultInitialPlanClient) Query(context.Context, string) (*QueryResult, error) {
	c.queryCalls++
	return &QueryResult{Results: []ToolResult{{Server: "linear", Tool: "fallback", Text: "fallback query context"}}}, nil
}

func (c *fakeNoResultInitialPlanClient) AvailableTools() []ToolInfo {
	return []ToolInfo{{
		Server:      "linear",
		Name:        "list_teams",
		Description: "List Linear teams",
		ReadOnly:    true,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`),
	}}
}

func (c *fakeNoResultInitialPlanClient) CallTool(_ context.Context, call ToolCall) (ToolResult, error) {
	c.calls = append(c.calls, call)
	return ToolResult{Server: call.Server, Tool: call.Tool}, nil
}

type fakeMutatingPlanClient struct {
	queryCalls int
	calls      []ToolCall
}

func (c *fakeMutatingPlanClient) Query(context.Context, string) (*QueryResult, error) {
	c.queryCalls++
	return &QueryResult{Results: []ToolResult{{Server: "linear", Tool: "fallback", Text: "fallback query context"}}}, nil
}

func (c *fakeMutatingPlanClient) AvailableTools() []ToolInfo {
	return []ToolInfo{{
		Server:      "linear",
		Name:        "create_issue",
		Description: "Create a Linear issue",
		ReadOnly:    false,
		InputSchema: json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"}},"required":["title"]}`),
	}}
}

func (c *fakeMutatingPlanClient) CallTool(_ context.Context, call ToolCall) (ToolResult, error) {
	c.calls = append(c.calls, call)
	return ToolResult{Server: call.Server, Tool: call.Tool, Text: `{"created":true}`}, nil
}

type scriptedRetryPlanner struct {
	responses []string
	requests  []string
}

func (p *scriptedRetryPlanner) GenerateChatResponse(_ context.Context, messages []bedrock.ChatMessage) (string, error) {
	if len(messages) > 0 {
		p.requests = append(p.requests, messages[len(messages)-1].Content)
	}
	if len(p.responses) == 0 {
		return `{"calls":[]}`, nil
	}
	response := p.responses[0]
	p.responses = p.responses[1:]
	return response, nil
}
