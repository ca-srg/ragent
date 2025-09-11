package unit

import (
	"testing"
	"time"

	"github.com/ca-srg/mdrag/internal/slackbot"
	"github.com/slack-go/slack"
)

func TestFormatterSearchResult(t *testing.T) {
	f := &slackbot.Formatter{}
	res := &slackbot.SearchResult{
		Items:   []slackbot.SearchItem{{Title: "Doc1", Snippet: "概要"}},
		Total:   1,
		Elapsed: 120 * time.Millisecond,
	}
	opt := f.BuildSearchResult("テストクエリ", res)
	if opt == nil {
		t.Fatalf("expected MsgOption not nil")
	}
	// Ensure it can be applied to a message; no panic
	_ = []slack.MsgOption{opt}
}
