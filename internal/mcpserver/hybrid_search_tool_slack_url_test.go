package mcpserver

import (
	"context"
	"io"
	"log"
	"testing"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ca-srg/ragent/internal/pkg/opensearch"
	"github.com/ca-srg/ragent/internal/pkg/slacksearch"
)

type fakeSlackURLFetcher struct {
	request  *slacksearch.FetchRequest
	response *slacksearch.FetchResponse
}

func (f *fakeSlackURLFetcher) FetchByURLs(_ context.Context, req *slacksearch.FetchRequest) (*slacksearch.FetchResponse, error) {
	f.request = req
	return f.response, nil
}

func TestHybridSearchToolFetchesReferencedSlackURLWithoutSlackSearchService(t *testing.T) {
	message := slacksearch.EnrichedMessage{
		OriginalMessage: slack.Message{Msg: slack.Msg{
			Channel:   "C01234567",
			Timestamp: "1234567890.123456",
			User:      "U123",
			Text:      "referenced message",
		}},
		Permalink: "https://example-workspace.slack.com/archives/C01234567/p1234567890123456",
	}
	fetcher := &fakeSlackURLFetcher{response: &slacksearch.FetchResponse{
		EnrichedMessages: []slacksearch.EnrichedMessage{message},
		FetchedCount:     1,
	}}
	adapter := &HybridSearchToolAdapter{logger: log.New(io.Discard, "", 0)}
	adapter.setSlackURLFetcher(fetcher)

	query := "この URL の内容を教えて\nhttps://example-workspace.slack.com/archives/C01234567/p1234567890123456"
	messages := adapter.fetchReferencedSlackMessages(context.Background(), query)

	require.Len(t, messages, 1)
	require.NotNil(t, fetcher.request)
	require.Len(t, fetcher.request.URLs, 1)
	assert.Equal(t, "C01234567", fetcher.request.URLs[0].ChannelID)
	assert.Equal(t, "1234567890.123456", fetcher.request.URLs[0].MessageTS)

	response := adapter.convertToMCPResponse(
		&HybridSearchRequest{Query: query, SearchMode: "hybrid"},
		&opensearch.HybridSearchResult{FusionResult: &opensearch.FusionResult{}},
		nil,
		messages,
	)
	require.Len(t, response.ReferencedSlackURLs, 1)
	assert.Equal(t, "referenced message", response.ReferencedSlackURLs[0].Message)
	assert.Equal(t, message.Permalink, response.ReferencedSlackURLs[0].Permalink)
	assert.Equal(t, 1, response.Total)
	assert.Equal(t, []string{"slack_urls"}, response.SearchSources)
}

func TestHybridSearchToolAllowsURLFetchWhenSlackSearchRequestedWithoutService(t *testing.T) {
	const slackURL = "https://example-workspace.slack.com/archives/C01234567/p1234567890123456"
	tests := []struct {
		name  string
		query string
		flag  bool
	}{
		{name: "request flag", query: slackURL, flag: true},
		{name: "query directive", query: "Slack検索を利用してこのURLの内容を教えて " + slackURL},
		{name: "disable directive", query: "Slack検索を利用せずこのURLの内容を教えて " + slackURL, flag: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fetcher := &fakeSlackURLFetcher{response: &slacksearch.FetchResponse{}}
			adapter := newSlackURLTestAdapter(fetcher)

			result, err := adapter.HandleToolCall(context.Background(), map[string]interface{}{
				"query":               tt.query,
				"top_k":               1,
				"enable_slack_search": tt.flag,
			})

			require.NoError(t, err)
			require.NotNil(t, result)
			assert.False(t, result.IsError)
			require.NotNil(t, fetcher.request)
		})
	}
}

func TestHybridSearchToolRejectsUnavailableSlackSearchWithoutFetchableURL(t *testing.T) {
	const slackURL = "https://example-workspace.slack.com/archives/C01234567/p1234567890123456"
	tests := []struct {
		name        string
		query       string
		withFetcher bool
	}{
		{name: "flag without URL", query: "Slackの内容を検索", withFetcher: true},
		{name: "directive without URL", query: "Slack検索を利用して最近の議論を教えて", withFetcher: true},
		{name: "URL without fetcher", query: slackURL},
		{name: "unparseable URL", query: slackURL + "%zz", withFetcher: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fetcher slackURLFetcher
			if tt.withFetcher {
				fetcher = &fakeSlackURLFetcher{response: &slacksearch.FetchResponse{}}
			}
			adapter := newSlackURLTestAdapter(fetcher)

			result, err := adapter.HandleToolCall(context.Background(), map[string]interface{}{
				"query":               tt.query,
				"top_k":               1,
				"enable_slack_search": true,
			})

			require.Error(t, err)
			require.NotNil(t, result)
			assert.True(t, result.IsError)
			assert.Contains(t, err.Error(), "slack search requested but not configured")
		})
	}
}

func newSlackURLTestAdapter(fetcher slackURLFetcher) *HybridSearchToolAdapter {
	adapter := NewHybridSearchToolAdapter(
		&fakeSDKSearchClient{},
		&fakeSDKEmbeddingClient{},
		&HybridSearchConfig{DefaultIndexName: "ragent-docs"},
		nil,
	)
	adapter.logger = log.New(io.Discard, "", 0)
	adapter.slackURLFetcher = fetcher
	return adapter
}

func TestHybridSearchToolPreservesSlackServiceURLFetcher(t *testing.T) {
	existing := &fakeSlackURLFetcher{}
	replacement := &fakeSlackURLFetcher{}
	adapter := &HybridSearchToolAdapter{slackURLFetcher: existing}

	adapter.setSlackURLFetcher(replacement)

	assert.Equal(t, existing, adapter.slackURLFetcher)
}

func TestHybridSearchToolIgnoresNilSlackURLFetcher(t *testing.T) {
	adapter := &HybridSearchToolAdapter{}
	var fetcher *slacksearch.MessageFetcher

	adapter.setSlackURLFetcher(fetcher)

	assert.Nil(t, adapter.slackURLFetcher)
}
