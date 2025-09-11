package contract

import (
	"context"
	"testing"

	"github.com/ca-srg/ragent/internal/slackbot"
	"github.com/slack-go/slack"
)

type spyingSearch struct{ got string }

func (s *spyingSearch) Search(ctx context.Context, q string) *slackbot.SearchResult {
	s.got = q
	return &slackbot.SearchResult{}
}

func TestSearchInterfaceIsCalledWithExtractedQuery(t *testing.T) {
	spy := &spyingSearch{}
	proc := slackbot.NewProcessor(&slackbot.MentionDetector{}, &slackbot.QueryExtractor{}, spy, &slackbot.Formatter{})
	ev := &slack.MessageEvent{Msg: slack.Msg{Text: "<@UBOT> 予定の確認", Channel: "C1", User: "U1"}}
	_ = proc.ProcessMessage(context.Background(), "UBOT", ev)
	if spy.got != "予定の確認" {
		t.Fatalf("expected search query to be '予定の確認', got %q", spy.got)
	}
}
