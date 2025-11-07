package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ca-srg/ragent/internal/slackbot"
	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubSlackSearch struct {
	result    *slackbot.SearchResult
	lastQuery string
}

func (s *stubSlackSearch) Search(_ context.Context, query string, _ slackbot.SearchOptions) *slackbot.SearchResult {
	s.lastQuery = query
	return s.result
}

func extractContextTexts(t *testing.T, options []slack.MsgOption) []string {
	t.Helper()
	_, values, err := slack.UnsafeApplyMsgOptions("test-token", "C111", "https://slack.com/api/", options...)
	require.NoError(t, err)
	blocksJSON := values.Get("blocks")
	require.NotEmpty(t, blocksJSON)
	var blocks slack.Blocks
	require.NoError(t, json.Unmarshal([]byte(blocksJSON), &blocks))
	var texts []string
	for _, block := range blocks.BlockSet {
		ctx, ok := block.(*slack.ContextBlock)
		if !ok {
			continue
		}
		for _, element := range ctx.ContextElements.Elements {
			if textElem, ok := element.(*slack.TextBlockObject); ok {
				texts = append(texts, textElem.Text)
			}
		}
	}
	return texts
}

func TestSlackProcessorIncludesSearchMethod(t *testing.T) {
	detector := &slackbot.MentionDetector{}
	extractor := &slackbot.QueryExtractor{}
	formatter := &slackbot.Formatter{}

	tcases := []struct {
		name         string
		message      string
		searchResult *slackbot.SearchResult
		expectedMode string
	}{
		{
			name:    "url exact match response",
			message: "https://example.com/doc の概要を教えて",
			searchResult: &slackbot.SearchResult{
				GeneratedResponse: "URL exact match response",
				Total:             1,
				Elapsed:           45 * time.Millisecond,
				SearchMethod:      "url_exact_match",
				URLDetected:       true,
			},
			expectedMode: "url_exact_match",
		},
		{
			name:    "hybrid fallback response",
			message: "機械学習について",
			searchResult: &slackbot.SearchResult{
				Items: []slackbot.SearchItem{{
					Title:   "ML Guide",
					Snippet: "概要テキスト",
				}},
				Total:        1,
				Elapsed:      72 * time.Millisecond,
				SearchMethod: "hybrid_search",
			},
			expectedMode: "hybrid_search",
		},
	}

	const botID = "UBOT"

	for _, tc := range tcases {
		t.Run(tc.name, func(t *testing.T) {
			adapter := &stubSlackSearch{result: tc.searchResult}
			processor := slackbot.NewProcessor(detector, extractor, adapter, formatter, nil)

			msg := &slack.MessageEvent{Msg: slack.Msg{Text: fmt.Sprintf("<@%s> %s", botID, tc.message), Channel: "C111", User: "U222"}}
			reply := processor.ProcessMessage(context.Background(), botID, msg)
			require.NotNil(t, reply, "expected reply for mention")
			require.Len(t, reply.MsgOptions, 1)

			texts := extractContextTexts(t, reply.MsgOptions)
			require.NotEmpty(t, texts, "expected context metadata")

			found := false
			for _, text := range texts {
				if strings.Contains(text, tc.expectedMode) {
					found = true
					break
				}
			}
			assert.True(t, found, "context should include search method %s in %v", tc.expectedMode, texts)

			expectedQuery := strings.TrimSpace(tc.message)
			assert.Equal(t, expectedQuery, adapter.lastQuery)
		})
	}
}
