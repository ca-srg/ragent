package slackbot

import (
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"testing"

	"github.com/ca-srg/ragent/internal/pkg/config"
	"github.com/slack-go/slack"
)

func TestThreadContextBuilder_BuildFormatsHistory(t *testing.T) {
	mock := &mockSlackClient{
		repliesFunc: func(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error) {
			return []slack.Message{
				{Msg: slack.Msg{User: "U1", Text: "first message should be dropped"}},
				{Msg: slack.Msg{User: "U2", Text: "second message"}},
				{Msg: slack.Msg{BotID: "B123", Text: "Thanks for the update."}},
			}, false, "", nil
		},
	}
	cfg := &config.SlackConfig{
		ThreadContextEnabled:     true,
		ThreadContextMaxMessages: 2,
	}
	builder := NewThreadContextBuilder(mock, cfg, log.New(io.Discard, "", 0))

	current := "最新の状況は？"
	result, err := builder.Build(context.Background(), "C1", "123.456", current)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Bot-authored messages are intentionally excluded from history to
	// prevent reply loops where the bot's own past output reseeds the query.
	expected := "[過去の会話]\nユーザー: second message\n\n[現在の質問]\n" + current
	if result != expected {
		t.Fatalf("unexpected formatted result:\nwant: %q\ngot : %q", expected, result)
	}
	if strings.Contains(result, "first message") {
		t.Fatalf("result should respect maxMessages limit: %s", result)
	}
	if strings.Contains(result, "Thanks for the update") {
		t.Fatalf("bot-authored messages must be skipped: %s", result)
	}
}

func TestThreadContextBuilder_ReturnsCurrentQueryWhenDisabled(t *testing.T) {
	cfg := &config.SlackConfig{
		ThreadContextEnabled:     false,
		ThreadContextMaxMessages: 5,
	}
	builder := NewThreadContextBuilder(&mockSlackClient{}, cfg, log.New(io.Discard, "", 0))

	current := "disable test"
	result, err := builder.Build(context.Background(), "C1", "123", current)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != current {
		t.Fatalf("expected current query when disabled, got %q", result)
	}
}

func TestThreadContextBuilder_ReturnsCurrentQueryWhenNoThread(t *testing.T) {
	cfg := &config.SlackConfig{
		ThreadContextEnabled:     true,
		ThreadContextMaxMessages: 5,
	}
	builder := NewThreadContextBuilder(&mockSlackClient{}, cfg, log.New(io.Discard, "", 0))

	current := "no thread timestamp"
	result, err := builder.Build(context.Background(), "C1", "", current)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != current {
		t.Fatalf("expected current query when thread timestamp is empty, got %q", result)
	}
}

func TestThreadContextBuilder_FallsBackOnError(t *testing.T) {
	wantErr := errors.New("slack down")
	mock := &mockSlackClient{
		repliesFunc: func(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error) {
			return nil, false, "", wantErr
		},
	}
	cfg := &config.SlackConfig{
		ThreadContextEnabled:     true,
		ThreadContextMaxMessages: 5,
	}
	builder := NewThreadContextBuilder(mock, cfg, log.New(io.Discard, "", 0))

	current := "error scenario"
	result, err := builder.Build(context.Background(), "C1", "111", current)
	if result != current {
		t.Fatalf("expected current query on error, got %q", result)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected error %v, got %v", wantErr, err)
	}
}

func TestThreadContextBuilder_ReturnsCurrentQueryForEmptyThread(t *testing.T) {
	mock := &mockSlackClient{
		repliesFunc: func(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error) {
			return []slack.Message{}, false, "", nil
		},
	}
	cfg := &config.SlackConfig{
		ThreadContextEnabled:     true,
		ThreadContextMaxMessages: 5,
	}
	builder := NewThreadContextBuilder(mock, cfg, log.New(io.Discard, "", 0))

	current := "empty history"
	result, err := builder.Build(context.Background(), "C1", "111", current)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != current {
		t.Fatalf("expected current query for empty history, got %q", result)
	}
}

func TestThreadContextBuilder_SkipsBotMessages(t *testing.T) {
	longText := strings.Repeat("甲", 250)
	mock := &mockSlackClient{
		repliesFunc: func(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error) {
			return []slack.Message{
				{Msg: slack.Msg{BotID: "B1", Text: longText}},
				{Msg: slack.Msg{SubType: "bot_message", Text: "subtype bot reply"}},
				{Msg: slack.Msg{Username: "RAGent", Text: "username bot reply"}},
			}, false, "", nil
		},
	}
	cfg := &config.SlackConfig{
		ThreadContextEnabled:     true,
		ThreadContextMaxMessages: 5,
	}
	builder := NewThreadContextBuilder(mock, cfg, log.New(io.Discard, "", 0))

	current := "skip-bot test"
	result, err := builder.Build(context.Background(), "C1", "999", current)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With only bot messages in the thread, history should be empty and the
	// builder must return the current query unchanged.
	if result != current {
		t.Fatalf("expected current query when only bot messages exist, got %q", result)
	}
	if strings.ContainsRune(result, '甲') {
		t.Fatalf("bot text must not appear in history: %q", result)
	}
	if strings.Contains(result, "subtype bot reply") || strings.Contains(result, "username bot reply") {
		t.Fatalf("bot-authored messages must be skipped: %q", result)
	}
}

type mockSlackClient struct {
	repliesFunc func(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error)
}

func (m *mockSlackClient) AuthTest() (*slack.AuthTestResponse, error)   { return nil, nil }
func (m *mockSlackClient) NewRTM(options ...slack.RTMOption) *slack.RTM { return nil }
func (m *mockSlackClient) PostMessage(channelID string, options ...slack.MsgOption) (string, string, error) {
	return "", "", nil
}
func (m *mockSlackClient) SetAssistantThreadsStatusContext(ctx context.Context, params slack.AssistantThreadsSetStatusParameters) error {
	return nil
}
func (m *mockSlackClient) GetConversationReplies(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error) {
	if m.repliesFunc != nil {
		return m.repliesFunc(params)
	}
	return nil, false, "", nil
}
