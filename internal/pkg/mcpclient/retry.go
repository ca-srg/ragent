package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ca-srg/ragent/internal/pkg/embedding/bedrock"
)

const maxRetryCalls = 3

// RetryChatClient plans follow-up MCP calls from tool schemas and previous errors.
type RetryChatClient interface {
	GenerateChatResponse(ctx context.Context, messages []bedrock.ChatMessage) (string, error)
}

// RetryClient is the MCP client surface needed by QueryWithRetry.
type RetryClient interface {
	Query(ctx context.Context, query string) (*QueryResult, error)
	AvailableTools() []ToolInfo
	CallTool(ctx context.Context, call ToolCall) (ToolResult, error)
}

type retryPlan struct {
	Calls []retryCall `json:"calls"`
}

type retryCall struct {
	Server    string         `json:"server"`
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments"`
	Reason    string         `json:"reason,omitempty"`
}

type retryPlanningState struct {
	Results []ToolResult `json:"results,omitempty"`
	Errors  []string     `json:"errors,omitempty"`
}

// QueryWithRetry runs the normal query pass, then asks an LLM for one safe follow-up round when tools failed.
func QueryWithRetry(ctx context.Context, client RetryClient, planner RetryChatClient, query string, logf func(string, ...any)) (*QueryResult, error) {
	if client == nil {
		return nil, nil
	}
	result, err := client.Query(ctx, query)
	if err != nil || result == nil || planner == nil || len(result.Errors) == 0 {
		return result, err
	}
	retryMCPWithLLM(ctx, client, planner, query, result, logf)
	return result, nil
}

func retryMCPWithLLM(ctx context.Context, client RetryClient, planner RetryChatClient, query string, result *QueryResult, logf func(string, ...any)) {
	tools := client.AvailableTools()
	if len(tools) == 0 {
		return
	}

	originalErrors := append([]string(nil), result.Errors...)
	plan, err := planRetries(ctx, planner, query, result, tools)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("MCP retry planning failed: %v", err))
		return
	}

	var retryErrors []string
	succeeded := false
	for i, call := range plan.Calls {
		if i >= maxRetryCalls {
			break
		}
		if strings.TrimSpace(call.Tool) == "" {
			continue
		}
		if logf != nil {
			logf("MCP retrying tool: %s/%s", call.Server, call.Tool)
		}
		toolResult, err := client.CallTool(ctx, ToolCall{Server: call.Server, Tool: call.Tool, Arguments: call.Arguments})
		if err != nil {
			retryErrors = append(retryErrors, fmt.Sprintf("MCP retry %s/%s failed: %v", call.Server, call.Tool, err))
			continue
		}
		if strings.TrimSpace(toolResult.Text) != "" {
			result.Results = append(result.Results, toolResult)
			succeeded = true
		}
	}

	if succeeded {
		result.Errors = retryErrors
		return
	}
	result.Errors = append(originalErrors, retryErrors...)
}

func planRetries(ctx context.Context, planner RetryChatClient, query string, result *QueryResult, tools []ToolInfo) (*retryPlan, error) {
	toolsJSON, err := json.MarshalIndent(tools, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal MCP tools: %w", err)
	}
	stateJSON, err := json.MarshalIndent(retryPlanningState{Results: result.Results, Errors: result.Errors}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal MCP state: %w", err)
	}

	messages := []bedrock.ChatMessage{
		{Role: "system", Content: retrySystemPrompt},
		{Role: "user", Content: fmt.Sprintf("User request: %q\n\nAvailable MCP tools:\n%s\n\nPrevious MCP results/errors:\n%s", query, toolsJSON, stateJSON)},
	}
	text, err := planner.GenerateChatResponse(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("failed to generate MCP retry plan: %w", err)
	}

	var plan retryPlan
	if err := json.Unmarshal([]byte(cleanJSONResponse(text)), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse MCP retry plan: %w", err)
	}
	if len(plan.Calls) > maxRetryCalls {
		plan.Calls = plan.Calls[:maxRetryCalls]
	}
	return &plan, nil
}

var retrySystemPrompt = strings.TrimSpace(`
You plan one follow-up round of read-only MCP tool calls after some MCP calls failed.
Return only a JSON object with this schema:
{"calls":[{"server":"string","tool":"string","arguments":{},"reason":"string"}]}

Rules:
- Use only tools from Available MCP tools.
- Include every required argument shown by the tool input schema.
- Use IDs, team names, or other arguments only when they are present in the user request or previous MCP results.
- Do not repeat the same invalid argument shape that already failed.
- If no useful safe follow-up call can be made, return {"calls":[]}.
- Plan at most 3 calls.
`)

func cleanJSONResponse(text string) string {
	cleaned := strings.TrimSpace(text)
	if !strings.HasPrefix(cleaned, "```") {
		return cleaned
	}
	cleaned = strings.TrimPrefix(cleaned, "```")
	if idx := strings.IndexByte(cleaned, '\n'); idx >= 0 {
		cleaned = cleaned[idx+1:]
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(cleaned), "```"))
}
