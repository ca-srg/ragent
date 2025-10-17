package integration

import (
	"context"
	"testing"

	"github.com/ca-srg/ragent/internal/slackbot"
	"github.com/slack-go/slack"
)

type failingSearch struct{}

func (f *failingSearch) Search(ctx context.Context, q string) *slackbot.SearchResult {
	return &slackbot.SearchResult{}
}

func TestProcessorHandlesEmptyQuery(t *testing.T) {
	p := slackbot.NewProcessor(&slackbot.MentionDetector{}, &slackbot.QueryExtractor{}, &failingSearch{}, &slackbot.Formatter{}, nil)
	r := p.ProcessMessage(context.Background(), "UBOT", &slack.MessageEvent{Msg: slack.Msg{Text: "<@UBOT>    ", Channel: "C", User: "U"}})
	if r == nil {
		t.Fatalf("expected usage hint reply for empty query")
	}
}
