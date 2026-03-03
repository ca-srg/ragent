package slacksearch

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/ca-srg/ragent/internal/slackbot"
	"github.com/slack-go/slack"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	circuitFailureThreshold  = 3
	circuitCooldownDuration  = 5 * time.Minute
	slackSearchRateLimitKey  = "slacksearch"
	minSearchResults         = 1
	maxSearchResults         = 100
	defaultSearchResultLimit = 20
)

// Searcher executes Slack search queries with rate limiting and retries.
type slackSearchClient interface {
	SearchMessagesContext(ctx context.Context, query string, params slack.SearchParameters) (*slack.SearchMessages, error)
}

type Searcher struct {
	client         slackSearchClient
	rateLimiter    *slackbot.RateLimiter
	logger         *log.Logger
	requestTimeout time.Duration

	mu               sync.Mutex
	consecutiveError int
	circuitOpenUntil time.Time
}

// SearchRequest describes the parameters used for Slack search.
type SearchRequest struct {
	Query      string
	TimeRange  *TimeRange
	MaxResults int
}

// SearchResponse contains the Slack search results and metadata.
type SearchResponse struct {
	Messages   []slack.Message
	TotalCount int
	HasMore    bool
	Query      string
}

// NewSearcher constructs a new Searcher instance.
// requestTimeout specifies the timeout for each Slack API call.
func NewSearcher(client *slack.Client, rateLimiter *slackbot.RateLimiter, requestTimeout time.Duration) *Searcher {
	logger := log.New(log.Default().Writer(), "slacksearch/searcher ", log.LstdFlags)
	return &Searcher{
		client:         client,
		rateLimiter:    rateLimiter,
		logger:         logger,
		requestTimeout: requestTimeout,
	}
}

// Search performs a single Slack search request without retries.
func (s *Searcher) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, span := slackSearchTracer.Start(ctx, "slacksearch.searcher.search")
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

	queryHash := telemetryFingerprint(query)
	span.SetAttributes(
		attribute.String("slack.query_hash", queryHash),
		attribute.Int("slack.max_results_requested", req.MaxResults),
	)

	if err := s.ensureCircuitAvailable(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "circuit_open")
		return nil, err
	}

	if s.rateLimiter != nil && !s.rateLimiter.Allow(slackSearchRateLimitKey, slackSearchRateLimitKey) {
		err := fmt.Errorf("slack search rate limit exceeded")
		s.logger.Printf("Slack search rate limit triggered hash=%s", queryHash)
		span.RecordError(err)
		span.SetStatus(codes.Error, "rate_limited")
		return nil, err
	}

	enrichedQuery := s.buildQuery(query, req.TimeRange)
	enrichedHash := telemetryFingerprint(enrichedQuery)
	span.SetAttributes(
		attribute.String("slack.enriched_query_hash", enrichedHash),
		attribute.Bool("slack.time_filter_present", req.TimeRange != nil),
	)

	params := slack.NewSearchParameters()
	params.Sort = "timestamp"
	params.SortDirection = "desc"
	params.Highlight = false
	params.Count = clamp(max(req.MaxResults, defaultSearchResultLimit), minSearchResults, maxSearchResults)

	span.SetAttributes(attribute.Int("slack.request_limit", params.Count))

	ctx, cancel := context.WithTimeout(ctx, s.requestTimeout)
	defer cancel()

	s.logger.Printf("Slack search executing hash=%s count=%d", enrichedHash, params.Count)
	result, err := s.client.SearchMessagesContext(ctx, enrichedQuery, params)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "search_failed")
		return nil, err
	}

	messages := make([]slack.Message, 0, len(result.Matches))
	for _, match := range result.Matches {
		messages = append(messages, convertSearchMessage(match))
	}

	paging := result.Paging
	hasMore := paging.Pages > paging.Page
	response := &SearchResponse{
		Messages:   messages,
		TotalCount: result.Total,
		HasMore:    hasMore,
		Query:      enrichedQuery,
	}

	span.SetAttributes(
		attribute.Int("slack.messages_returned", len(messages)),
		attribute.Int("slack.total_count", result.Total),
		attribute.Bool("slack.has_more", hasMore),
	)
	s.logger.Printf("Slack search completed hash=%s returned=%d total=%d has_more=%t", enrichedHash, len(messages), result.Total, hasMore)

	return response, nil
}

// SearchWithRetry executes Slack search with retry logic and circuit breaking.
func (s *Searcher) SearchWithRetry(ctx context.Context, req *SearchRequest, maxRetries int) (*SearchResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, span := slackSearchTracer.Start(ctx, "slacksearch.searcher.search_with_retry")
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

		retryAfter, retryable := classifySlackError(err)
		lastErr = err
		s.recordFailure()

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
		s.logger.Printf("Slack search retry hash=%s attempt=%d backoff=%s error=%v", queryHash, attempt+1, backoff, err)
		span.SetAttributes(attribute.String("slack.retry_backoff", backoff.String()))
		if err := sleepWithContext(ctx, backoff); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "backoff_interrupted")
			return nil, err
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("unknown Slack search error")
	}
	span.RecordError(lastErr)
	span.SetStatus(codes.Error, "search_failed")
	return nil, lastErr
}

func (s *Searcher) ensureCircuitAvailable() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.circuitOpenUntil.IsZero() {
		if remaining := time.Until(s.circuitOpenUntil); remaining > 0 {
			return fmt.Errorf("slack search circuit open, retry after %s", remaining.Round(time.Second))
		}
		s.circuitOpenUntil = time.Time{}
	}
	return nil
}

func (s *Searcher) recordSuccess() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consecutiveError = 0
}

func (s *Searcher) recordFailure() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.consecutiveError++
	if s.consecutiveError >= circuitFailureThreshold {
		s.circuitOpenUntil = time.Now().Add(circuitCooldownDuration)
		s.logger.Printf("Slack search circuit breaker tripped; cooldown until %s", s.circuitOpenUntil.UTC().Format(time.RFC3339))
		s.consecutiveError = 0
	}
}

func (s *Searcher) buildQuery(base string, timeRange *TimeRange) string {
	builder := strings.Builder{}
	builder.WriteString(strings.TrimSpace(base))

	if timeRange != nil {
		if timeRange.Start != nil {
			builder.WriteString(" after:")
			builder.WriteString(timeRange.Start.In(time.UTC).Format("2006-01-02"))
		}
		if timeRange.End != nil {
			builder.WriteString(" before:")
			builder.WriteString(timeRange.End.In(time.UTC).Format("2006-01-02"))
		}
	}

	return strings.TrimSpace(builder.String())
}

func classifySlackError(err error) (time.Duration, bool) {
	if err == nil {
		return 0, false
	}

	var rateErr *slack.RateLimitedError
	if errors.As(err, &rateErr) {
		return rateErr.RetryAfter, true
	}

	var statusErr slack.StatusCodeError
	if errors.As(err, &statusErr) {
		return 0, statusErr.Retryable()
	}

	return 0, false
}

func computeBackoffDuration(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}

	backoff := time.Duration(1<<attempt) * 500 * time.Millisecond
	maxBackoff := 10 * time.Second
	if backoff > maxBackoff {
		return maxBackoff
	}
	return backoff
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func convertSearchMessage(msg slack.SearchMessage) slack.Message {
	return slack.Message{
		Msg: slack.Msg{
			Type:        msg.Type,
			Channel:     msg.Channel.ID,
			User:        msg.User,
			Username:    msg.Username,
			Text:        msg.Text,
			Timestamp:   msg.Timestamp,
			Blocks:      msg.Blocks,
			Attachments: msg.Attachments,
			Permalink:   msg.Permalink,
		},
	}
}

func clamp(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func max(lhs, rhs int) int {
	if lhs > rhs {
		return lhs
	}
	return rhs
}
