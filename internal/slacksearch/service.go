package slacksearch

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/ca-srg/ragent/internal/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/slackbot"
	"github.com/ca-srg/ragent/internal/types"
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
type SlackSearchService struct {
	botClient          *slack.Client
	userClient         *slack.Client
	queryGenerator     slackQueryGenerator
	searcher           slackSearcher
	contextRetriever   slackContextRetriever
	sufficiencyChecker slackSufficiencyChecker
	config             *SlackSearchConfig
	logger             *log.Logger
	progressHandler    func(iteration int, maxIterations int)
}

// NewSlackSearchService constructs a new SlackSearchService instance.
func NewSlackSearchService(
	baseConfig *types.Config,
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
	if cfg.Enabled && strings.TrimSpace(cfg.UserToken) == "" {
		return nil, fmt.Errorf("SLACK_USER_TOKEN must be set when Slack search is enabled")
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid Slack search configuration: %w", err)
	}

	var userClient *slack.Client
	if strings.TrimSpace(cfg.UserToken) != "" {
		userClient = slack.New(cfg.UserToken)
	}

	searchLimiter := slackbot.NewRateLimiter(20, 20, 20)
	contextLimiter := slackbot.NewRateLimiter(60, 120, 200)

	llmTimeout := time.Duration(cfg.LLMTimeoutSeconds) * time.Second
	queryGenerator := NewQueryGenerator(bedrockClient, llmTimeout)

	var (
		searcher         slackSearcher
		contextRetriever slackContextRetriever
		err              error
	)

	if userClient != nil {
		searcher = NewSearcher(userClient, searchLimiter)
		contextRetriever, err = NewContextRetriever(userClient, contextLimiter, bedrockClient, cfg, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create context retriever: %w", err)
		}
	}

	sufficiencyChecker := NewSufficiencyChecker(bedrockClient, logger, llmTimeout)

	service := &SlackSearchService{
		botClient:          botClient,
		userClient:         userClient,
		queryGenerator:     queryGenerator,
		searcher:           searcher,
		contextRetriever:   contextRetriever,
		sufficiencyChecker: sufficiencyChecker,
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

	if s.userClient == nil {
		return fmt.Errorf("slack user client is not configured")
	}

	s.logger.Printf("Slack search health check passed")
	return nil
}

// Search executes the Slack search pipeline.
func (s *SlackSearchService) Search(ctx context.Context, userQuery string, channels []string) (*SlackSearchResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	ctx, span := slackSearchTracer.Start(ctx, "slacksearch.service.search")
	defer span.End()

	queryHash := telemetryFingerprint(userQuery)
	span.SetAttributes(
		attribute.String("slack.query_hash", queryHash),
		attribute.Int("slack.channel_count", len(channels)),
	)
	s.logger.Printf("Slack search started hash=%s channels=%d enabled=%t", queryHash, len(channels), s.config.Enabled)

	if !s.config.Enabled {
		err := fmt.Errorf("slack search is disabled in configuration")
		span.RecordError(err)
		span.SetStatus(codes.Error, "slack_search_disabled")
		return nil, err
	}
	if s.queryGenerator == nil || s.searcher == nil || s.contextRetriever == nil || s.sufficiencyChecker == nil {
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

	startTime := time.Now()
	maxIterations := s.config.MaxIterations
	if maxIterations <= 0 {
		maxIterations = 1
	}

	var (
		executedQueries  []string
		enrichedMessages []EnrichedMessage
		totalMatches     int
		suffResult       *SufficiencyResponse
		previousQueries  []string
		previousResults  int
		iterationsDone   int
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
			searchMessages []slack.Message
			matches        int
			err            error
		)

		searchMessages, _, executedQueries, previousQueries, _, err = s.runSearchIteration(
			iterationCtx,
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

		searchMessages, botFiltered := filterBotMessages(searchMessages)
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

		contextResp, err := s.contextRetriever.RetrieveContext(iterationCtx, &ContextRequest{
			Messages:  searchMessages,
			UserQuery: userQuery,
		})
		if err != nil {
			s.logger.Printf("Slack search context retrieval failed: %v", err)
			iterSpan.AddEvent("context_retrieval_failed", trace.WithAttributes(attribute.String("error", err.Error())))
			contextResp = &ContextResponse{
				EnrichedMessages: s.fallbackEnriched(searchMessages),
				TotalRetrieved:   len(searchMessages),
			}
		} else {
			s.logger.Printf("Slack search context retrieved hash=%s retrieved=%d", queryHash, contextResp.TotalRetrieved)
		}

		enrichedMessages = contextResp.EnrichedMessages
		iterSpan.SetAttributes(
			attribute.Int("slack.enriched_message_count", len(enrichedMessages)),
			attribute.Int("slack.context_total_retrieved", contextResp.TotalRetrieved),
		)

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

		if suffResult.IsSufficient || iteration+1 >= maxIterations {
			iterSpan.AddEvent("sufficiency_reached", trace.WithAttributes(
				attribute.Bool("slack.is_sufficient", suffResult.IsSufficient),
				attribute.Int("slack.iteration", iteration+1),
			))
			iterSpan.End()
			break
		}

		iterSpan.End()
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
	userQuery string,
	channels []string,
	previousQueries []string,
	previousResults int,
	executedQueries []string,
	iteration int,
) ([]slack.Message, int, []string, []string, int, error) {
	var (
		searchMessages []slack.Message
		totalMatches   int
		genResp        *QueryGenerationResponse
		err            error
	)
	queryHash := telemetryFingerprint(userQuery)

	maxAttempts := s.config.MaxRetries
	if maxAttempts < 0 {
		maxAttempts = 0
	}

	for attempt := 0; attempt <= maxAttempts; attempt++ {
		genReq := &QueryGenerationRequest{
			UserQuery:       userQuery,
			PreviousQueries: previousQueries,
			PreviousResults: previousResults,
		}

		if iteration == 0 && attempt == 0 {
			genResp, err = s.queryGenerator.GenerateQueries(ctx, genReq)
		} else {
			genResp, err = s.queryGenerator.GenerateAlternativeQueries(ctx, genReq)
		}
		if err != nil {
			return nil, 0, executedQueries, previousQueries, previousResults, fmt.Errorf("failed to generate Slack queries: %w", err)
		}

		if len(channels) > 0 {
			genResp.ChannelFilter = append(genResp.ChannelFilter, channels...)
			genResp.ChannelFilter = uniqueStrings(genResp.ChannelFilter)
		}

		if len(genResp.Queries) == 0 {
			s.logger.Printf("Slack search iteration=%d hash=%s attempt=%d generated_zero_queries", iteration+1, queryHash, attempt+1)
			continue
		}

		s.logger.Printf("Slack search iteration=%d hash=%s attempt=%d generated_queries=%d channel_filters=%d time_filter=%t",
			iteration+1, queryHash, attempt+1, len(genResp.Queries), len(genResp.ChannelFilter), genResp.TimeFilter != nil)

		searchMessages, totalMatches, executedQueries = s.executeSlackSearch(ctx, genResp, executedQueries)
		previousQueries = append(previousQueries, genResp.Queries...)
		previousResults = totalMatches

		if len(searchMessages) > 0 {
			break
		}
	}

	return searchMessages, totalMatches, executedQueries, previousQueries, previousResults, nil
}

func (s *SlackSearchService) executeSlackSearch(
	ctx context.Context,
	genResp *QueryGenerationResponse,
	executedQueries []string,
) ([]slack.Message, int, []string) {
	messageMap := make(map[string]slack.Message)
	messages := make([]slack.Message, 0)
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
			return messages, totalMatches, executedQueries
		default:
		}

		request := &SearchRequest{
			Query:      query,
			TimeRange:  genResp.TimeFilter,
			Channels:   genResp.ChannelFilter,
			MaxResults: s.config.MaxResults,
		}

		response, err := s.searcher.SearchWithRetry(ctx, request, s.config.MaxRetries)
		executedQueries = append(executedQueries, query)
		if err != nil {
			s.logger.Printf("Slack search query failed hash=%s error=%v", telemetryFingerprint(query), err)
			continue
		}

		totalMatches += response.TotalCount
		s.logger.Printf("Slack search query succeeded hash=%s matches=%d total_matches=%d", telemetryFingerprint(query), len(response.Messages), totalMatches)
		for _, msg := range response.Messages {
			key := fmt.Sprintf("%s:%s", msg.Channel, msg.Timestamp)
			if _, exists := messageMap[key]; exists {
				continue
			}
			messageMap[key] = msg
			messages = append(messages, msg)
			if len(messages) >= limit {
				break
			}
		}
	}

	return messages, totalMatches, executedQueries
}

func (s *SlackSearchService) fallbackEnriched(messages []slack.Message) []EnrichedMessage {
	enriched := make([]EnrichedMessage, 0, len(messages))
	for _, msg := range messages {
		enriched = append(enriched, EnrichedMessage{
			OriginalMessage: msg,
		})
	}
	return enriched
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
