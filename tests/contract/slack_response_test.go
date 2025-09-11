package contract

import (
	"testing"
	"time"

	"github.com/ca-srg/mdrag/internal/slackbot"
)

func TestSlackResponseBlocksShape(t *testing.T) {
	f := &slackbot.Formatter{}
	res := &slackbot.SearchResult{
		Items:   []slackbot.SearchItem{{Title: "タイトル", Snippet: "抜粋"}},
		Total:   1,
		Elapsed: 200 * time.Millisecond,
	}
	opt := f.BuildSearchResult("クエリ", res)
	if opt == nil {
		t.Fatal("expected non-nil MsgOption")
	}
}
