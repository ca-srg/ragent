package slackbot

import "context"

type SlackConversationResult struct {
	IterationCount int
	TotalMatches   int
	IsSufficient   bool
	MissingInfo    []string
	Messages       []SlackConversationMessage
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
