package query

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	appconfig "github.com/ca-srg/ragent/internal/pkg/config"
	"github.com/ca-srg/ragent/internal/pkg/embedding"
	"github.com/ca-srg/ragent/internal/pkg/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/pkg/mcpclient"
	"github.com/ca-srg/ragent/internal/query/search"
)

func TestGenerateChatResponseRetriesMCPWarningWithPlannedToolCall(t *testing.T) {
	origNewHybridSearchServiceFunc := NewHybridSearchServiceFunc
	t.Cleanup(func() { NewHybridSearchServiceFunc = origNewHybridSearchServiceFunc })
	NewHybridSearchServiceFunc = func(_ *appconfig.Config, _ embedding.EmbeddingClient) (HybridSearchInitializer, error) {
		return fakeChatSearchService{}, nil
	}

	chatClient := &fakeMCPRetryChatResponder{}
	mcpClient := &fakeMCPRetryClient{}
	result, err := GenerateChatResponse("lookup TEAM-1", nil, chatClient, fakeChatEmbeddingClient{}, &appconfig.Config{OpenSearchIndex: "docs"}, aws.Config{}, false, ChatOptions{
		ContextSize:  3,
		BM25Weight:   0.5,
		VectorWeight: 0.5,
		MCPClient:    mcpClient,
	})
	if err != nil {
		t.Fatalf("GenerateChatResponse returned error: %v", err)
	}
	if result.Response != "final answer" {
		t.Fatalf("expected final answer, got %q", result.Response)
	}
	if len(chatClient.calls) != 2 {
		t.Fatalf("expected retry planning and final response calls, got %d", len(chatClient.calls))
	}
	if len(mcpClient.calls) != 1 {
		t.Fatalf("expected one planned MCP retry call, got %d", len(mcpClient.calls))
	}

	planPrompt := chatClient.calls[0][1].Content
	if !strings.Contains(planPrompt, "Available MCP tools") || !strings.Contains(planPrompt, "missing id") {
		t.Fatalf("retry planning prompt missing tool list or warning: %s", planPrompt)
	}

	call := mcpClient.calls[0]
	if call.Server != "docs" || call.Tool != "lookup" || call.Arguments["id"] != "TEAM-1" {
		t.Fatalf("unexpected MCP retry call: %#v", call)
	}

	finalPrompt := chatClient.calls[1][len(chatClient.calls[1])-1].Content
	if !strings.Contains(finalPrompt, "resolved context from MCP retry") {
		t.Fatalf("final prompt did not include MCP retry result: %s", finalPrompt)
	}
}

type fakeMCPRetryChatResponder struct {
	calls [][]bedrock.ChatMessage
}

func (r *fakeMCPRetryChatResponder) GenerateChatResponse(_ context.Context, messages []bedrock.ChatMessage) (string, error) {
	r.calls = append(r.calls, append([]bedrock.ChatMessage(nil), messages...))
	if len(r.calls) == 1 {
		return `{"calls":[{"server":"docs","tool":"lookup","arguments":{"id":"TEAM-1"},"reason":"retry with id"}]}`, nil
	}
	return "final answer", nil
}

type fakeMCPRetryClient struct {
	calls []mcpclient.ToolCall
}

func (c *fakeMCPRetryClient) Query(_ context.Context, _ string) (*mcpclient.QueryResult, error) {
	return &mcpclient.QueryResult{Errors: []string{"MCP docs/lookup returned error: missing id"}}, nil
}

func (c *fakeMCPRetryClient) AvailableTools() []mcpclient.ToolInfo {
	return []mcpclient.ToolInfo{{
		Server:      "docs",
		Name:        "lookup",
		Description: "Lookup docs by id",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`),
	}}
}

func (c *fakeMCPRetryClient) CallTool(_ context.Context, call mcpclient.ToolCall) (mcpclient.ToolResult, error) {
	c.calls = append(c.calls, call)
	return mcpclient.ToolResult{Server: call.Server, Tool: call.Tool, Text: "resolved context from MCP retry"}, nil
}

type fakeChatSearchService struct{}

func (fakeChatSearchService) Initialize(_ context.Context) error { return nil }

func (fakeChatSearchService) Search(_ context.Context, _ *search.SearchRequest) (*search.SearchResponse, error) {
	return &search.SearchResponse{ContextParts: []string{"document context"}, References: map[string]string{}}, nil
}

type fakeChatEmbeddingClient struct{}

func (fakeChatEmbeddingClient) GenerateEmbedding(_ context.Context, _ string) ([]float64, error) {
	return []float64{0.1}, nil
}

func (fakeChatEmbeddingClient) ValidateConnection(_ context.Context) error { return nil }

func (fakeChatEmbeddingClient) GetModelInfo() (string, int, error) { return "fake", 1, nil }
