package slacksearch

import "context"

// SlackConversationSearcher is the interface for searching Slack conversations.
// It is consumed by the slackbot adapter layer (HybridSearchAdapter, SlackOnlySearchAdapter)
// and implemented in the cmd layer by botSlackSearcher, which delegates to SlackSearchService.
type SlackConversationSearcher interface {
	SearchConversations(ctx context.Context, query string, opts SearchOptions) (*SlackConversationResult, error)
}
