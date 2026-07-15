package slacksearch

import (
	"context"
	"errors"
	"io"
	"log"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestMessageFetcherPreservesPartialMessageWhenThreadFetchFails(t *testing.T) {
	const messageTS = "1234567890.123456"
	const originalURL = "https://example-workspace.slack.com/archives/C01234567/p1234567890123456"
	root := slack.Message{Msg: slack.Msg{Channel: "C01234567", Timestamp: messageTS, Text: "root", ReplyCount: 1}}
	client := &mockSlackConversationClient{}
	client.On("GetConversationHistoryContext", mock.Anything, mock.Anything).
		Return(&slack.GetConversationHistoryResponse{Messages: []slack.Message{root}}, nil)
	client.On("GetPermalinkContext", mock.Anything, mock.Anything).Return("", errors.New("permalink unavailable"))
	client.On("GetConversationRepliesContext", mock.Anything, mock.Anything).
		Return(nil, false, "", errors.New("thread unavailable"))
	fetcher := &MessageFetcher{client: client, apiTimeout: time.Second, logger: log.New(io.Discard, "", 0)}

	response, err := fetcher.FetchByURLs(context.Background(), &FetchRequest{URLs: []*SlackURLInfo{{
		ChannelID: "C01234567", MessageTS: messageTS, OriginalURL: originalURL,
	}}})

	require.NoError(t, err)
	require.Len(t, response.EnrichedMessages, 1)
	assert.Equal(t, originalURL, response.EnrichedMessages[0].Permalink)
	assert.Empty(t, response.EnrichedMessages[0].ThreadMessages)
	assert.Equal(t, 1, response.FetchedCount)
	assert.Empty(t, response.Errors)
	require.Len(t, response.Warnings, 1)
	assert.Contains(t, response.Warnings[0], "thread replies could not be fetched")
	client.AssertExpectations(t)
}

func TestMessageFetcherFetchesRepliesForRootMessage(t *testing.T) {
	const messageTS = "1234567890.123456"
	root := slack.Message{Msg: slack.Msg{Channel: "C01234567", Timestamp: messageTS, Text: "root", ReplyCount: 1}}
	reply := slack.Message{Msg: slack.Msg{Timestamp: "1234567890.654321", Text: "reply"}}
	client := &mockSlackConversationClient{}
	client.On("GetConversationHistoryContext", mock.Anything, mock.Anything).
		Return(&slack.GetConversationHistoryResponse{Messages: []slack.Message{root}}, nil)
	client.On("GetPermalinkContext", mock.Anything, mock.Anything).Return("https://example.slack.com/message", nil)
	client.On("GetConversationRepliesContext", mock.Anything, mock.Anything).
		Return([]slack.Message{root, reply}, false, "", nil)
	fetcher := &MessageFetcher{client: client, apiTimeout: time.Second, logger: log.New(io.Discard, "", 0)}

	response, err := fetcher.FetchByURLs(context.Background(), &FetchRequest{URLs: []*SlackURLInfo{{
		ChannelID: "C01234567", MessageTS: messageTS,
	}}})

	require.NoError(t, err)
	require.Len(t, response.EnrichedMessages, 1)
	require.Len(t, response.EnrichedMessages[0].ThreadMessages, 1)
	assert.Equal(t, "reply", response.EnrichedMessages[0].ThreadMessages[0].Text)
	assert.Equal(t, "C01234567", response.EnrichedMessages[0].ThreadMessages[0].Channel)
	assert.Equal(t, 1, response.FetchedCount)
	assert.Empty(t, response.Errors)
	assert.Empty(t, response.Warnings)
	client.AssertExpectations(t)
}
