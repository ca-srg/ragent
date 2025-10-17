package slackbot

import (
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"testing"

	"github.com/ca-srg/ragent/internal/config"
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
	expected := "[過去の会話]\nユーザー: second message\nBOT: Thanks for the update.\n\n[現在の質問]\n" + current
	if result != expected {
		t.Fatalf("unexpected formatted result:\nwant: %q\ngot : %q", expected, result)
	}
	if strings.Contains(result, "first message") {
		t.Fatalf("result should respect maxMessages limit: %s", result)
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

func TestThreadContextBuilder_TruncatesBotMessages(t *testing.T) {
	longText := strings.Repeat("甲", 250)
	mock := &mockSlackClient{
		repliesFunc: func(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error) {
			return []slack.Message{
				{Msg: slack.Msg{BotID: "B1", Text: longText}},
			}, false, "", nil
		},
	}
	cfg := &config.SlackConfig{
		ThreadContextEnabled:     true,
		ThreadContextMaxMessages: 5,
	}
	builder := NewThreadContextBuilder(mock, cfg, log.New(io.Discard, "", 0))

	current := "truncation test"
	result, err := builder.Build(context.Background(), "C1", "999", current)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedBotText := strings.Repeat("甲", 200)
	if !strings.Contains(result, "BOT: "+expectedBotText) {
		t.Fatalf("expected truncated bot text (%d chars), got %q", len(expectedBotText), result)
	}
	if strings.Contains(result, strings.Repeat("甲", 201)) {
		t.Fatalf("bot text should be truncated to 200 characters")
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
func (m *mockSlackClient) GetConversationReplies(params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error) {
	if m.repliesFunc != nil {
		return m.repliesFunc(params)
	}
	return nil, false, "", nil
}
