package slacksearch

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	appconfig "github.com/ca-srg/ragent/internal/pkg/config"
	"github.com/ca-srg/ragent/internal/pkg/embedding/bedrock"
	"github.com/slack-go/slack"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type slackQueryGenerator interface {
	GenerateQueries(ctx context.Context, req *QueryGenerationRequest) (*QueryGenerationResponse, error)
	GenerateAlternativeQueries(ctx context.Context, req *QueryGenerationRequest) (*QueryGenerationResponse, error)
}

type slackSearcher interface {
	SearchWithRetry(ctx context.Context, req *SearchRequest, maxRetries int) (*SearchResponse, error)
}

type slackContextRetriever interface {
	RetrieveContext(ctx context.Context, req *ContextRequest) (*ContextResponse, error)
}

type slackSufficiencyChecker interface {
	Check(ctx context.Context, req *SufficiencyRequest) (*SufficiencyResponse, error)
}

// SlackSearchService orchestrates the Slack search pipeline.
//
// It can dispatch a search to either of two backends, chosen per-call with
// the user-token backend taking priority when it is configured:
//
//   - userSearcher       — legacy `search.messages` over a User Token (xoxp);
//     preferred whenever SLACK_USER_TOKEN is configured, including Slack bot
//     events that also carry an `action_token`.
//   - assistantSearcher  — Real-time Search API (`assistant.search.context`)
//     over the Bot Token (xoxb) plus a short-lived `action_token` obtained
//     from an `app_mention` / `message` event. Used only when the user-token
//     backend is unavailable.
type SlackSearchService struct {
	botClient          *slack.Client
	userClient         *slack.Client // nil unless SLACK_USER_TOKEN is set
	queryGenerator     slackQueryGenerator
	userSearcher       slackSearcher         // search.messages; nil without user token
	assistantSearcher  slackSearcher         // assistant.search.context; nil if bot client missing
	contextRetriever   slackContextRetriever // requires user token
	sufficiencyChecker slackSufficiencyChecker
	messageFetcher     *MessageFetcher // URL fetch; user token preferred, bot token fallback
	config             *SlackSearchConfig
	logger             *log.Logger
	progressHandler    func(iteration int, maxIterations int)
}

// NewSlackSearchService constructs a new SlackSearchService instance.
func NewSlackSearchService(
	baseConfig *appconfig.Config,
	botClient *slack.Client,
	bedrockClient *bedrock.BedrockClient,
	logger *log.Logger,
) (*SlackSearchService, error) {
	if baseConfig == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}
	if botClient == nil {
		return nil, fmt.Errorf("slack bot client cannot be nil")
	}
	if bedrockClient == nil {
		return nil, fmt.Errorf("bedrock client cannot be nil")
	}
	if logger == nil {
		logger = log.Default()
	}

	cfg := &SlackSearchConfig{
		Enabled:              baseConfig.SlackSearchEnabled,
		MaxResults:           baseConfig.SlackSearchMaxResults,
		MaxRetries:           baseConfig.SlackSearchMaxRetries,
		ContextWindowMinutes: baseConfig.SlackSearchContextWindowMinutes,
		MaxIterations:        baseConfig.SlackSearchMaxIterations,
		MaxContextMessages:   baseConfig.SlackSearchMaxContextMessages,
		TimeoutSeconds:       baseConfig.SlackSearchTimeoutSeconds,
		LLMTimeoutSeconds:    baseConfig.SlackSearchLLMTimeoutSeconds,
	}

	if cfg.Enabled {
		cfg.BotToken = os.Getenv("SLACK_BOT_TOKEN")
		if cfg.BotToken == "" {
			return nil, fmt.Errorf("SLACK_BOT_TOKEN must be set when Slack search is enabled")
		}
	}

	cfg.UserToken = baseConfig.SlackUserToken
	if strings.TrimSpace(cfg.UserToken) == "" {
		cfg.UserToken = os.Getenv("SLACK_USER_TOKEN")
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid Slack search configuration: %w", err)
	}

	// Build the user-token client (search.messages backend) when xoxp is set.
	// The bot token is always available (validated above); the assistant
	// searcher uses it together with a per-request action_token.
	var userClient *slack.Client
	if strings.TrimSpace(cfg.UserToken) != "" {
		userClient = slack.New(cfg.UserToken)
	} else if cfg.Enabled {
		logger.Printf("Slack search: SLACK_USER_TOKEN not set; user-token backend (search.messages) is disabled, " +
			"slack-bot mention path can still use assistant.search.context with action_token")
	}

	searchLimiter := NewRateLimiter(20, 20, 20)
	contextLimiter := NewRateLimiter(60, 120, 200)

	llmTimeout := time.Duration(cfg.LLMTimeoutSeconds) * time.Second
	queryGenerator := NewQueryGenerator(bedrockClient, llmTimeout)

	requestTimeout := time.Duration(cfg.TimeoutSeconds) * time.Second

	var (
		userSearcher     slackSearcher
		contextRetriever slackContextRetriever
		messageFetcher   *MessageFetcher
	)

	if userClient != nil {
		userSearcher = NewSearcher(userClient, searchLimiter, requestTimeout)
		var err error
		contextRetriever, err = NewContextRetriever(userClient, contextLimiter, bedrockClient, cfg, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create context retriever: %w", err)
		}
		messageFetcher = NewMessageFetcher(userClient, contextLimiter, cfg, logger)
	} else {
		// URL fetch over the bot token still works for channels the bot is
		// invited to (channels:history scope). It is the only viable path
		// when SLACK_USER_TOKEN is not configured.
		messageFetcher = NewMessageFetcher(botClient, contextLimiter, cfg, logger)
	}

	// assistant.search.context always uses the bot token + action_token.
	assistantSearcher := NewAssistantSearcher(botClient, searchLimiter, requestTimeout)

	sufficiencyChecker := NewSufficiencyChecker(bedrockClient, logger, llmTimeout)

	service := &SlackSearchService{
		botClient:          botClient,
		userClient:         userClient,
		queryGenerator:     queryGenerator,
		userSearcher:       userSearcher,
		assistantSearcher:  assistantSearcher,
		contextRetriever:   contextRetriever,
		sufficiencyChecker: sufficiencyChecker,
		messageFetcher:     messageFetcher,
		config:             cfg,
		logger:             logger,
		progressHandler:    nil,
	}

	return service, nil
}

// Initialize prepares the Slack search service.
func (s *SlackSearchService) Initialize(ctx context.Context) error {
	s.logger.Printf("Slack search service initialized (enabled=%t, max_results=%d)", s.config.Enabled, s.config.MaxResults)
	return nil
}

// SetProgressHandler registers a callback invoked at the beginning of each iteration.
func (s *SlackSearchService) SetProgressHandler(handler func(iteration int, maxIterations int)) {
	s.progressHandler = handler
}

// SlackClient exposes the underlying Slack client (useful for bots sharing the service).
func (s *SlackSearchService) SlackClient() *slack.Client {
	return s.botClient
}

// HealthCheck verifies service dependencies and configuration state.
func (s *SlackSearchService) HealthCheck(ctx context.Context) error {
	if s.botClient == nil {
		return fmt.Errorf("slack bot client is not configured")
	}

	if s.config == nil {
		return fmt.Errorf("slack search configuration is not initialized")
	}

	if err := s.config.Validate(); err != nil {
		return fmt.Errorf("slack search configuration validation failed: %w", err)
	}

	if !s.config.Enabled {
		s.logger.Printf("Slack search disabled; skipping Slack API health check")
		return nil
	}

	if strings.TrimSpace(s.config.BotToken) == "" {
		return fmt.Errorf("SLACK_BOT_TOKEN must be set when Slack search is enabled")
	}

	if s.userSearcher == nil && s.assistantSearcher == nil {
		return fmt.Errorf("slack realtime search client is not configured (no user token and no bot token)")
	}

	s.logger.Printf("Slack search health check passed")
	return nil
}

// SearchBackend identifies which Slack search API the service ended up using.
type SearchBackend string

const (
	// SearchBackendUser uses search.messages over a user token.
	SearchBackendUser SearchBackend = "user_search_messages"
	// SearchBackendAssistant uses assistant.search.context over a bot token
	// plus an action_token surfaced by a Slack event.
	SearchBackendAssistant SearchBackend = "assistant_search_context"
)

// selectSearcher picks the active searcher for this call.
//
// Priority:
//  1. userSearcher available → legacy search.messages path.
//  2. opts.ActionToken set + assistantSearcher available → assistant path.
//  3. otherwise → caller error.
func (s *SlackSearchService) selectSearcher(opts SearchOptions) (slackSearcher, SearchBackend, error) {
	if s.userSearcher != nil {
		return s.userSearcher, SearchBackendUser, nil
	}
	if strings.TrimSpace(opts.ActionToken) != "" && s.assistantSearcher != nil {
		return s.assistantSearcher, SearchBackendAssistant, nil
	}
	return nil, "", fmt.Errorf(
		"slack search requires either SLACK_USER_TOKEN (legacy search.messages) " +
			"or a Slack event action_token (assistant.search.context)")
}

// Search executes the Slack search pipeline.
//
// SLACK_USER_TOKEN decides the backend first: when the user-token backend is
// configured, the service uses `search.messages` even if opts.ActionToken is
// present. The assistant backend is used only when the user-token backend is
// unavailable and opts.ActionToken is non-empty.
func (s *SlackSearchService) Search(
	ctx context.Context,
	userQuery string,
	channels []string,
	opts SearchOptions,
) (*SlackSearchResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	ctx, span := slackSearchTracer.Start(ctx, "slacksearch.service.search")
	defer span.End()

	queryHash := telemetryFingerprint(userQuery)
	span.SetAttributes(
		attribute.String("slack.query_hash", queryHash),
		attribute.Int("slack.channel_count", len(channels)),
		attribute.Bool("slack.has_action_token", strings.TrimSpace(opts.ActionToken) != ""),
	)
	s.logger.Printf("Slack search started hash=%s channels=%d enabled=%t has_action_token=%t",
		queryHash, len(channels), s.config.Enabled, strings.TrimSpace(opts.ActionToken) != "")

	if !s.config.Enabled {
		err := fmt.Errorf("slack search is disabled in configuration")
		span.RecordError(err)
		span.SetStatus(codes.Error, "slack_search_disabled")
		return nil, err
	}
	if s.queryGenerator == nil || s.sufficiencyChecker == nil {
		err := fmt.Errorf("slack search service not fully initialized")
		span.RecordError(err)
		span.SetStatus(codes.Error, "service_not_initialized")
		return nil, err
	}
	if userQuery == "" {
		err := fmt.Errorf("user query cannot be empty")
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid_query")
		return nil, err
	}

	activeSearcher, backend, err := s.selectSearcher(opts)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "no_searcher_available")
		return nil, err
	}
	span.SetAttributes(attribute.String("slack.search_backend", string(backend)))
	s.logger.Printf("Slack search backend=%s hash=%s", backend, queryHash)

	startTime := time.Now()

	// URL direct fetch: detect Slack URLs and fetch messages directly
	var urlFetchedMessages []EnrichedMessage
	if HasSlackURL(userQuery) && s.messageFetcher != nil {
		fetchResp, err := s.FetchMessagesFromQuery(ctx, userQuery)
		if err != nil {
			s.logger.Printf("Slack URL fetch warning: %v", err)
			span.AddEvent("url_fetch_warning", trace.WithAttributes(attribute.String("error", err.Error())))
		} else if fetchResp != nil && len(fetchResp.EnrichedMessages) > 0 {
			urlFetchedMessages = fetchResp.EnrichedMessages
			s.logger.Printf("Fetched %d message(s) from Slack URL(s)", len(urlFetchedMessages))
			span.SetAttributes(attribute.Int("slack.url_fetched_count", len(urlFetchedMessages)))
		}
	}

	// If query contains only URLs (no other text), return URL-fetched results immediately
	cleanedQuery := ExtractQueryWithoutURLs(userQuery)
	if len(urlFetchedMessages) > 0 && strings.TrimSpace(cleanedQuery) == "" {
		s.logger.Printf("Slack search completed (URL-only query) hash=%s fetched=%d duration=%s",
			queryHash, len(urlFetchedMessages), time.Since(startTime).String())
		span.SetAttributes(
			attribute.Bool("slack.url_only_query", true),
			attribute.Int("slack.url_fetched_count", len(urlFetchedMessages)),
			attribute.Float64("slack.execution_ms", float64(time.Since(startTime).Milliseconds())),
		)
		return s.buildURLOnlyResult(startTime, urlFetchedMessages), nil
	}

	maxIterations := s.config.MaxIterations
	if maxIterations <= 0 {
		maxIterations = 1
	}

	var (
		executedQueries       []string
		enrichedMessages      []EnrichedMessage
		totalMatches          int
		suffResult            *SufficiencyResponse
		previousQueries       []string
		previousResults       int
		iterationsDone        int
		previousEnrichedCount int
		stagnationCount       int
	)

	for iteration := 0; iteration < maxIterations; iteration++ {
		iterationCtx, iterSpan := slackSearchTracer.Start(ctx, "slacksearch.service.iteration", trace.WithAttributes(
			attribute.Int("slack.iteration_index", iteration+1),
			attribute.Int("slack.previous_queries_count", len(previousQueries)),
			attribute.Int("slack.previous_results", previousResults),
		))

		if s.progressHandler != nil {
			s.progressHandler(iteration+1, maxIterations)
		}
		iterationsDone = iteration + 1

		s.logger.Printf("Slack search iteration=%d hash=%s previous_queries=%d previous_results=%d", iterationsDone, queryHash, len(previousQueries), previousResults)
		select {
		case <-iterationCtx.Done():
			err := iterationCtx.Err()
			iterSpan.RecordError(err)
			iterSpan.SetStatus(codes.Error, "context_cancelled")
			iterSpan.End()
			span.RecordError(err)
			span.SetStatus(codes.Error, "context_cancelled")
			return nil, err
		default:
		}

		var (
			searchMessages         []slack.Message
			searchEnrichedMessages []EnrichedMessage
			matches                int
			err                    error
		)

		searchMessages, searchEnrichedMessages, _, executedQueries, previousQueries, _, err = s.runSearchIteration(
			iterationCtx,
			activeSearcher,
			opts,
			userQuery,
			channels,
			previousQueries,
			previousResults,
			executedQueries,
			iteration,
		)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				err = fmt.Errorf("%w: LLMリクエストがタイムアウトしました (llmRequestTimeout=%ds)", err, s.config.LLMTimeoutSeconds)
			}
			iterSpan.RecordError(err)
			iterSpan.SetStatus(codes.Error, "iteration_failed")
			iterSpan.End()
			span.RecordError(err)
			span.SetStatus(codes.Error, "iteration_failed")
			return nil, err
		}

		searchMessages, searchEnrichedMessages, botFiltered := filterBotSearchResults(searchMessages, searchEnrichedMessages)
		if botFiltered > 0 {
			iterSpan.SetAttributes(attribute.Int("slack.filtered_bot_messages", botFiltered))
			s.logger.Printf("Slack search iteration=%d hash=%s filtered_bot_messages=%d", iterationsDone, queryHash, botFiltered)
		}

		matches = len(searchMessages)
		previousResults = matches
		totalMatches = matches
		iterSpan.SetAttributes(
			attribute.Int("slack.iteration_matches", matches),
			attribute.Int("slack.executed_queries_total", len(executedQueries)),
		)
		s.logger.Printf("Slack search iteration=%d hash=%s matches=%d executed_queries_total=%d", iterationsDone, queryHash, matches, len(executedQueries))

		if len(searchMessages) == 0 {
			iterSpan.AddEvent("no_messages_returned")

			// If we have URL-fetched messages, merge them and return success
			if len(urlFetchedMessages) > 0 {
				mergedMessages := mergeEnrichedMessages(urlFetchedMessages, enrichedMessages)
				s.logger.Printf("Search returned 0 matches, but URL-fetched %d message(s) available", len(urlFetchedMessages))
				result := s.buildResult(startTime, executedQueries, iteration+1, len(urlFetchedMessages), mergedMessages, &SufficiencyResponse{
					IsSufficient: true,
					MissingInfo:  []string{},
					Reasoning:    "URL-fetched messages available",
					Confidence:   1.0,
				})
				iterSpan.End()
				span.SetAttributes(
					attribute.Int("slack.iterations_completed", iterationsDone),
					attribute.Int("slack.total_matches", len(urlFetchedMessages)),
					attribute.Int("slack.enriched_messages", len(result.EnrichedMessages)),
					attribute.Bool("slack.sufficient", result.IsSufficient),
					attribute.Float64("slack.execution_ms", float64(time.Since(startTime).Milliseconds())),
				)
				s.logger.Printf("Slack search completed hash=%s iterations=%d matches=%d enriched=%d sufficient=%t duration=%s (URL-fetched)",
					queryHash, iterationsDone, len(urlFetchedMessages), len(result.EnrichedMessages), result.IsSufficient, time.Since(startTime).String())
				return result, nil
			}

			result := s.buildResult(startTime, executedQueries, iteration+1, totalMatches, enrichedMessages, &SufficiencyResponse{
				IsSufficient: false,
				MissingInfo:  []string{"No Slack conversations matched the query"},
				Reasoning:    "Slack search returned zero messages",
				Confidence:   0,
			})
			iterSpan.End()
			span.SetAttributes(
				attribute.Int("slack.iterations_completed", iterationsDone),
				attribute.Int("slack.total_matches", totalMatches),
				attribute.Int("slack.enriched_messages", len(result.EnrichedMessages)),
				attribute.Bool("slack.sufficient", result.IsSufficient),
				attribute.Float64("slack.execution_ms", float64(time.Since(startTime).Milliseconds())),
			)
			s.logger.Printf("Slack search completed hash=%s iterations=%d matches=%d enriched=%d sufficient=%t duration=%s (no matches)",
				queryHash, iterationsDone, totalMatches, len(result.EnrichedMessages), result.IsSufficient, time.Since(startTime).String())
			return result, nil
		}

		// assistant.search.context already returns permalinks and inline
		// context_messages, so we skip the ContextRetriever — its
		// underlying APIs (conversations.history/replies) require the bot to
		// be a channel member, which defeats the whole point of using the
		// assistant path.
		var contextResp *ContextResponse
		switch {
		case backend == SearchBackendAssistant:
			if len(searchEnrichedMessages) == 0 {
				searchEnrichedMessages = s.fallbackEnriched(searchMessages)
			}
			contextResp = &ContextResponse{
				EnrichedMessages: searchEnrichedMessages,
				TotalRetrieved:   len(searchEnrichedMessages),
			}
		case s.contextRetriever == nil:
			contextResp = &ContextResponse{
				EnrichedMessages: s.fallbackEnriched(searchMessages),
				TotalRetrieved:   len(searchMessages),
			}
		default:
			var retrieveErr error
			contextResp, retrieveErr = s.contextRetriever.RetrieveContext(iterationCtx, &ContextRequest{
				Messages:  searchMessages,
				UserQuery: userQuery,
			})
			if retrieveErr != nil {
				s.logger.Printf("Slack search context retrieval failed: %v", retrieveErr)
				iterSpan.AddEvent("context_retrieval_failed", trace.WithAttributes(attribute.String("error", retrieveErr.Error())))
				contextResp = &ContextResponse{
					EnrichedMessages: s.fallbackEnriched(searchMessages),
					TotalRetrieved:   len(searchMessages),
				}
			} else {
				s.logger.Printf("Slack search context retrieved hash=%s retrieved=%d", queryHash, contextResp.TotalRetrieved)
			}
		}

		enrichedMessages = contextResp.EnrichedMessages
		iterSpan.SetAttributes(
			attribute.Int("slack.enriched_message_count", len(enrichedMessages)),
			attribute.Int("slack.context_total_retrieved", contextResp.TotalRetrieved),
		)

		// Stagnation detection: if no new enriched messages found, increment stagnation counter
		currentEnrichedCount := len(enrichedMessages)
		if iteration > 0 && currentEnrichedCount <= previousEnrichedCount {
			stagnationCount++
			s.logger.Printf("Slack search stagnation detected hash=%s iteration=%d current=%d previous=%d stagnation_count=%d",
				queryHash, iterationsDone, currentEnrichedCount, previousEnrichedCount, stagnationCount)
		} else {
			stagnationCount = 0
		}
		previousEnrichedCount = currentEnrichedCount

		sufficiencyReq := &SufficiencyRequest{
			UserQuery:     userQuery,
			Messages:      enrichedMessages,
			Iteration:     iteration,
			MaxIterations: maxIterations,
		}

		suffResult, err = s.sufficiencyChecker.Check(iterationCtx, sufficiencyReq)
		if err != nil {
			iterSpan.RecordError(err)
			iterSpan.SetStatus(codes.Error, "sufficiency_failed")
			iterSpan.End()
			span.RecordError(err)
			span.SetStatus(codes.Error, "sufficiency_failed")
			return nil, fmt.Errorf("sufficiency check failed: %w", err)
		}

		iterSpan.SetAttributes(
			attribute.Bool("slack.is_sufficient", suffResult.IsSufficient),
			attribute.Float64("slack.sufficiency_confidence", suffResult.Confidence),
			attribute.Int("slack.missing_info_count", len(suffResult.MissingInfo)),
		)
		s.logger.Printf("Slack sufficiency evaluated hash=%s iteration=%d sufficient=%t confidence=%.2f missing=%d",
			queryHash, iterationsDone, suffResult.IsSufficient, suffResult.Confidence, len(suffResult.MissingInfo))

		// Early termination conditions:
		// 1. Sufficient information found
		// 2. Max iterations reached
		// 3. Stagnation detected (no progress for 2 consecutive iterations)
		shouldTerminate := suffResult.IsSufficient || iteration+1 >= maxIterations || stagnationCount >= 2
		if shouldTerminate {
			reason := "max_iterations"
			if suffResult.IsSufficient {
				reason = "sufficient"
			} else if stagnationCount >= 2 {
				reason = "stagnation"
				s.logger.Printf("Slack search early termination due to stagnation hash=%s iteration=%d stagnation_count=%d",
					queryHash, iterationsDone, stagnationCount)
			}
			iterSpan.AddEvent("search_terminated", trace.WithAttributes(
				attribute.Bool("slack.is_sufficient", suffResult.IsSufficient),
				attribute.Int("slack.iteration", iteration+1),
				attribute.String("slack.termination_reason", reason),
				attribute.Int("slack.stagnation_count", stagnationCount),
			))
			iterSpan.End()
			break
		}

		iterSpan.End()
	}

	// Merge URL-fetched messages with search results (URL messages first, deduplicated)
	if len(urlFetchedMessages) > 0 {
		enrichedMessages = mergeEnrichedMessages(urlFetchedMessages, enrichedMessages)
		s.logger.Printf("Merged URL-fetched messages with search results: total=%d", len(enrichedMessages))
	}

	result := s.buildResult(startTime, executedQueries, iterationsDone, totalMatches, enrichedMessages, suffResult)
	span.SetAttributes(
		attribute.Int("slack.iterations_completed", iterationsDone),
		attribute.Int("slack.total_matches", totalMatches),
		attribute.Int("slack.enriched_messages", len(result.EnrichedMessages)),
		attribute.Bool("slack.sufficient", result.IsSufficient),
		attribute.Float64("slack.execution_ms", float64(time.Since(startTime).Milliseconds())),
	)
	s.logger.Printf("Slack search completed hash=%s iterations=%d matches=%d enriched=%d sufficient=%t duration=%s",
		queryHash, iterationsDone, totalMatches, len(result.EnrichedMessages), result.IsSufficient, time.Since(startTime).String())
	return result, nil
}

func (s *SlackSearchService) runSearchIteration(
	ctx context.Context,
	activeSearcher slackSearcher,
	opts SearchOptions,
	userQuery string,
	channels []string,
	previousQueries []string,
	previousResults int,
	executedQueries []string,
	iteration int,
) ([]slack.Message, []EnrichedMessage, int, []string, []string, int, error) {
	var (
		searchMessages         []slack.Message
		searchEnrichedMessages []EnrichedMessage
		totalMatches           int
		genResp                *QueryGenerationResponse
		err                    error
	)
	queryHash := telemetryFingerprint(userQuery)

	genReq := &QueryGenerationRequest{
		UserQuery:       userQuery,
		PreviousQueries: previousQueries,
		PreviousResults: previousResults,
	}

	// First iteration uses initial query generation, subsequent iterations use alternative queries
	if iteration == 0 {
		genResp, err = s.queryGenerator.GenerateQueries(ctx, genReq)
	} else {
		genResp, err = s.queryGenerator.GenerateAlternativeQueries(ctx, genReq)
	}
	if err != nil {
		return nil, nil, 0, executedQueries, previousQueries, previousResults, fmt.Errorf("failed to generate Slack queries: %w", err)
	}

	if len(genResp.Queries) == 0 {
		s.logger.Printf("Slack search iteration=%d hash=%s generated_zero_queries", iteration+1, queryHash)
		return searchMessages, searchEnrichedMessages, totalMatches, executedQueries, previousQueries, previousResults, nil
	}

	s.logger.Printf("Slack search iteration=%d hash=%s generated_queries=%d time_filter=%t",
		iteration+1, queryHash, len(genResp.Queries), genResp.TimeFilter != nil)

	searchMessages, searchEnrichedMessages, totalMatches, executedQueries = s.executeSlackSearch(ctx, activeSearcher, opts, genResp, executedQueries)
	previousQueries = append(previousQueries, genResp.Queries...)
	previousResults = totalMatches

	return searchMessages, searchEnrichedMessages, totalMatches, executedQueries, previousQueries, previousResults, nil
}

func (s *SlackSearchService) executeSlackSearch(
	ctx context.Context,
	activeSearcher slackSearcher,
	opts SearchOptions,
	genResp *QueryGenerationResponse,
	executedQueries []string,
) ([]slack.Message, []EnrichedMessage, int, []string) {
	messageMap := make(map[string]slack.Message)
	messages := make([]slack.Message, 0)
	enrichedMessages := make([]EnrichedMessage, 0)
	totalMatches := 0

	limit := s.config.MaxResults
	if limit <= 0 || limit > s.config.MaxContextMessages {
		limit = s.config.MaxContextMessages
	}
	if limit <= 0 {
		limit = 1
	}

	for _, query := range genResp.Queries {
		select {
		case <-ctx.Done():
			return messages, enrichedMessages, totalMatches, executedQueries
		default:
		}

		request := &SearchRequest{
			Query:            query,
			TimeRange:        genResp.TimeFilter,
			MaxResults:       s.config.MaxResults,
			ActionToken:      opts.ActionToken,
			ContextChannelID: opts.ChannelID,
		}

		response, err := activeSearcher.SearchWithRetry(ctx, request, s.config.MaxRetries)
		executedQueries = append(executedQueries, query)
		if err != nil {
			s.logger.Printf("Slack search query failed hash=%s error=%v", telemetryFingerprint(query), err)
			continue
		}

		totalMatches += response.TotalCount
		s.logger.Printf("Slack search query succeeded hash=%s matches=%d total_matches=%d", telemetryFingerprint(query), len(response.Messages), totalMatches)
		responseMessages := response.Messages
		if len(responseMessages) == 0 && len(response.EnrichedMessages) > 0 {
			responseMessages = originalMessagesFromEnriched(response.EnrichedMessages)
		}
		enrichedByKey := enrichedMessagesByKey(response.EnrichedMessages)
		for _, msg := range responseMessages {
			key := fmt.Sprintf("%s:%s", msg.Channel, msg.Timestamp)
			if _, exists := messageMap[key]; exists {
				continue
			}
			messageMap[key] = msg
			messages = append(messages, msg)
			if enrichedMsg, ok := enrichedByKey[key]; ok {
				enrichedMessages = append(enrichedMessages, enrichedMsg)
			} else if len(response.EnrichedMessages) > 0 {
				enrichedMessages = append(enrichedMessages, EnrichedMessage{OriginalMessage: msg, Permalink: msg.Permalink})
			}
			if len(messages) >= limit {
				break
			}
		}
	}

	return messages, enrichedMessages, totalMatches, executedQueries
}

func (s *SlackSearchService) fallbackEnriched(messages []slack.Message) []EnrichedMessage {
	enriched := make([]EnrichedMessage, 0, len(messages))
	for _, msg := range messages {
		enriched = append(enriched, EnrichedMessage{
			OriginalMessage: msg,
			Permalink:       msg.Permalink,
		})
	}
	return enriched
}

func originalMessagesFromEnriched(enriched []EnrichedMessage) []slack.Message {
	messages := make([]slack.Message, 0, len(enriched))
	for _, msg := range enriched {
		messages = append(messages, msg.OriginalMessage)
	}
	return messages
}

func enrichedMessagesByKey(enriched []EnrichedMessage) map[string]EnrichedMessage {
	byKey := make(map[string]EnrichedMessage, len(enriched))
	for _, msg := range enriched {
		key := fmt.Sprintf("%s:%s", msg.OriginalMessage.Channel, msg.OriginalMessage.Timestamp)
		byKey[key] = msg
	}
	return byKey
}

func (s *SlackSearchService) buildResult(
	start time.Time,
	executedQueries []string,
	iterationCount int,
	totalMatches int,
	enriched []EnrichedMessage,
	suff *SufficiencyResponse,
) *SlackSearchResult {
	if iterationCount <= 0 {
		iterationCount = 1
	}

	result := &SlackSearchResult{
		EnrichedMessages: enriched,
		Queries:          executedQueries,
		IterationCount:   iterationCount,
		TotalMatches:     totalMatches,
		ExecutionTime:    time.Since(start),
		Sources:          make(map[string]string),
	}

	if suff != nil {
		result.IsSufficient = suff.IsSufficient
		result.MissingInfo = coalesceStrings(suff.MissingInfo)
	} else {
		result.IsSufficient = len(enriched) > 0
		result.MissingInfo = []string{}
	}

	for _, msg := range enriched {
		if msg.Permalink == "" {
			continue
		}
		key := fmt.Sprintf("%s#%s", msg.OriginalMessage.Channel, msg.OriginalMessage.Timestamp)
		result.Sources[key] = msg.Permalink
	}

	return result
}

func filterBotSearchResults(messages []slack.Message, enriched []EnrichedMessage) ([]slack.Message, []EnrichedMessage, int) {
	filteredMessages, dropped := filterBotMessages(messages)
	if len(enriched) == 0 || dropped == 0 {
		return filteredMessages, enriched, dropped
	}

	enrichedByKey := enrichedMessagesByKey(enriched)
	filteredEnriched := make([]EnrichedMessage, 0, len(filteredMessages))
	for _, msg := range filteredMessages {
		key := fmt.Sprintf("%s:%s", msg.Channel, msg.Timestamp)
		if enrichedMsg, ok := enrichedByKey[key]; ok {
			filteredEnriched = append(filteredEnriched, enrichedMsg)
		}
	}

	return filteredMessages, filteredEnriched, dropped
}

func filterBotMessages(messages []slack.Message) ([]slack.Message, int) {
	if len(messages) == 0 {
		return messages, 0
	}

	filtered := make([]slack.Message, 0, len(messages))
	dropped := 0

	for _, msg := range messages {
		if isBotMessage(msg) {
			dropped++
			continue
		}
		filtered = append(filtered, msg)
	}

	return filtered, dropped
}

func isBotMessage(msg slack.Message) bool {
	if msg.BotID != "" || msg.SubType == "bot_message" {
		return true
	}

	user := strings.TrimSpace(msg.User)
	if user == "" {
		return strings.TrimSpace(msg.Username) != ""
	}

	if strings.EqualFold(user, "USLACKBOT") || strings.HasPrefix(user, "B") {
		return true
	}

	return false
}

// FetchMessagesFromQuery detects Slack URLs in the query and fetches their content.
// Returns nil if no Slack URLs are found in the query.
// This method works independently of the Slack search enabled setting.
func (s *SlackSearchService) FetchMessagesFromQuery(ctx context.Context, query string) (*FetchResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	ctx, span := slackSearchTracer.Start(ctx, "slacksearch.service.fetch_from_query")
	defer span.End()

	// Detect Slack URLs in the query
	urls := DetectSlackURLs(query)
	if len(urls) == 0 {
		span.SetAttributes(attribute.Bool("slack.has_urls", false))
		return nil, nil
	}

	span.SetAttributes(
		attribute.Bool("slack.has_urls", true),
		attribute.Int("slack.url_count", len(urls)),
		attribute.String("slack.query_hash", telemetryFingerprint(query)),
	)

	s.logger.Printf("SlackSearchService: detected %d Slack URL(s) in query", len(urls))

	// Check if message fetcher is available
	if s.messageFetcher == nil {
		err := fmt.Errorf("message fetcher not initialized (Slack client may not be configured)")
		span.RecordError(err)
		return nil, err
	}

	// Fetch messages from URLs
	return s.messageFetcher.FetchByURLs(ctx, &FetchRequest{
		URLs:      urls,
		UserQuery: query,
	})
}

// HasSlackURLs checks if the query contains any Slack message URLs.
func (s *SlackSearchService) HasSlackURLs(query string) bool {
	return HasSlackURL(query)
}

// GetMessageFetcher returns the message fetcher for direct use.
// Returns nil if the fetcher is not initialized.
func (s *SlackSearchService) GetMessageFetcher() *MessageFetcher {
	return s.messageFetcher
}

// buildURLOnlyResult constructs a SlackSearchResult when only URL direct fetch is used.
// This is returned when the query contains only Slack URLs with no additional search text.
func (s *SlackSearchService) buildURLOnlyResult(start time.Time, messages []EnrichedMessage) *SlackSearchResult {
	result := &SlackSearchResult{
		EnrichedMessages: messages,
		Queries:          []string{"[URL direct fetch]"},
		IterationCount:   0,
		TotalMatches:     len(messages),
		ExecutionTime:    time.Since(start),
		IsSufficient:     len(messages) > 0,
		MissingInfo:      []string{},
		Sources:          make(map[string]string),
	}

	// Populate Sources with permalinks
	for _, msg := range messages {
		if msg.Permalink != "" {
			key := fmt.Sprintf("%s#%s", msg.OriginalMessage.Channel, msg.OriginalMessage.Timestamp)
			result.Sources[key] = msg.Permalink
		}
	}

	return result
}

// mergeEnrichedMessages combines URL-fetched messages with search results.
// URL-fetched messages are placed first, and duplicates are removed.
// Duplicates are identified by channel:timestamp combination.
func mergeEnrichedMessages(urlMessages, searchMessages []EnrichedMessage) []EnrichedMessage {
	seen := make(map[string]struct{})
	result := make([]EnrichedMessage, 0, len(urlMessages)+len(searchMessages))

	// Add URL-fetched messages first
	for _, msg := range urlMessages {
		key := fmt.Sprintf("%s:%s", msg.OriginalMessage.Channel, msg.OriginalMessage.Timestamp)
		if _, exists := seen[key]; !exists {
			seen[key] = struct{}{}
			result = append(result, msg)
		}
	}

	// Add search results, skipping duplicates
	for _, msg := range searchMessages {
		key := fmt.Sprintf("%s:%s", msg.OriginalMessage.Channel, msg.OriginalMessage.Timestamp)
		if _, exists := seen[key]; !exists {
			seen[key] = struct{}{}
			result = append(result, msg)
		}
	}

	return result
}
