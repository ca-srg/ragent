package slacksearch

import (
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
		fmt.Fprintf(&sb, "- #%s at %s by %s: %s\n",
			msg.Channel,
			msg.Timestamp,
			user,
			strings.TrimSpace(msg.Text),
		)
		for _, prev := range msg.Previous {
			prevUser := prev.Username
			if prevUser == "" {
				prevUser = prev.User
			}
			if prevUser == "" {
				prevUser = "unknown"
			}
			fmt.Fprintf(&sb, "    • Previous at %s by %s: %s\n",
				prev.Timestamp,
				prevUser,
				strings.TrimSpace(prev.Text),
			)
		}
		for _, reply := range msg.Thread {
			replyUser := reply.Username
			if replyUser == "" {
				replyUser = reply.User
			}
			if replyUser == "" {
				replyUser = "unknown"
			}
			fmt.Fprintf(&sb, "    • Reply at %s by %s: %s\n",
				reply.Timestamp,
				replyUser,
				strings.TrimSpace(reply.Text),
			)
		}
		for _, next := range msg.Next {
			nextUser := next.Username
			if nextUser == "" {
				nextUser = next.User
			}
			if nextUser == "" {
				nextUser = "unknown"
			}
			fmt.Fprintf(&sb, "    • Next at %s by %s: %s\n",
				next.Timestamp,
				nextUser,
				strings.TrimSpace(next.Text),
			)
		}
	}
	return sb.String()
}

type SlackConversationMessage struct {
	Channel   string
	Timestamp string
	User      string
	Username  string
	Text      string
	Permalink string
	Thread    []SlackThreadMessage
	Previous  []SlackThreadMessage
	Next      []SlackThreadMessage
}

type SlackThreadMessage struct {
	Timestamp string
	User      string
	Username  string
	Text      string
}
