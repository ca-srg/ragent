package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ca-srg/ragent/internal/pkg/evalexport"
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
		ThreadMessages: []slack.Message{{Msg: slack.Msg{
			Timestamp: "1234567890.654321",
			User:      "U456",
			Text:      "thread reply",
		}}},
	}
	fetcher := &fakeSlackURLFetcher{response: &slacksearch.FetchResponse{
		EnrichedMessages: []slacksearch.EnrichedMessage{message},
		FetchedCount:     1,
	}}
	adapter := newSlackURLTestAdapter(fetcher)
	evalDir := t.TempDir()
	evalWriter, err := evalexport.NewWriter(evalDir)
	require.NoError(t, err)
	adapter.SetEvalWriter(evalWriter)

	query := "この URL の内容を教えて\nhttps://example-workspace.slack.com/archives/C01234567/p1234567890123456"
	result, err := adapter.HandleToolCall(context.Background(), map[string]interface{}{
		"query": query,
		"top_k": 10,
	})

	require.NoError(t, err)
	require.Len(t, result.Content, 1)
	require.NotNil(t, fetcher.request)
	require.Len(t, fetcher.request.URLs, 1)
	assert.Equal(t, "C01234567", fetcher.request.URLs[0].ChannelID)
	assert.Equal(t, "1234567890.123456", fetcher.request.URLs[0].MessageTS)

	var response HybridSearchResponse
	require.NoError(t, json.Unmarshal([]byte(result.Content[0].Text), &response))
	require.Len(t, response.ReferencedSlackURLs, 1)
	assert.Equal(t, "referenced message", response.ReferencedSlackURLs[0].Message)
	assert.Equal(t, message.Permalink, response.ReferencedSlackURLs[0].Permalink)
	require.Len(t, response.ReferencedSlackURLs[0].ThreadReplies, 1)
	assert.Equal(t, "thread reply", response.ReferencedSlackURLs[0].ThreadReplies[0].Text)
	assert.Empty(t, response.Results)
	assert.Equal(t, slackURLDirectFetchMethod, response.SearchMethod)
	assert.Equal(t, 1, response.Total)
	assert.Equal(t, []string{"slack_urls"}, response.SearchSources)
	assert.Less(t, strings.Index(result.Content[0].Text, `"referenced_slack_urls"`), strings.Index(result.Content[0].Text, `"results"`))

	searchClient := adapter.searchClient.(*fakeSDKSearchClient)
	assert.Nil(t, searchClient.bm25Query)
	assert.Nil(t, searchClient.vectorQuery)

	evalFiles, err := filepath.Glob(filepath.Join(evalDir, "eval_*.jsonl"))
	require.NoError(t, err)
	require.Len(t, evalFiles, 1)
	evalData, err := os.ReadFile(evalFiles[0])
	require.NoError(t, err)
	var record evalexport.EvalRecord
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(string(evalData))), &record))
	assert.Equal(t, slackURLDirectFetchMethod, record.RunConfig.SearchMode)
	assert.Contains(t, record.RetrievedContexts, slacksearch.FormatFetchedContext([]slacksearch.EnrichedMessage{message}))
	assert.Equal(t, message.Permalink, record.References["C01234567#1234567890.123456"])
	assert.Empty(t, record.RetrievedDocs)
}

func TestHybridSearchToolDoesNotDirectReturnWhenWorkspaceSlackSearchRequested(t *testing.T) {
	const slackURL = "https://example-workspace.slack.com/archives/C01234567/p1234567890123456"
	tests := []struct {
		name   string
		query  string
		enable bool
	}{
		{name: "request flag", query: slackURL, enable: true},
		{name: "query directive", query: "Slack検索を利用して関連会話も探して " + slackURL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fetcher := &fakeSlackURLFetcher{response: &slacksearch.FetchResponse{
				EnrichedMessages: []slacksearch.EnrichedMessage{{Permalink: slackURL}},
				FetchedCount:     1,
			}}
			adapter := newSlackURLTestAdapter(fetcher)
			adapter.slackService = &slacksearch.SlackSearchService{}
			adapter.searchClient.(*fakeSDKSearchClient).healthCheckErr = assert.AnError

			_, err := adapter.HandleToolCall(context.Background(), map[string]interface{}{
				"query":               tt.query,
				"top_k":               1,
				"enable_slack_search": tt.enable,
			})

			require.Error(t, err)
			assert.Contains(t, err.Error(), "OpenSearch connection failed")
		})
	}
}

func TestHybridSearchToolFallsBackWhenSlackURLFetchIsPartial(t *testing.T) {
	const firstURL = "https://example-workspace.slack.com/archives/C01234567/p1234567890123456"
	const secondURL = "https://example-workspace.slack.com/archives/C07654321/p1234567890654321"
	fetcher := &fakeSlackURLFetcher{response: &slacksearch.FetchResponse{
		EnrichedMessages: []slacksearch.EnrichedMessage{{
			OriginalMessage: slack.Message{Msg: slack.Msg{Channel: "C01234567", Timestamp: "1234567890.123456"}},
			Permalink:       firstURL,
		}},
		FetchedCount: 1,
		Errors:       []string{"failed to fetch " + secondURL},
	}}
	adapter := newSlackURLTestAdapter(fetcher)

	result, err := adapter.HandleToolCall(context.Background(), map[string]interface{}{
		"query": firstURL + " " + secondURL,
		"top_k": 1,
	})

	require.NoError(t, err)
	var response HybridSearchResponse
	require.NoError(t, json.Unmarshal([]byte(result.Content[0].Text), &response))
	assert.NotEqual(t, slackURLDirectFetchMethod, response.SearchMethod)
	require.Len(t, response.ReferencedSlackURLs, 1)
	searchClient := adapter.searchClient.(*fakeSDKSearchClient)
	assert.NotNil(t, searchClient.bm25Query)
	assert.NotNil(t, searchClient.vectorQuery)
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
