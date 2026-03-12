package slackbot

import (
	"context"
	"log"

	"github.com/slack-go/slack"
)

type StatusSetter interface {
	SetAssistantThreadsStatusContext(ctx context.Context, params slack.AssistantThreadsSetStatusParameters) error
}

type ProgressNotifier interface {
	Notify(ctx context.Context, status string)
}

type SlackProgressNotifier struct {
	client    StatusSetter
	channelID string
	threadTS  string
}

func NewSlackProgressNotifier(client StatusSetter, channelID, threadTS string) *SlackProgressNotifier {
	return &SlackProgressNotifier{
		client:    client,
		channelID: channelID,
		threadTS:  threadTS,
	}
}

func (n *SlackProgressNotifier) Notify(ctx context.Context, status string) {
	if n == nil || n.client == nil {
		return
	}

	err := n.client.SetAssistantThreadsStatusContext(ctx, slack.AssistantThreadsSetStatusParameters{
		ChannelID: n.channelID,
		ThreadTS:  n.threadTS,
		Status:    status,
	})
	if err != nil {
		log.Printf("progress_notify_error status=%q err=%v", status, err)
	}
}

type noopProgressNotifier struct{}

func (n *noopProgressNotifier) Notify(ctx context.Context, status string) {}

type progressNotifierContextKey struct{}

func ContextWithProgressNotifier(ctx context.Context, notifier ProgressNotifier) context.Context {
	return context.WithValue(ctx, progressNotifierContextKey{}, notifier)
}

func ProgressNotifierFromContext(ctx context.Context) ProgressNotifier {
	n, _ := ctx.Value(progressNotifierContextKey{}).(ProgressNotifier)
	return n
}

func NotifyProgress(ctx context.Context, status string) {
	n := ProgressNotifierFromContext(ctx)
	if n == nil {
		return
	}
	n.Notify(ctx, status)
}
