package integration

import (
	"context"
	"sync"
	"testing"

	"github.com/ca-srg/mdrag/internal/slackbot"
	"github.com/slack-go/slack"
)

type nullSearch struct{}

func (n *nullSearch) Search(ctx context.Context, q string) *slackbot.SearchResult {
	return &slackbot.SearchResult{}
}

func TestProcessorHandlesConcurrentMentions(t *testing.T) {
	p := slackbot.NewProcessor(&slackbot.MentionDetector{}, &slackbot.QueryExtractor{}, &nullSearch{}, &slackbot.Formatter{})
	var wg sync.WaitGroup
	n := 10
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = p.ProcessMessage(context.Background(), "UBOT", &slack.MessageEvent{Msg: slack.Msg{Text: "<@UBOT> ping", Channel: "C", User: "U"}})
		}()
	}
	wg.Wait()
}
