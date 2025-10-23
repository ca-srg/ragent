package slacksearch

import (
	"context"
	"io"
	"log"
	"testing"
	"time"

	"github.com/ca-srg/ragent/internal/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/slackbot"
	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockSlackConversationClient struct {
	mock.Mock
}

func (m *mockSlackConversationClient) GetConversationRepliesContext(ctx context.Context, params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error) {
	args := m.Called(ctx, params)
	if res := args.Get(0); res != nil {
		replySlice := res.([]slack.Message)
		return replySlice, args.Bool(1), args.String(2), args.Error(3)
	}
	return nil, args.Bool(1), args.String(2), args.Error(3)
}

func (m *mockSlackConversationClient) GetConversationHistoryContext(ctx context.Context, params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
	args := m.Called(ctx, params)
	if res := args.Get(0); res != nil {
		return res.(*slack.GetConversationHistoryResponse), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *mockSlackConversationClient) GetPermalinkContext(ctx context.Context, params *slack.PermalinkParameters) (string, error) {
	args := m.Called(ctx, params)
	return args.String(0), args.Error(1)
}

func newTestContextRetriever(client slackConversationClient, bedrockClient bedrockChatClient, cfg *SlackSearchConfig) *ContextRetriever {
	slackClient := slack.New("dummy-token")
	rateLimiter := slackbot.NewRateLimiter(1000, 1000, 1000)
	cr, err := NewContextRetriever(slackClient, rateLimiter, &bedrock.BedrockClient{}, cfg, log.New(io.Discard, "", 0))
	if err != nil {
		panic(err)
	}
	cr.client = client
	cr.bedrockClient = bedrockClient
	cr.logger.SetOutput(io.Discard)
	return cr
}

func TestContextRetrieverRetrieveContextSuccess(t *testing.T) {
	bedrockMock := &mockBedrockClient{}
	slackMock := &mockSlackConversationClient{}

	cfg := &SlackSearchConfig{
		ContextWindowMinutes: 30,
		MaxContextMessages:   4,
		TimeoutSeconds:       5,
		Enabled:              true,
		UserToken:            "xoxp-test",
	}

	retriever := newTestContextRetriever(slackMock, bedrockMock, cfg)

	msgTS := "1697880000.000100"
	threadReplyTS := "1697883600.000200"
	prevTS := "1697876400.000100"
	nextTS := "1697887200.000100"

	messages := []slack.Message{
		{
			Msg: slack.Msg{
				Channel:         "C123",
				User:            "U123",
				Text:            "Deployment summary needed",
				Timestamp:       msgTS,
				ThreadTimestamp: msgTS,
			},
		},
		{
			Msg: slack.Msg{
				Channel:   "C123",
				User:      "U999",
				Text:      "Any blockers?",
				Timestamp: "1697888000.000100",
			},
		},
	}

	bedrockMock.On("GenerateChatResponse", mock.Anything, mock.Anything).
		Return(`{"message_indices":[0,1]}`, nil).Once()

	slackMock.On("GetPermalinkContext", mock.Anything, mock.MatchedBy(func(params *slack.PermalinkParameters) bool {
		return params.Channel == "C123" && params.Ts == msgTS
	})).Return("https://example/slack/permalink", nil).Once()

	slackMock.On("GetConversationRepliesContext", mock.Anything, mock.MatchedBy(func(params *slack.GetConversationRepliesParameters) bool {
		return params.ChannelID == "C123" && params.Timestamp == msgTS
	})).Return([]slack.Message{
		{Msg: slack.Msg{Timestamp: msgTS}}, // original message (filtered)
		{Msg: slack.Msg{Timestamp: threadReplyTS, Text: "Reply with results"}},
	}, false, "", nil).Once()

	slackMock.On("GetConversationHistoryContext", mock.Anything, mock.AnythingOfType("*slack.GetConversationHistoryParameters")).
		Return(&slack.GetConversationHistoryResponse{
			Messages: []slack.Message{
				{Msg: slack.Msg{Timestamp: nextTS, Text: "Post deployment status"}},
				{Msg: slack.Msg{Timestamp: prevTS, Text: "Pre deployment checklist"}},
				{Msg: slack.Msg{Timestamp: msgTS, Text: "Duplicate original"}}, // should be filtered
			},
		}, nil).
		Twice()

	slackMock.On("GetPermalinkContext", mock.Anything, mock.MatchedBy(func(params *slack.PermalinkParameters) bool {
		return params.Channel == "C123" && params.Ts == "1697888000.000100"
	})).Return("https://example/slack/permalink-2", nil).Once()

	resp, err := retriever.RetrieveContext(context.Background(), &ContextRequest{
		Messages:      messages,
		UserQuery:     "Summarize deployment",
		ContextWindow: 15 * time.Minute,
	})
	require.NoError(t, err)
	require.Len(t, resp.EnrichedMessages, 2)

	first := resp.EnrichedMessages[0]
	assert.Equal(t, "https://example/slack/permalink", first.Permalink)
	require.Len(t, first.ThreadMessages, 1)
	assert.Equal(t, threadReplyTS, first.ThreadMessages[0].Timestamp)
	require.Len(t, first.PreviousMessages, 1)
	assert.Equal(t, prevTS, first.PreviousMessages[0].Timestamp)
	require.Len(t, first.NextMessages, 1)
	assert.Equal(t, nextTS, first.NextMessages[0].Timestamp)

	second := resp.EnrichedMessages[1]
	assert.Equal(t, "https://example/slack/permalink-2", second.Permalink)
	assert.Empty(t, second.ThreadMessages)

	assert.Equal(t, 4, resp.TotalRetrieved) // thread reply + 3 timeline messages

	bedrockMock.AssertExpectations(t)
	slackMock.AssertExpectations(t)
}

func TestContextRetrieverFallbackOnLLMFailure(t *testing.T) {
	bedrockMock := &mockBedrockClient{}
	slackMock := &mockSlackConversationClient{}
	cfg := &SlackSearchConfig{
		ContextWindowMinutes: 30,
		MaxContextMessages:   2,
		TimeoutSeconds:       5,
		Enabled:              true,
		UserToken:            "xoxp-test",
	}
	retriever := newTestContextRetriever(slackMock, bedrockMock, cfg)

	message := slack.Message{
		Msg: slack.Msg{
			Channel:   "C123",
			User:      "U1",
			Text:      "Reminder",
			Timestamp: "1697880000.000100",
		},
	}

	bedrockMock.On("GenerateChatResponse", mock.Anything, mock.Anything).
		Return("not json", nil).Once()

	slackMock.On("GetPermalinkContext", mock.Anything, mock.AnythingOfType("*slack.PermalinkParameters")).
		Return("permalink", nil).Once()
	slackMock.On("GetConversationHistoryContext", mock.Anything, mock.AnythingOfType("*slack.GetConversationHistoryParameters")).
		Return(&slack.GetConversationHistoryResponse{Messages: []slack.Message{}}, nil).Once()

	resp, err := retriever.RetrieveContext(context.Background(), &ContextRequest{
		Messages:  []slack.Message{message},
		UserQuery: "Any reminders?",
	})
	require.NoError(t, err)
	require.Len(t, resp.EnrichedMessages, 1)

	bedrockMock.AssertExpectations(t)
	slackMock.AssertExpectations(t)
}

func TestContextRetrieverHandlesSlackErrorsGracefully(t *testing.T) {
	bedrockMock := &mockBedrockClient{}
	slackMock := &mockSlackConversationClient{}
	cfg := &SlackSearchConfig{
		ContextWindowMinutes: 30,
		MaxContextMessages:   2,
		TimeoutSeconds:       5,
		Enabled:              true,
		UserToken:            "xoxp-test",
	}
	retriever := newTestContextRetriever(slackMock, bedrockMock, cfg)

	message := slack.Message{
		Msg: slack.Msg{
			Channel:   "C123",
			User:      "U1",
			Text:      "Status check",
			Timestamp: "1697880000.000100",
		},
	}

	bedrockMock.On("GenerateChatResponse", mock.Anything, mock.Anything).
		Return(`{"message_indices":[0]}`, nil).Once()

	slackMock.On("GetPermalinkContext", mock.Anything, mock.AnythingOfType("*slack.PermalinkParameters")).
		Return("", assert.AnError).Once()
	slackMock.On("GetConversationHistoryContext", mock.Anything, mock.AnythingOfType("*slack.GetConversationHistoryParameters")).
		Return(nil, assert.AnError).Once()

	resp, err := retriever.RetrieveContext(context.Background(), &ContextRequest{
		Messages:  []slack.Message{message},
		UserQuery: "Check status",
	})
	require.NoError(t, err)
	require.Len(t, resp.EnrichedMessages, 1)
	assert.Empty(t, resp.EnrichedMessages[0].ThreadMessages)
	assert.Empty(t, resp.EnrichedMessages[0].PreviousMessages)
	assert.Empty(t, resp.EnrichedMessages[0].NextMessages)

	bedrockMock.AssertExpectations(t)
	slackMock.AssertExpectations(t)
}
