package slackbot

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/slack-go/slack"
)

// MentionDetector determines if a message mentions the bot
type MentionDetector struct{}

// IsMentionToBot returns true if message text includes a mention to the bot or thread broadcast
func (d *MentionDetector) IsMentionToBot(botUserID string, msg *slack.MessageEvent) bool {
	if msg == nil {
		return false
	}
	// pattern matches <@U12345> at start or anywhere
	mention := fmt.Sprintf("<@%s>", botUserID)
	if strings.Contains(msg.Text, mention) {
		return true
	}
	// Also consider the case where the bot is the thread parent and the message uses broadcast mention
	// e.g., <!here> or <!channel> shouldn't trigger; ignore for now
	return false
}

var angleMention = regexp.MustCompile(`<@([A-Z0-9]+)>`)

// StripMentions removes all <@U123> tokens from text
func StripMentions(text string) string {
	return strings.TrimSpace(angleMention.ReplaceAllString(text, ""))
}
