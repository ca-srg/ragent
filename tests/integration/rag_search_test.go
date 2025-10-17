package integration

import (
	"context"
	"testing"

	"github.com/ca-srg/ragent/internal/slackbot"
	"github.com/slack-go/slack"
)

type okSearch struct{}

func (o *okSearch) Search(ctx context.Context, q string) *slackbot.SearchResult {
	return &slackbot.SearchResult{Items: []slackbot.SearchItem{{Title: "Doc", Snippet: "概要"}}, Total: 1}
}

func TestProcessorEndToEndFormatsResult(t *testing.T) {
	p := slackbot.NewProcessor(&slackbot.MentionDetector{}, &slackbot.QueryExtractor{}, &okSearch{}, &slackbot.Formatter{}, nil)
	r := p.ProcessMessage(context.Background(), "UBOT", &slack.MessageEvent{Msg: slack.Msg{Text: "<@UBOT> help", Channel: "C", User: "U"}})
	if r == nil || len(r.MsgOptions) == 0 {
		t.Fatalf("expected a formatted reply")
	}
}
