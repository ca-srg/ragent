package slacksearch

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockAssistantSearchClient struct {
	mock.Mock
}

func (m *mockAssistantSearchClient) SearchAssistantContextContext(
	ctx context.Context,
	params slack.AssistantSearchContextParameters,
) (*slack.AssistantSearchContextResponse, error) {
	args := m.Called(ctx, params)
	if r := args.Get(0); r != nil {
		return r.(*slack.AssistantSearchContextResponse), args.Error(1)
	}
	return nil, args.Error(1)
}

func newTestAssistantSearcher(client assistantSearchClient) *AssistantSearcher {
	s := NewAssistantSearcher(nil, NewRateLimiter(1000, 1000, 1000), 5*time.Second)
	s.client = client
	s.logger.SetOutput(io.Discard)
	return s
}

func TestAssistantSearcher_SearchSuccess(t *testing.T) {
	client := &mockAssistantSearchClient{}
	s := newTestAssistantSearcher(client)

	client.On("SearchAssistantContextContext", mock.Anything, mock.MatchedBy(func(p slack.AssistantSearchContextParameters) bool {
		// Verify ActionToken, ContextChannelID, channel/content types, time range
		return p.Query == "deployment" &&
			p.ActionToken == "TK123" &&
			p.ContextChannelID == "C_ORIGIN" &&
			len(p.ChannelTypes) == 1 && p.ChannelTypes[0] == "public_channel" &&
			len(p.ContentTypes) == 1 && p.ContentTypes[0] == "messages" &&
			p.IncludeContextMessages
	})).Return(&slack.AssistantSearchContextResponse{
		SlackResponse: slack.SlackResponse{Ok: true},
		Results: slack.AssistantSearchContextResults{
			Messages: []slack.AssistantSearchContextMessage{
				{
					ChannelID:    "C_HIT",
					ChannelName:  "engineering",
					AuthorUserID: "U1",
					AuthorName:   "alice",
					MessageTS:    "1700000000.000100",
					Content:      "deployment completed",
					Permalink:    "https://example.slack.com/archives/C_HIT/p1700000000000100",
				},
			},
		},
	}, nil).Once()

	resp, err := s.Search(context.Background(), &SearchRequest{
		Query:            "deployment",
		ActionToken:      "TK123",
		ContextChannelID: "C_ORIGIN",
		MaxResults:       5,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Messages, 1)
	got := resp.Messages[0]
	assert.Equal(t, "C_HIT", got.Channel)
	assert.Equal(t, "U1", got.User)
	assert.Equal(t, "alice", got.Username)
	assert.Equal(t, "deployment completed", got.Text)
	assert.Equal(t, "1700000000.000100", got.Timestamp)
	assert.Equal(t, "https://example.slack.com/archives/C_HIT/p1700000000000100", got.Permalink)
	client.AssertExpectations(t)
}

func TestAssistantSearcher_RejectsMissingActionToken(t *testing.T) {
	client := &mockAssistantSearchClient{}
	s := newTestAssistantSearcher(client)

	_, err := s.Search(context.Background(), &SearchRequest{Query: "x", MaxResults: 5})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "action_token is required")
	client.AssertNotCalled(t, "SearchAssistantContextContext", mock.Anything, mock.Anything)
}

func TestAssistantSearcher_PassesTimeRangeAsUnix(t *testing.T) {
	client := &mockAssistantSearchClient{}
	s := newTestAssistantSearcher(client)

	start := time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 10, 21, 0, 0, 0, 0, time.UTC)

	client.On("SearchAssistantContextContext", mock.Anything, mock.MatchedBy(func(p slack.AssistantSearchContextParameters) bool {
		return p.After == start.Unix() && p.Before == end.Unix()
	})).Return(&slack.AssistantSearchContextResponse{
		SlackResponse: slack.SlackResponse{Ok: true},
	}, nil).Once()

	_, err := s.Search(context.Background(), &SearchRequest{
		Query:       "q",
		ActionToken: "TK",
		MaxResults:  5,
		TimeRange:   &TimeRange{Start: &start, End: &end},
	})
	require.NoError(t, err)
	client.AssertExpectations(t)
}

func TestAssistantSearcher_FatalErrorFastFails(t *testing.T) {
	client := &mockAssistantSearchClient{}
	s := newTestAssistantSearcher(client)

	// Even with retries, an invalid_action_token must not be retried.
	client.On("SearchAssistantContextContext", mock.Anything, mock.Anything).
		Return(&slack.AssistantSearchContextResponse{
			SlackResponse: slack.SlackResponse{Ok: false, Error: "invalid_action_token"},
		}, nil).Once()

	_, err := s.SearchWithRetry(context.Background(), &SearchRequest{
		Query:       "x",
		ActionToken: "expired",
	}, 5)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_action_token")
	client.AssertNumberOfCalls(t, "SearchAssistantContextContext", 1)
}

func TestAssistantSearcher_NotOkBecomesError(t *testing.T) {
	client := &mockAssistantSearchClient{}
	s := newTestAssistantSearcher(client)

	client.On("SearchAssistantContextContext", mock.Anything, mock.Anything).
		Return(&slack.AssistantSearchContextResponse{
			SlackResponse: slack.SlackResponse{Ok: false, Error: "missing_scope"},
		}, nil).Once()

	_, err := s.Search(context.Background(), &SearchRequest{Query: "q", ActionToken: "TK"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing_scope")
}
