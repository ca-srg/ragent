package contract

import (
	"testing"
	"time"

	"github.com/ca-srg/mdrag/internal/slackbot"
)

func TestBuildSearchResult_EmptyAndSingle(t *testing.T) {
	f := &slackbot.Formatter{}
	// Empty
	optEmpty := f.BuildSearchResult("Q", &slackbot.SearchResult{Items: nil, Total: 0, Elapsed: 10 * time.Millisecond})
	if optEmpty == nil {
		t.Fatal("expected MsgOption for empty result")
	}

	// Single item
	res := &slackbot.SearchResult{Items: []slackbot.SearchItem{{Title: "Doc", Snippet: "概要", Link: "https://ex"}}, Total: 1, Elapsed: 20 * time.Millisecond}
	optOne := f.BuildSearchResult("Q", res)
	if optOne == nil {
		t.Fatal("expected MsgOption for single item")
	}
}
