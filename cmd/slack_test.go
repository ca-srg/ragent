package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/ca-srg/ragent/internal/slackbot"
	"github.com/ca-srg/ragent/internal/slacksearch"
	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBotSlackSearcherSearchesConversations(t *testing.T) {
	stub := &stubSlackService{
		result: &slacksearch.SlackSearchResult{
			IterationCount: 1,
			TotalMatches:   1,
			EnrichedMessages: []slacksearch.EnrichedMessage{
				{OriginalMessage: slack.Message{Msg: slack.Msg{Channel: "C123", Timestamp: "1700000000.000", Text: "Release plan"}}},
			},
		},
	}

	searcher := newBotSlackSearcher(stub, nil)
	res, err := searcher.SearchConversations(context.Background(), "release plan", slackbot.SearchOptions{})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "release plan", stub.lastQuery)
	assert.Nil(t, stub.lastChannels)
	assert.Equal(t, 1, res.TotalMatches)
	assert.Equal(t, "C123", res.Messages[0].Channel)
}

func TestSlackFormatterIncludesSlackBlocks(t *testing.T) {
	result := &slackbot.SearchResult{
		GeneratedResponse: "Answer",
		Total:             1,
		Slack: &slackbot.SlackConversationResult{
			IterationCount: 1,
			TotalMatches:   1,
			Messages: []slackbot.SlackConversationMessage{
				{
					Channel:   "general",
					Timestamp: "1700000000.000",
					User:      "U123",
					Text:      "Deployment discussion",
					Permalink: "https://slack.example.com/archives/C123/p1700000000000",
				},
			},
		},
	}

	blocks := slackbot.BuildSlackResultBlocksForTest(result.Slack)
	foundHeader := false
	foundPermalink := false

	for _, block := range blocks {
		switch b := block.(type) {
		case *slack.SectionBlock:
			if b.Text != nil && strings.Contains(b.Text.Text, "Conversations from Slack") {
				foundHeader = true
			}
			if b.Accessory != nil && b.Accessory.ButtonElement != nil && b.Accessory.ButtonElement.URL == "https://slack.example.com/archives/C123/p1700000000000" {
				foundPermalink = true
			}
		}
	}

	assert.True(t, foundHeader)
	assert.True(t, foundPermalink)
}

type stubSlackService struct {
	result       *slacksearch.SlackSearchResult
	lastQuery    string
	lastChannels []string
}

func (s *stubSlackService) Search(ctx context.Context, query string, channels []string) (*slacksearch.SlackSearchResult, error) {
	s.lastQuery = query
	s.lastChannels = channels
	return s.result, nil
}
