package slackbot

import (
	"context"
	"fmt"
	"strings"
)

type SlackConversationResult struct {
	IterationCount int
	TotalMatches   int
	IsSufficient   bool
	MissingInfo    []string
	Messages       []SlackConversationMessage
}

// ForPrompt returns a formatted string of Slack messages suitable for LLM context.
func (r *SlackConversationResult) ForPrompt() string {
	if r == nil || len(r.Messages) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Slack Conversations:\n")
	for _, msg := range r.Messages {
		user := msg.Username
		if user == "" {
			user = msg.User
		}
		if user == "" {
			user = "unknown"
		}
		sb.WriteString(fmt.Sprintf("- #%s at %s by %s: %s\n",
			msg.Channel,
			msg.Timestamp,
			user,
			strings.TrimSpace(msg.Text),
		))
		for _, reply := range msg.Thread {
			replyUser := reply.Username
			if replyUser == "" {
				replyUser = reply.User
			}
			if replyUser == "" {
				replyUser = "unknown"
			}
			sb.WriteString(fmt.Sprintf("    • Reply at %s by %s: %s\n",
				reply.Timestamp,
				replyUser,
				strings.TrimSpace(reply.Text),
			))
		}
	}
	return sb.String()
}

type SlackConversationSearcher interface {
	SearchConversations(ctx context.Context, query string, opts SearchOptions) (*SlackConversationResult, error)
}

type SlackConversationMessage struct {
	Channel   string
	Timestamp string
	User      string
	Username  string
	Text      string
	Permalink string
	Thread    []SlackThreadMessage
}

type SlackThreadMessage struct {
	Timestamp string
	User      string
	Username  string
	Text      string
}
