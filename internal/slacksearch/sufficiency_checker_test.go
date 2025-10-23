package slacksearch

import (
	"context"
	"io"
	"log"
	"testing"

	"github.com/ca-srg/ragent/internal/embedding/bedrock"
	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockSufficiencyBedrockClient struct {
	mock.Mock
}

func (m *mockSufficiencyBedrockClient) GenerateChatResponse(ctx context.Context, messages []bedrock.ChatMessage) (string, error) {
	args := m.Called(ctx, messages)
	return args.String(0), args.Error(1)
}

func newTestSufficiencyChecker(client bedrockChatClient) *SufficiencyChecker {
	checker := NewSufficiencyChecker(nil, log.New(io.Discard, "", 0))
	checker.bedrockClient = client
	return checker
}

func TestSufficiencyCheckerReturnsParsedResponse(t *testing.T) {
	mockClient := &mockSufficiencyBedrockClient{}
	checker := newTestSufficiencyChecker(mockClient)

	mockClient.On("GenerateChatResponse", mock.Anything, mock.Anything).
		Return(`{"is_sufficient":true,"missing_info":[],"reasoning":"All requirements met","confidence":0.88}`, nil).
		Once()

	req := &SufficiencyRequest{
		UserQuery: "What was decided in the release meeting?",
		Messages: []EnrichedMessage{
			{OriginalMessage: slackMessage("C1", "U1", "1697880000.000100", "We approved release 1.2")},
		},
		Iteration:     1,
		MaxIterations: 5,
	}

	resp, err := checker.Check(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, resp.IsSufficient)
	assert.Empty(t, resp.MissingInfo)
	assert.Equal(t, "All requirements met", resp.Reasoning)
	assert.InDelta(t, 0.88, resp.Confidence, 0.0001)

	mockClient.AssertExpectations(t)
}

func TestSufficiencyCheckerHandlesParseFailure(t *testing.T) {
	mockClient := &mockSufficiencyBedrockClient{}
	checker := newTestSufficiencyChecker(mockClient)

	mockClient.On("GenerateChatResponse", mock.Anything, mock.Anything).
		Return(`{invalid json`, nil).
		Once()

	req := &SufficiencyRequest{
		UserQuery: "Summarize the incident follow-up",
		Messages: []EnrichedMessage{
			{OriginalMessage: slackMessage("C1", "U1", "1697880000.000200", "Need RCA details")},
		},
	}

	resp, err := checker.Check(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, resp.IsSufficient)
	assert.NotEmpty(t, resp.MissingInfo)
	assert.Contains(t, resp.Reasoning, "invalid")

	mockClient.AssertExpectations(t)
}

func TestSufficiencyCheckerHonorsMaxIterations(t *testing.T) {
	mockClient := &mockSufficiencyBedrockClient{}
	checker := newTestSufficiencyChecker(mockClient)

	req := &SufficiencyRequest{
		UserQuery:     "Any remaining risks?",
		Messages:      nil,
		Iteration:     5,
		MaxIterations: 5,
	}

	resp, err := checker.Check(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, resp.IsSufficient)
	require.NotEmpty(t, resp.MissingInfo)
	assert.Contains(t, resp.MissingInfo[0], "Maximum iteration limit reached")
	assert.Equal(t, 0.3, resp.Confidence)

	mockClient.AssertNotCalled(t, "GenerateChatResponse", mock.Anything, mock.Anything)
}

func slackMessage(channel, user, ts, text string) slack.Message {
	return slack.Message{
		Msg: slack.Msg{
			Channel:   channel,
			User:      user,
			Timestamp: ts,
			Text:      text,
		},
	}
}
