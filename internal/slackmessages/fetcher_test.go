package slackmessages

import (
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"golang.org/x/time/rate"
)

func TestMessageFetcher_FetchMessagesBasic(t *testing.T) {
	mock := &mockSlackAPI{
		conversationInfoFunc: func(input *slack.GetConversationInfoInput) (*slack.Channel, error) {
			ch := newChannel(input.ChannelID, "general")
			return &ch, nil
		},
		historyFunc: func(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
			return &slack.GetConversationHistoryResponse{
				Messages: []slack.Message{
					{
						Msg: slack.Msg{
							User:      "U123",
							Text:      "hello world",
							Timestamp: "1700000000.123456",
							Team:      "T123",
							Permalink: "https://slack.example.com/archives/C123/p1700000000123456",
						},
					},
				},
			}, nil
		},
		userInfoFunc: func(user string) (*slack.User, error) {
			return &slack.User{
				Name: "john",
				Profile: slack.UserProfile{
					DisplayName: "John Doe",
				},
				RealName: "Johnathan Doe",
			}, nil
		},
		permalinkFunc: func(params *slack.PermalinkParameters) (string, error) {
			return "https://slack.example.com/archives/C123/p1700000000123456", nil
		},
	}

	logger := log.New(io.Discard, "", 0)
	fetcher := NewMessageFetcher(
		mock,
		WithRateLimiter(rate.NewLimiter(rate.Every(time.Millisecond), 10)),
		WithLogger(logger),
		WithBackoffBase(10*time.Millisecond),
	)

	cfg := FetchConfig{
		ChannelIDs: []string{"C123"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	messages, err := fetcher.FetchMessages(ctx, cfg)
	assert.NoError(t, err)
	if assert.Len(t, messages, 1) {
		msg := messages[0]
		assert.Equal(t, "C123", msg.ChannelID)
		assert.Equal(t, "general", msg.ChannelName)
		assert.Equal(t, "U123", msg.UserID)
		assert.Equal(t, "John Doe", msg.UserName)
		assert.Equal(t, "hello world", msg.Text)
		assert.Equal(t, "slack-C123-1700000000.123456", msg.GenerateVectorID())

		metadata := msg.ToDocumentMetadata()
		assert.Equal(t, "#general - John Doe", metadata.Title)
		assert.Equal(t, "slack-message", metadata.Category)
		assert.Equal(t, "https://slack.example.com/archives/C123/p1700000000123456", metadata.Reference)
	}
}

func TestMessageFetcher_ExcludeBotsAndShortMessages(t *testing.T) {
	mock := &mockSlackAPI{
		conversationInfoFunc: func(input *slack.GetConversationInfoInput) (*slack.Channel, error) {
			ch := newChannel(input.ChannelID, "alerts")
			return &ch, nil
		},
		historyFunc: func(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
			return &slack.GetConversationHistoryResponse{
				Messages: []slack.Message{
					{Msg: slack.Msg{User: "U123", Text: "hi", Timestamp: "1700000000.000001"}},
					{Msg: slack.Msg{BotID: "B1", Text: "bot message", Timestamp: "1700000001.000001"}},
				},
			}, nil
		},
		userInfoFunc: func(user string) (*slack.User, error) {
			return &slack.User{Name: "user"}, nil
		},
		permalinkFunc: func(params *slack.PermalinkParameters) (string, error) {
			return "https://example.com", nil
		},
	}

	fetcher := NewMessageFetcher(
		mock,
		WithRateLimiter(rate.NewLimiter(rate.Every(time.Millisecond), 10)),
		WithLogger(log.New(io.Discard, "", 0)),
	)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	messages, err := fetcher.FetchMessages(ctx, FetchConfig{
		ChannelIDs:       []string{"C999"},
		ExcludeBots:      true,
		MinMessageLength: 5,
	})
	assert.NoError(t, err)
	assert.Empty(t, messages)
}

func TestMessageFetcher_IncludeThreads(t *testing.T) {
	mock := &mockSlackAPI{
		conversationInfoFunc: func(input *slack.GetConversationInfoInput) (*slack.Channel, error) {
			ch := newChannel(input.ChannelID, "support")
			return &ch, nil
		},
		historyFunc: func(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
			return &slack.GetConversationHistoryResponse{
				Messages: []slack.Message{
					{Msg: slack.Msg{
						User:            "U111",
						Text:            "root message",
						Timestamp:       "1700000002.000001",
						ThreadTimestamp: "1700000002.000001",
					}},
				},
			}, nil
		},
		repliesFunc: func(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error) {
			return []slack.Message{
				{Msg: slack.Msg{User: "U111", Text: "root message", Timestamp: "1700000002.000001", ThreadTimestamp: "1700000002.000001"}},
				{Msg: slack.Msg{User: "U222", Text: "first reply", Timestamp: "1700000003.000001", ThreadTimestamp: "1700000002.000001"}},
				{Msg: slack.Msg{User: "U333", Text: "second reply", Timestamp: "1700000004.000001", ThreadTimestamp: "1700000002.000001"}},
			}, false, "", nil
		},
		userInfoFunc: func(user string) (*slack.User, error) {
			return &slack.User{
				Name: user,
				Profile: slack.UserProfile{
					DisplayName: strings.ToUpper(user),
				},
			}, nil
		},
		permalinkFunc: func(params *slack.PermalinkParameters) (string, error) {
			return "https://example.com", nil
		},
	}

	fetcher := NewMessageFetcher(
		mock,
		WithRateLimiter(rate.NewLimiter(rate.Every(time.Millisecond), 10)),
		WithLogger(log.New(io.Discard, "", 0)),
	)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	messages, err := fetcher.FetchMessages(ctx, FetchConfig{
		ChannelIDs:     []string{"C777"},
		IncludeThreads: true,
	})
	assert.NoError(t, err)
	if assert.Len(t, messages, 3) {
		assert.False(t, messages[0].IsThreadReply)
		assert.True(t, messages[1].IsThreadReply)
		assert.True(t, messages[2].IsThreadReply)
		assert.Equal(t, "first reply", messages[1].Text)
		assert.Equal(t, "second reply", messages[2].Text)
	}
}

func TestMessageFetcher_RetryOnRateLimit(t *testing.T) {
	var calls int
	mock := &mockSlackAPI{
		conversationInfoFunc: func(input *slack.GetConversationInfoInput) (*slack.Channel, error) {
			ch := newChannel(input.ChannelID, "general")
			return &ch, nil
		},
		historyFunc: func(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
			calls++
			if calls == 1 {
				return nil, &slack.RateLimitedError{RetryAfter: 0}
			}
			return &slack.GetConversationHistoryResponse{
				Messages: []slack.Message{
					{Msg: slack.Msg{User: "U1", Text: "retry success", Timestamp: "1700000050.000001"}},
				},
			}, nil
		},
		userInfoFunc: func(user string) (*slack.User, error) {
			return &slack.User{Name: user}, nil
		},
		permalinkFunc: func(params *slack.PermalinkParameters) (string, error) {
			return "https://example.com", nil
		},
	}

	fetcher := NewMessageFetcher(
		mock,
		WithRateLimiter(rate.NewLimiter(rate.Every(time.Millisecond), 10)),
		WithLogger(log.New(io.Discard, "", 0)),
		WithBackoffBase(5*time.Millisecond),
		WithMaxRetries(3),
	)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	messages, err := fetcher.FetchMessages(ctx, FetchConfig{
		ChannelIDs: []string{"C123"},
	})
	assert.NoError(t, err)
	assert.Len(t, messages, 1)
	assert.Equal(t, 2, calls)
}

type mockSlackAPI struct {
	conversationsFunc    func(params *slack.GetConversationsParameters) ([]slack.Channel, string, error)
	conversationInfoFunc func(input *slack.GetConversationInfoInput) (*slack.Channel, error)
	historyFunc          func(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error)
	repliesFunc          func(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error)
	userInfoFunc         func(userID string) (*slack.User, error)
	permalinkFunc        func(params *slack.PermalinkParameters) (string, error)
}

func (m *mockSlackAPI) GetConversations(params *slack.GetConversationsParameters) ([]slack.Channel, string, error) {
	if m.conversationsFunc != nil {
		return m.conversationsFunc(params)
	}
	return nil, "", nil
}

func (m *mockSlackAPI) GetConversationInfo(input *slack.GetConversationInfoInput) (*slack.Channel, error) {
	if m.conversationInfoFunc != nil {
		return m.conversationInfoFunc(input)
	}
	return nil, errors.New("not implemented")
}

func (m *mockSlackAPI) GetConversationHistory(params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
	if m.historyFunc != nil {
		return m.historyFunc(params)
	}
	return nil, errors.New("not implemented")
}

func (m *mockSlackAPI) GetConversationReplies(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error) {
	if m.repliesFunc != nil {
		return m.repliesFunc(params)
	}
	return nil, false, "", nil
}

func (m *mockSlackAPI) GetUserInfo(userID string) (*slack.User, error) {
	if m.userInfoFunc != nil {
		return m.userInfoFunc(userID)
	}
	return nil, errors.New("not implemented")
}

func (m *mockSlackAPI) GetPermalink(params *slack.PermalinkParameters) (string, error) {
	if m.permalinkFunc != nil {
		return m.permalinkFunc(params)
	}
	return "", errors.New("not implemented")
}

func newChannel(id, name string) slack.Channel {
	return slack.Channel{
		GroupConversation: slack.GroupConversation{
			Conversation: slack.Conversation{
				ID:             id,
				NameNormalized: name,
			},
			Name: name,
		},
		IsChannel: true,
		IsGeneral: name == "general",
	}
}
