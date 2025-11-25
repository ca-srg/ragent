package slacksearch

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ca-srg/ragent/internal/slackbot"
	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockSlackClient struct {
	mock.Mock
}

func (m *mockSlackClient) SearchMessagesContext(ctx context.Context, query string, params slack.SearchParameters) (*slack.SearchMessages, error) {
	args := m.Called(ctx, query, params)
	if result := args.Get(0); result != nil {
		return result.(*slack.SearchMessages), args.Error(1)
	}
	return nil, args.Error(1)
}

func newTestSearcher(client slackSearchClient) *Searcher {
	s := NewSearcher(nil, slackbot.NewRateLimiter(1000, 1000, 1000), 5*time.Second)
	s.client = client
	s.logger.SetOutput(io.Discard)
	return s
}

func TestSearcherSearchSuccessWithFilters(t *testing.T) {
	mockClient := &mockSlackClient{}
	searcher := newTestSearcher(mockClient)

	start := time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 10, 21, 0, 0, 0, 0, time.UTC)

	mockClient.On("SearchMessagesContext", mock.Anything, mock.MatchedBy(func(q string) bool {
		return strings.Contains(q, "in:#general") && strings.Contains(q, "after:2025-10-01") && strings.Contains(q, "before:2025-10-21")
	}), mock.MatchedBy(func(params slack.SearchParameters) bool {
		return params.Count == 50
	})).Return(&slack.SearchMessages{
		Matches: []slack.SearchMessage{
			{
				Text:      "Deployment completed",
				Timestamp: "1697880000.000100",
				User:      "U123",
				Username:  "alice",
				Channel:   slack.CtxChannel{ID: "CDEV"},
				Permalink: "https://workspace.slack.com/archives/CDEV/p1697880000000100",
			},
		},
		Paging: slack.Paging{Page: 1, Pages: 1},
		Total:  1,
	}, nil).Once()

	req := &SearchRequest{
		Query: "deployment update",
		TimeRange: &TimeRange{
			Start: &start,
			End:   &end,
		},
		Channels:   []string{"general"},
		MaxResults: 50,
	}

	resp, err := searcher.Search(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, resp.Messages, 1)
	assert.Equal(t, "Deployment completed", resp.Messages[0].Text)
	assert.True(t, strings.Contains(resp.Query, "in:#general"))

	mockClient.AssertExpectations(t)
}

func TestSearcherRetryOnRateLimit(t *testing.T) {
	mockClient := &mockSlackClient{}
	searcher := newTestSearcher(mockClient)

	req := &SearchRequest{
		Query:      "incident",
		MaxResults: 10,
	}

	mockClient.On("SearchMessagesContext", mock.Anything, mock.AnythingOfType("string"), mock.Anything).
		Return((*slack.SearchMessages)(nil), &slack.RateLimitedError{RetryAfter: time.Millisecond}).
		Once()

	mockClient.On("SearchMessagesContext", mock.Anything, mock.AnythingOfType("string"), mock.Anything).
		Return(&slack.SearchMessages{
			Matches: []slack.SearchMessage{
				{Text: "incident report", Timestamp: "1.0", Channel: slack.CtxChannel{ID: "C1"}},
			},
			Paging: slack.Paging{Page: 1, Pages: 1},
			Total:  1,
		}, nil).
		Once()

	resp, err := searcher.SearchWithRetry(context.Background(), req, 1)
	require.NoError(t, err)
	require.Len(t, resp.Messages, 1)

	mockClient.AssertNumberOfCalls(t, "SearchMessagesContext", 2)
}

func TestSearcherCircuitBreakerTripsAfterFailures(t *testing.T) {
	mockClient := &mockSlackClient{}
	searcher := newTestSearcher(mockClient)
	req := &SearchRequest{Query: "status"}

	mockClient.On("SearchMessagesContext", mock.Anything, mock.AnythingOfType("string"), mock.Anything).
		Return((*slack.SearchMessages)(nil), slack.StatusCodeError{Code: 500, Status: "500 Internal Server Error"}).
		Times(3)

	for i := 0; i < 3; i++ {
		_, err := searcher.SearchWithRetry(context.Background(), req, 0)
		require.Error(t, err)
	}

	_, err := searcher.SearchWithRetry(context.Background(), req, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circuit")

	mockClient.AssertNumberOfCalls(t, "SearchMessagesContext", 3)
}
