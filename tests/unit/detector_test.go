package unit

import (
	"testing"

	"github.com/ca-srg/mdrag/internal/slackbot"
	"github.com/slack-go/slack"
)

func TestIsMentionToBot(t *testing.T) {
	d := &slackbot.MentionDetector{}
	botID := "U123456"
	ev := &slack.MessageEvent{Msg: slack.Msg{Text: "<@U123456> こんにちは"}}
	if !d.IsMentionToBot(botID, ev) {
		t.Fatalf("expected mention to be detected")
	}

	ev2 := &slack.MessageEvent{Msg: slack.Msg{Text: "ただのメッセージ"}}
	if d.IsMentionToBot(botID, ev2) {
		t.Fatalf("did not expect mention")
	}
}
