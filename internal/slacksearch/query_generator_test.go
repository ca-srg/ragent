package slacksearch

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/ca-srg/ragent/internal/embedding/bedrock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockBedrockClient struct {
	mock.Mock
}

func (m *mockBedrockClient) GenerateChatResponse(ctx context.Context, messages []bedrock.ChatMessage) (string, error) {
	args := m.Called(ctx, messages)
	return args.String(0), args.Error(1)
}

func newTestQueryGenerator(client bedrockChatClient, now time.Time) *QueryGenerator {
	qg := NewQueryGenerator(nil, 60*time.Second)
	qg.bedrockClient = client
	qg.nowFunc = func() time.Time { return now }
	qg.logger.SetOutput(io.Discard)
	return qg
}

func TestQueryGeneratorGenerateQueriesExtractsMetadata(t *testing.T) {
	mockClient := &mockBedrockClient{}
	now := time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC)

	mockClient.On("GenerateChatResponse", mock.Anything, mock.Anything).
		Return(`{"queries":["deployment summary timeline","deployment incidents"],"time_filter":null,"reasoning":"focus on deployment updates"}`, nil).
		Once()

	gen := newTestQueryGenerator(mockClient, now)

	req := &QueryGenerationRequest{
		UserQuery: "Give me the deployment summary from last week in #deployments",
	}

	resp, err := gen.GenerateQueries(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, resp.Queries, 2)
	assert.ElementsMatch(t, []string{"deployment summary timeline", "deployment incidents"}, resp.Queries)

	require.NotNil(t, resp.TimeFilter)
	expectedStart := startOfDay(now.AddDate(0, 0, -7))
	expectedEnd := now
	assert.WithinDuration(t, expectedStart, *resp.TimeFilter.Start, time.Second)
	assert.WithinDuration(t, expectedEnd, *resp.TimeFilter.End, time.Second)

	assert.Equal(t, "focus on deployment updates", resp.Reasoning)

	mockClient.AssertExpectations(t)
}

func TestQueryGeneratorGenerateAlternativeQueriesSkipsPrevious(t *testing.T) {
	mockClient := &mockBedrockClient{}
	now := time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC)

	mockClient.On("GenerateChatResponse", mock.Anything, mock.Anything).
		Return(`{"queries":["deployments summary","rollout checklist","deployments summary"],"time_filter":null,"reasoning":""}`, nil).
		Once()

	gen := newTestQueryGenerator(mockClient, now)

	req := &QueryGenerationRequest{
		UserQuery:       "Recent deployment notes",
		PreviousQueries: []string{"deployments summary"},
		PreviousResults: 0,
	}

	resp, err := gen.GenerateAlternativeQueries(context.Background(), req)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"rollout checklist"}, resp.Queries)

	mockClient.AssertExpectations(t)
}

func TestQueryGeneratorHandlesLLMTimeoutError(t *testing.T) {
	mockClient := &mockBedrockClient{}
	now := time.Date(2025, 10, 21, 12, 0, 0, 0, time.UTC)

	mockClient.On("GenerateChatResponse", mock.Anything, mock.Anything).
		Return("", context.DeadlineExceeded).
		Once()

	gen := newTestQueryGenerator(mockClient, now)

	_, err := gen.GenerateAlternativeQueries(context.Background(), &QueryGenerationRequest{
		UserQuery: "How did the outage get resolved?",
	})
	require.Error(t, err)

	mockClient.AssertExpectations(t)
}
