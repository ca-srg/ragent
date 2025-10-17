package integration

import (
	"context"
	"testing"

	"github.com/ca-srg/ragent/internal/slackbot"
	"github.com/slack-go/slack"
)

// fakeSearch simply echoes the query
type fakeSearch struct{}

func (f *fakeSearch) Search(ctx context.Context, q string) *slackbot.SearchResult {
	return &slackbot.SearchResult{}
}

func TestProcessorDetectsMentionAndBuildsReply(t *testing.T) {
	p := slackbot.NewProcessor(&slackbot.MentionDetector{}, &slackbot.QueryExtractor{}, &fakeSearch{}, &slackbot.Formatter{}, nil)
	ev := &slack.MessageEvent{Msg: slack.Msg{Text: "<@UBOT> 使い方教えて", Channel: "C123", User: "U42"}}
	r := p.ProcessMessage(context.Background(), "UBOT", ev)
	if r == nil {
		t.Fatalf("expected a reply for a mention")
	}
	if r.Channel != "C123" {
		t.Fatalf("unexpected channel: %s", r.Channel)
	}
}
