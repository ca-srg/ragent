package cmd

import (
	"context"
	"strings"
	"testing"

	"github.com/ca-srg/ragent/internal/pkg/slacksearch"
	"github.com/ca-srg/ragent/internal/slackbot"
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

	searcher := slackbot.NewBotSlackSearcher(stub, nil)
	require.NotNil(t, searcher)
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

func TestConvertSlackSearchResult(t *testing.T) {
	tests := []struct {
		name   string
		src    *slacksearch.SlackSearchResult
		assert func(t *testing.T, got *slackbot.SlackConversationResult)
	}{
		{
			name: "nil input returns nil",
			src:  nil,
			assert: func(t *testing.T, got *slackbot.SlackConversationResult) {
				assert.Nil(t, got)
			},
		},
		{
			name: "empty result initializes slices",
			src: &slacksearch.SlackSearchResult{
				IterationCount: 2,
				TotalMatches:   0,
				IsSufficient:   false,
			},
			assert: func(t *testing.T, got *slackbot.SlackConversationResult) {
				require.NotNil(t, got)
				assert.Equal(t, 2, got.IterationCount)
				assert.Zero(t, got.TotalMatches)
				assert.False(t, got.IsSufficient)
				assert.NotNil(t, got.MissingInfo)
				assert.Empty(t, got.MissingInfo)
				assert.NotNil(t, got.Messages)
				assert.Empty(t, got.Messages)
			},
		},
		{
			name: "normal result converts messages and thread replies",
			src: &slacksearch.SlackSearchResult{
				IterationCount: 3,
				TotalMatches:   1,
				IsSufficient:   true,
				MissingInfo:    []string{"deploy time"},
				EnrichedMessages: []slacksearch.EnrichedMessage{
					{
						OriginalMessage: slack.Message{Msg: slack.Msg{
							Channel:   "C123",
							Timestamp: "1700000000.000100",
							User:      "U123",
							Username:  "alice",
							Text:      "Deployment finished",
						}},
						Permalink: "https://slack.example.com/archives/C123/p1700000000000100",
						ThreadMessages: []slack.Message{
							{Msg: slack.Msg{
								Timestamp: "1700000001.000200",
								User:      "U234",
								Username:  "bob",
								Text:      "Thanks for the update",
							}},
						},
					},
				},
			},
			assert: func(t *testing.T, got *slackbot.SlackConversationResult) {
				require.NotNil(t, got)
				assert.Equal(t, 3, got.IterationCount)
				assert.Equal(t, 1, got.TotalMatches)
				assert.True(t, got.IsSufficient)
				require.Equal(t, []string{"deploy time"}, got.MissingInfo)
				require.Len(t, got.Messages, 1)

				msg := got.Messages[0]
				assert.Equal(t, "C123", msg.Channel)
				assert.Equal(t, "1700000000.000100", msg.Timestamp)
				assert.Equal(t, "U123", msg.User)
				assert.Equal(t, "alice", msg.Username)
				assert.Equal(t, "Deployment finished", msg.Text)
				assert.Equal(t, "https://slack.example.com/archives/C123/p1700000000000100", msg.Permalink)
				require.Len(t, msg.Thread, 1)
				assert.Equal(t, slackbot.SlackThreadMessage{
					Timestamp: "1700000001.000200",
					User:      "U234",
					Username:  "bob",
					Text:      "Thanks for the update",
				}, msg.Thread[0])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slackbot.ConvertSlackSearchResult(tt.src)
			tt.assert(t, got)
		})
	}
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
