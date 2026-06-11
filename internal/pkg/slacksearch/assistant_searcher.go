package slacksearch

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/slack-go/slack"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// assistantSearchClient narrows the slack.Client surface that
// AssistantSearcher depends on so it can be replaced in unit tests.
type assistantSearchClient interface {
	SearchAssistantContextContext(
		ctx context.Context,
		params slack.AssistantSearchContextParameters,
	) (*slack.AssistantSearchContextResponse, error)
}

const maxAssistantSearchResults = 20

// AssistantSearcher invokes Slack's `assistant.search.context` API on a Bot
// Token. The caller must surface a short-lived `action_token` obtained from
// an `app_mention` or `message` event (Real-time Search API). The bot can
// then search public channels it has not been invited to, as long as the
// `search:read.public` granular scope is granted.
type AssistantSearcher struct {
	client         assistantSearchClient
	rateLimiter    *RateLimiter
	logger         *log.Logger
	requestTimeout time.Duration

	mu               sync.Mutex
	consecutiveError int
	circuitOpenUntil time.Time
}

// NewAssistantSearcher constructs an AssistantSearcher backed by the given
// Slack client (typically the Bot Token client).
func NewAssistantSearcher(client *slack.Client, rateLimiter *RateLimiter, requestTimeout time.Duration) *AssistantSearcher {
	logger := log.New(log.Default().Writer(), "slacksearch/assistant ", log.LstdFlags)
	return &AssistantSearcher{
		client:         client,
		rateLimiter:    rateLimiter,
		logger:         logger,
		requestTimeout: requestTimeout,
	}
}

// Search performs a single assistant.search.context call. The request must
// include a non-empty ActionToken.
func (s *AssistantSearcher) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, span := slackSearchTracer.Start(ctx, "slacksearch.assistant.search")
	defer span.End()

	if req == nil {
		err := fmt.Errorf("search request cannot be nil")
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid_request")
		return nil, err
	}
	if s.client == nil {
		err := fmt.Errorf("slack client not configured")
		span.RecordError(err)
		span.SetStatus(codes.Error, "client_not_configured")
		return nil, err
	}

	query := strings.TrimSpace(req.Query)
	if query == "" {
		err := fmt.Errorf("search query cannot be empty")
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid_query")
		return nil, err
	}
	if strings.TrimSpace(req.ActionToken) == "" {
		err := fmt.Errorf("action_token is required for assistant.search.context")
		span.RecordError(err)
		span.SetStatus(codes.Error, "missing_action_token")
		return nil, err
	}

	queryHash := telemetryFingerprint(query)
	span.SetAttributes(
		attribute.String("slack.query_hash", queryHash),
		attribute.Int("slack.max_results_requested", req.MaxResults),
		attribute.Bool("slack.has_context_channel_id", strings.TrimSpace(req.ContextChannelID) != ""),
	)

	if err := s.ensureCircuitAvailable(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "circuit_open")
		return nil, err
	}

	if s.rateLimiter != nil && !s.rateLimiter.Allow(slackSearchRateLimitKey, slackSearchRateLimitKey) {
		err := fmt.Errorf("slack search rate limit exceeded")
		s.logger.Printf("Slack assistant search rate limit triggered hash=%s", queryHash)
		span.RecordError(err)
		span.SetStatus(codes.Error, "rate_limited")
		return nil, err
	}

	params := slack.AssistantSearchContextParameters{
		Query:                  query,
		ActionToken:            req.ActionToken,
		ChannelTypes:           []string{"public_channel"},
		ContentTypes:           []string{"messages"},
		Limit:                  clamp(max(req.MaxResults, defaultSearchResultLimit), minSearchResults, maxAssistantSearchResults),
		IncludeContextMessages: true,
		Sort:                   "timestamp",
		SortDir:                "desc",
	}
	if strings.TrimSpace(req.ContextChannelID) != "" {
		params.ContextChannelID = req.ContextChannelID
	}
	if req.TimeRange != nil {
		if req.TimeRange.Start != nil {
			params.After = req.TimeRange.Start.Unix()
		}
		if req.TimeRange.End != nil {
			params.Before = req.TimeRange.End.Unix()
		}
	}

	span.SetAttributes(
		attribute.Int("slack.request_limit", params.Limit),
		attribute.Bool("slack.time_filter_present", req.TimeRange != nil),
	)

	ctx, cancel := context.WithTimeout(ctx, s.requestTimeout)
	defer cancel()

	s.logger.Printf("Slack assistant search executing hash=%s limit=%d", queryHash, params.Limit)
	resp, err := s.client.SearchAssistantContextContext(ctx, params)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "search_failed")
		return nil, err
	}
	if resp == nil {
		err := fmt.Errorf("assistant.search.context returned nil response")
		span.RecordError(err)
		span.SetStatus(codes.Error, "search_failed")
		return nil, err
	}
	if !resp.Ok {
		slackErr := strings.TrimSpace(resp.Error)
		if slackErr == "" {
			slackErr = "unknown_error"
		}
		err := fmt.Errorf("assistant.search.context: %s", slackErr)
		span.RecordError(err)
		span.SetStatus(codes.Error, "search_not_ok")
		return nil, err
	}

	messages := make([]slack.Message, 0, len(resp.Results.Messages))
	enrichedMessages := make([]EnrichedMessage, 0, len(resp.Results.Messages))
	for _, match := range resp.Results.Messages {
		messages = append(messages, convertAssistantSearchMessage(match))
		enrichedMessages = append(enrichedMessages, convertAssistantSearchEnrichedMessage(match))
	}

	hasMore := strings.TrimSpace(resp.ResponseMetadata.NextCursor) != ""
	response := &SearchResponse{
		Messages:         messages,
		EnrichedMessages: enrichedMessages,
		TotalCount:       len(messages),
		HasMore:          hasMore,
		Query:            query,
	}

	span.SetAttributes(
		attribute.Int("slack.messages_returned", len(messages)),
		attribute.Int("slack.total_count", len(messages)),
		attribute.Bool("slack.has_more", hasMore),
	)
	s.logger.Printf("Slack assistant search completed hash=%s returned=%d has_more=%t", queryHash, len(messages), hasMore)

	return response, nil
}

// SearchWithRetry mirrors Searcher.SearchWithRetry for the assistant path
// with retry, circuit breaking, and fatal-error fast-fail handling.
func (s *AssistantSearcher) SearchWithRetry(ctx context.Context, req *SearchRequest, maxRetries int) (*SearchResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, span := slackSearchTracer.Start(ctx, "slacksearch.assistant.search_with_retry")
	defer span.End()

	queryHash := telemetryFingerprint(req.Query)
	span.SetAttributes(
		attribute.String("slack.query_hash", queryHash),
		attribute.Int("slack.max_retries", maxRetries),
	)

	if err := s.ensureCircuitAvailable(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "circuit_open")
		return nil, err
	}

	attempts := maxRetries + 1
	var lastErr error

	for attempt := 0; attempt < attempts; attempt++ {
		span.AddEvent("search_attempt", trace.WithAttributes(attribute.Int("slack.attempt_index", attempt+1)))
		resp, err := s.Search(ctx, req)
		if err == nil {
			s.recordSuccess()
			span.SetAttributes(attribute.Int("slack.attempts_used", attempt+1))
			return resp, nil
		}

		lastErr = err
		if isAssistantActionTokenError(err) {
			span.RecordError(err)
			span.SetStatus(codes.Error, "non_retryable_action_token_error")
			return nil, lastErr
		}

		s.recordFailure()

		if isAssistantFatalError(err) {
			span.RecordError(err)
			span.SetStatus(codes.Error, "non_retryable_error")
			return nil, lastErr
		}

		retryAfter, retryable := classifySlackError(err)
		if !retryable || attempt == maxRetries {
			span.RecordError(err)
			if retryable {
				span.SetStatus(codes.Error, "max_retries_exhausted")
			} else {
				span.SetStatus(codes.Error, "non_retryable_error")
			}
			return nil, lastErr
		}

		backoff := computeBackoffDuration(attempt, retryAfter)
		s.logger.Printf("Slack assistant search retry hash=%s attempt=%d backoff=%s error=%v",
			queryHash, attempt+1, backoff, err)
		span.SetAttributes(attribute.String("slack.retry_backoff", backoff.String()))
		if err := sleepWithContext(ctx, backoff); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "backoff_interrupted")
			return nil, err
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("unknown Slack assistant search error")
	}
	span.RecordError(lastErr)
	span.SetStatus(codes.Error, "search_failed")
	return nil, lastErr
}

func (s *AssistantSearcher) ensureCircuitAvailable() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.circuitOpenUntil.IsZero() {
		if remaining := time.Until(s.circuitOpenUntil); remaining > 0 {
			return fmt.Errorf("slack assistant search circuit open, retry after %s", remaining.Round(time.Second))
		}
		s.circuitOpenUntil = time.Time{}
	}
	return nil
}

func (s *AssistantSearcher) recordSuccess() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consecutiveError = 0
}

func (s *AssistantSearcher) recordFailure() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.consecutiveError++
	if s.consecutiveError >= circuitFailureThreshold {
		s.circuitOpenUntil = time.Now().Add(circuitCooldownDuration)
		s.logger.Printf("Slack assistant search circuit breaker tripped; cooldown until %s",
			s.circuitOpenUntil.UTC().Format(time.RFC3339))
		s.consecutiveError = 0
	}
}

func convertAssistantSearchMessage(m slack.AssistantSearchContextMessage) slack.Message {
	msg := slack.Message{
		Msg: slack.Msg{
			Channel:   m.ChannelID,
			User:      m.AuthorUserID,
			Username:  m.AuthorName,
			Text:      m.Content,
			Timestamp: m.MessageTS,
			Permalink: m.Permalink,
			Blocks:    m.Blocks,
		},
	}
	if m.IsAuthorBot {
		msg.SubType = "bot_message"
	}
	return msg
}

func convertAssistantSearchEnrichedMessage(m slack.AssistantSearchContextMessage) EnrichedMessage {
	msg := convertAssistantSearchMessage(m)
	enriched := EnrichedMessage{
		OriginalMessage: msg,
		Permalink:       msg.Permalink,
	}
	if m.ContextMessages != nil {
		enriched.PreviousMessages = convertAssistantSearchContextMessages(m.ContextMessages.Before)
		enriched.NextMessages = convertAssistantSearchContextMessages(m.ContextMessages.After)
	}
	return enriched
}

func convertAssistantSearchContextMessages(messages []slack.AssistantSearchContextMessage) []slack.Message {
	if len(messages) == 0 {
		return nil
	}
	converted := make([]slack.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.IsAuthorBot {
			continue
		}
		converted = append(converted, convertAssistantSearchMessage(msg))
	}
	return converted
}

func isAssistantActionTokenError(err error) bool {
	return assistantErrorContains(err, "invalid_action_token", "token_expired")
}

// isAssistantFatalError reports whether the Slack error code (if any) is one
// of the documented fatal conditions for assistant.search.context that
// should never be retried.
func isAssistantFatalError(err error) bool {
	return assistantErrorContains(err,
		"invalid_action_token",
		"missing_scope",
		"not_allowed_token_type",
		"invalid_auth",
		"token_expired",
		"token_revoked",
		"access_denied",
		"feature_not_enabled",
		"assistant_search_context_disabled",
		"context_channel_not_found",
	)
}

func assistantErrorContains(err error, codes ...string) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, code := range codes {
		if strings.Contains(msg, code) {
			return true
		}
	}
	return false
}
