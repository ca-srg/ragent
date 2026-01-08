package slacksearch

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ca-srg/ragent/internal/slackbot"
	"github.com/slack-go/slack"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	defaultFetchTimeout = 10 * time.Second
	maxFetchURLs        = 10 // Maximum number of URLs to fetch in a single request
)

// MessageFetcher retrieves Slack messages by URL.
type MessageFetcher struct {
	client      slackConversationClient
	rateLimiter *slackbot.RateLimiter
	apiTimeout  time.Duration
	logger      *log.Logger
}

// FetchRequest contains the URL info to fetch.
type FetchRequest struct {
	URLs      []*SlackURLInfo
	UserQuery string // Original query for context
}

// FetchResponse contains fetched messages.
type FetchResponse struct {
	EnrichedMessages []EnrichedMessage
	FetchedCount     int
	Errors           []string
}

// NewMessageFetcher creates a new MessageFetcher instance.
func NewMessageFetcher(client *slack.Client, rateLimiter *slackbot.RateLimiter, config *SlackSearchConfig, logger *log.Logger) *MessageFetcher {
	if logger == nil {
		logger = log.Default()
	}

	timeout := defaultFetchTimeout
	if config != nil && config.TimeoutSeconds > 0 {
		timeout = time.Duration(config.TimeoutSeconds) * time.Second
	}

	return &MessageFetcher{
		client:      client,
		rateLimiter: rateLimiter,
		apiTimeout:  timeout,
		logger:      logger,
	}
}

// FetchByURLs retrieves messages from the given Slack URLs.
// Returns enriched messages with thread context and permalinks.
func (f *MessageFetcher) FetchByURLs(ctx context.Context, req *FetchRequest) (*FetchResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	ctx, span := slackSearchTracer.Start(ctx, "slacksearch.fetch_by_urls")
	defer span.End()

	if req == nil || len(req.URLs) == 0 {
		span.SetAttributes(attribute.Int("slack.url_count", 0))
		return &FetchResponse{}, nil
	}

	// Limit the number of URLs to fetch
	urls := req.URLs
	if len(urls) > maxFetchURLs {
		urls = urls[:maxFetchURLs]
		f.logger.Printf("MessageFetcher: truncated URL count from %d to %d", len(req.URLs), maxFetchURLs)
	}

	span.SetAttributes(
		attribute.Int("slack.url_count", len(urls)),
		attribute.String("slack.query_hash", telemetryFingerprint(req.UserQuery)),
	)

	response := &FetchResponse{
		EnrichedMessages: make([]EnrichedMessage, 0, len(urls)),
		Errors:           make([]string, 0),
	}

	for _, urlInfo := range urls {
		select {
		case <-ctx.Done():
			err := ctx.Err()
			span.RecordError(err)
			span.SetStatus(codes.Error, "context_cancelled")
			return response, err
		default:
		}

		enriched, err := f.fetchSingleMessage(ctx, urlInfo)
		if err != nil {
			errMsg := fmt.Sprintf("failed to fetch %s: %v", urlInfo.OriginalURL, err)
			f.logger.Printf("MessageFetcher: %s", errMsg)
			response.Errors = append(response.Errors, errMsg)
			span.AddEvent("fetch_error", trace.WithAttributes(
				attribute.String("slack.url", urlInfo.OriginalURL),
				attribute.String("error", err.Error()),
			))
			continue
		}

		if enriched != nil {
			response.EnrichedMessages = append(response.EnrichedMessages, *enriched)
			response.FetchedCount++
		}
	}

	span.SetAttributes(
		attribute.Int("slack.fetched_count", response.FetchedCount),
		attribute.Int("slack.error_count", len(response.Errors)),
	)

	f.logger.Printf("MessageFetcher: fetched %d/%d messages", response.FetchedCount, len(urls))
	return response, nil
}

// fetchSingleMessage retrieves a single message by channel and timestamp.
func (f *MessageFetcher) fetchSingleMessage(ctx context.Context, info *SlackURLInfo) (*EnrichedMessage, error) {
	if info == nil {
		return nil, fmt.Errorf("nil URL info")
	}

	ctx, span := slackSearchTracer.Start(ctx, "slacksearch.fetch_single_message")
	defer span.End()

	span.SetAttributes(
		attribute.String("slack.channel_id", info.ChannelID),
		attribute.String("slack.message_ts", info.MessageTS),
		attribute.Bool("slack.has_thread_ts", info.ThreadTS != ""),
	)

	// Rate limit check
	if err := f.allowRate(); err != nil {
		span.RecordError(err)
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, f.apiTimeout)
	defer cancel()

	var (
		message *slack.Message
		err     error
	)

	// If thread_ts is specified, fetch from thread replies
	if info.ThreadTS != "" {
		message, err = f.fetchFromThread(ctx, info)
	} else {
		message, err = f.fetchFromHistory(ctx, info)
	}

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "fetch_failed")
		return nil, err
	}

	if message == nil {
		return nil, fmt.Errorf("message not found: %s/%s", info.ChannelID, info.MessageTS)
	}

	// Build enriched message
	enriched := &EnrichedMessage{
		OriginalMessage: *message,
	}

	// Fetch permalink
	permalink, err := f.getPermalink(ctx, info.ChannelID, info.MessageTS)
	if err != nil {
		f.logger.Printf("MessageFetcher: failed to get permalink for %s: %v", info.MessageTS, err)
	} else {
		enriched.Permalink = permalink
	}

	// Fetch thread replies if the message has a thread
	if message.ThreadTimestamp != "" && message.ThreadTimestamp != message.Timestamp {
		threadMessages, err := f.fetchThreadReplies(ctx, info.ChannelID, message.ThreadTimestamp, info.MessageTS)
		if err != nil {
			f.logger.Printf("MessageFetcher: failed to fetch thread replies: %v", err)
		} else {
			enriched.ThreadMessages = threadMessages
		}
	}

	span.SetAttributes(
		attribute.Bool("slack.has_permalink", enriched.Permalink != ""),
		attribute.Int("slack.thread_replies", len(enriched.ThreadMessages)),
	)

	return enriched, nil
}

// fetchFromHistory retrieves a message using conversation history API.
func (f *MessageFetcher) fetchFromHistory(ctx context.Context, info *SlackURLInfo) (*slack.Message, error) {
	params := &slack.GetConversationHistoryParameters{
		ChannelID: info.ChannelID,
		Oldest:    info.MessageTS,
		Latest:    info.MessageTS,
		Limit:     1,
		Inclusive: true,
	}

	history, err := f.client.GetConversationHistoryContext(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("GetConversationHistory failed: %w", err)
	}

	if len(history.Messages) == 0 {
		return nil, fmt.Errorf("no message found at timestamp %s", info.MessageTS)
	}

	msg := history.Messages[0]
	msg.Channel = info.ChannelID // Ensure channel is set
	return &msg, nil
}

// fetchFromThread retrieves a message from a thread.
func (f *MessageFetcher) fetchFromThread(ctx context.Context, info *SlackURLInfo) (*slack.Message, error) {
	params := &slack.GetConversationRepliesParameters{
		ChannelID: info.ChannelID,
		Timestamp: info.ThreadTS,
		Limit:     100, // Fetch enough to find the target message
	}

	replies, _, _, err := f.client.GetConversationRepliesContext(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("GetConversationReplies failed: %w", err)
	}

	// Find the specific message in the thread
	for i := range replies {
		if replies[i].Timestamp == info.MessageTS {
			replies[i].Channel = info.ChannelID
			return &replies[i], nil
		}
	}

	return nil, fmt.Errorf("message not found in thread: %s", info.MessageTS)
}

// fetchThreadReplies retrieves all replies in a thread.
func (f *MessageFetcher) fetchThreadReplies(ctx context.Context, channelID, threadTS, excludeTS string) ([]slack.Message, error) {
	if err := f.allowRate(); err != nil {
		return nil, err
	}

	params := &slack.GetConversationRepliesParameters{
		ChannelID: channelID,
		Timestamp: threadTS,
		Limit:     50, // Limit thread replies
	}

	replies, _, _, err := f.client.GetConversationRepliesContext(ctx, params)
	if err != nil {
		return nil, err
	}

	// Filter out the original message and the target message
	result := make([]slack.Message, 0, len(replies))
	for _, reply := range replies {
		if reply.Timestamp == threadTS || reply.Timestamp == excludeTS {
			continue
		}
		reply.Channel = channelID
		result = append(result, reply)
	}

	return result, nil
}

// getPermalink retrieves the permalink for a message.
func (f *MessageFetcher) getPermalink(ctx context.Context, channelID, ts string) (string, error) {
	if err := f.allowRate(); err != nil {
		return "", err
	}

	return f.client.GetPermalinkContext(ctx, &slack.PermalinkParameters{
		Channel: channelID,
		Ts:      ts,
	})
}

// allowRate checks if the rate limiter allows the request.
func (f *MessageFetcher) allowRate() error {
	if f.rateLimiter == nil {
		return nil
	}
	if !f.rateLimiter.Allow(slackSearchRateLimitKey, slackSearchRateLimitKey) {
		return fmt.Errorf("slack API rate limit exceeded")
	}
	return nil
}

// FormatFetchedContext formats fetched messages for inclusion in a prompt.
func FormatFetchedContext(messages []EnrichedMessage) string {
	if len(messages) == 0 {
		return ""
	}

	var result string
	result += "Referenced Slack Messages:\n"

	for i, msg := range messages {
		orig := msg.OriginalMessage
		user := orig.User
		if orig.Username != "" {
			user = orig.Username
		}

		result += fmt.Sprintf("\n[%d] Channel: %s | User: %s | Time: %s\n",
			i+1,
			orig.Channel,
			user,
			formatSlackTimestamp(orig.Timestamp),
		)
		result += fmt.Sprintf("Message: %s\n", orig.Text)

		if msg.Permalink != "" {
			result += fmt.Sprintf("Link: %s\n", msg.Permalink)
		}

		if len(msg.ThreadMessages) > 0 {
			result += fmt.Sprintf("Thread Replies (%d):\n", len(msg.ThreadMessages))
			for _, reply := range msg.ThreadMessages {
				replyUser := reply.User
				if reply.Username != "" {
					replyUser = reply.Username
				}
				result += fmt.Sprintf("  - [%s] %s: %s\n",
					formatSlackTimestamp(reply.Timestamp),
					replyUser,
					reply.Text,
				)
			}
		}
	}

	return result
}
