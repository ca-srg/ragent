package slackbot

import (
	"context"
	"errors"
	"testing"

	"github.com/slack-go/slack"
)

type mockStatusSetter struct {
	setStatusFunc func(ctx context.Context, params slack.AssistantThreadsSetStatusParameters) error
	calledWith    []slack.AssistantThreadsSetStatusParameters
}

func (m *mockStatusSetter) SetAssistantThreadsStatusContext(ctx context.Context, params slack.AssistantThreadsSetStatusParameters) error {
	m.calledWith = append(m.calledWith, params)
	if m.setStatusFunc != nil {
		return m.setStatusFunc(ctx, params)
	}
	return nil
}

func TestSlackProgressNotifier_Notify(t *testing.T) {
	mock := &mockStatusSetter{}
	notifier := NewSlackProgressNotifier(mock, "C123", "1234567890.123456")

	notifier.Notify(context.Background(), "テスト中...")

	if len(mock.calledWith) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mock.calledWith))
	}
	if mock.calledWith[0].Status != "テスト中..." {
		t.Fatalf("expected status 'テスト中...', got %q", mock.calledWith[0].Status)
	}
	if mock.calledWith[0].ChannelID != "C123" {
		t.Fatalf("expected channel 'C123', got %q", mock.calledWith[0].ChannelID)
	}
	if mock.calledWith[0].ThreadTS != "1234567890.123456" {
		t.Fatalf("expected threadTS '1234567890.123456', got %q", mock.calledWith[0].ThreadTS)
	}
}

func TestSlackProgressNotifier_Notify_ErrorIsLogged(t *testing.T) {
	mock := &mockStatusSetter{
		setStatusFunc: func(ctx context.Context, params slack.AssistantThreadsSetStatusParameters) error {
			return errors.New("slack API error")
		},
	}
	notifier := NewSlackProgressNotifier(mock, "C123", "1234567890.123456")

	notifier.Notify(context.Background(), "テスト中...")
}

func TestProgressNotifierFromContext_ReturnsNilWhenNotSet(t *testing.T) {
	ctx := context.Background()
	n := ProgressNotifierFromContext(ctx)
	if n != nil {
		t.Fatalf("expected nil, got %v", n)
	}
}

func TestProgressNotifierFromContext_ReturnsNotifier(t *testing.T) {
	notifier := &noopProgressNotifier{}
	ctx := ContextWithProgressNotifier(context.Background(), notifier)
	got := ProgressNotifierFromContext(ctx)
	if got == nil {
		t.Fatal("expected notifier, got nil")
	}
	if got != notifier {
		t.Fatalf("expected same notifier, got different: %v", got)
	}
}

func TestNotifyProgress_NilSafe(t *testing.T) {
	ctx := context.Background()
	NotifyProgress(ctx, "テスト")
}

func TestSlackProgressNotifier_NilReceiver(t *testing.T) {
	var n *SlackProgressNotifier
	n.Notify(context.Background(), "テスト")
}

func TestSlackProgressNotifier_NilClient(t *testing.T) {
	n := NewSlackProgressNotifier(nil, "C123", "123.456")
	n.Notify(context.Background(), "テスト")
}
