package slacksearch

import (
	"context"

	"github.com/ca-srg/ragent/internal/slackbot"
)

// SlackConversationSearcher is the interface for searching Slack conversations.
// It is consumed by the slackbot adapter layer (HybridSearchAdapter, SlackOnlySearchAdapter)
// and implemented in the cmd layer by botSlackSearcher, which delegates to SlackSearchService.
type SlackConversationSearcher interface {
	SearchConversations(ctx context.Context, query string, opts slackbot.SearchOptions) (*slackbot.SlackConversationResult, error)
}
