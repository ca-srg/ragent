package slacksearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ca-srg/ragent/internal/embedding/bedrock"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

const maxGeneratedQueries = 3

var (
	channelRegex    = regexp.MustCompile(`(?i)#([a-z0-9_\-]+)`)
	lastNDaysRegex  = regexp.MustCompile(`(?i)(?:last|past)\s+(\d+)\s+day`)
	lastNWeeksRegex = regexp.MustCompile(`(?i)(?:last|past)\s+(\d+)\s+week`)
)

var queryGeneratorSystemPrompt = strings.TrimSpace(`
You are an assistant that creates precise Slack search queries.
Always respond with a JSON object using this schema:
{
  "queries": ["string"],
  "channels": ["channel-name"],
  "time_filter": {"start": "ISO8601", "end": "ISO8601"} | null,
  "reasoning": "string"
}
- Generate 1-3 distinct queries optimized for Slack's search syntax.
- Include channel names without the leading '#'.
- If no specific time filter is appropriate, set "time_filter" to null.
- Reasoning should summarize why the queries were chosen.
- Do not include Markdown code fences; return pure JSON.
`)

// QueryGenerator generates Slack search queries using an LLM.
type bedrockChatClient interface {
	GenerateChatResponse(ctx context.Context, messages []bedrock.ChatMessage) (string, error)
}

type QueryGenerator struct {
	bedrockClient bedrockChatClient
	logger        *log.Logger
	nowFunc       func() time.Time
	llmTimeout    time.Duration
}

// QueryGenerationRequest captures the inputs needed to generate Slack search queries.
type QueryGenerationRequest struct {
	UserQuery        string
	PreviousQueries  []string
	PreviousResults  int
	ConversationHist []string
}

// QueryGenerationResponse encapsulates the generated queries and metadata.
type QueryGenerationResponse struct {
	Queries       []string
	TimeFilter    *TimeRange
	ChannelFilter []string
	Reasoning     string
}

type llmQueryPayload struct {
	Queries    []string `json:"queries"`
	Channels   []string `json:"channels"`
	Reasoning  string   `json:"reasoning"`
	TimeFilter *struct {
		Start string `json:"start"`
		End   string `json:"end"`
	} `json:"time_filter"`
}

// NewQueryGenerator creates a new QueryGenerator instance.
func NewQueryGenerator(bedrockClient *bedrock.BedrockClient, llmTimeout time.Duration) *QueryGenerator {
	logger := log.New(log.Default().Writer(), "slacksearch/query_generator ", log.LstdFlags)
	return &QueryGenerator{
		bedrockClient: bedrockClient,
		logger:        logger,
		nowFunc:       time.Now,
		llmTimeout:    llmTimeout,
	}
}

// GenerateQueries produces optimized Slack search queries from the user request.
func (g *QueryGenerator) GenerateQueries(ctx context.Context, req *QueryGenerationRequest) (*QueryGenerationResponse, error) {
	return g.generate(ctx, req, false)
}

// GenerateAlternativeQueries produces alternative queries when previous attempts failed.
func (g *QueryGenerator) GenerateAlternativeQueries(ctx context.Context, req *QueryGenerationRequest) (*QueryGenerationResponse, error) {
	return g.generate(ctx, req, true)
}

func (g *QueryGenerator) generate(ctx context.Context, req *QueryGenerationRequest, alternative bool) (*QueryGenerationResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, span := slackSearchTracer.Start(ctx, "slacksearch.query_generator.generate")
	defer span.End()

	if req == nil {
		err := fmt.Errorf("query generation request cannot be nil")
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid_request")
		return nil, err
	}
	if strings.TrimSpace(req.UserQuery) == "" {
		err := fmt.Errorf("user query cannot be empty")
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid_query")
		return nil, err
	}
	if g.bedrockClient == nil {
		err := fmt.Errorf("bedrock client not configured")
		span.RecordError(err)
		span.SetStatus(codes.Error, "bedrock_client_missing")
		return nil, err
	}

	queryHash := telemetryFingerprint(req.UserQuery)
	span.SetAttributes(
		attribute.String("slack.query_hash", queryHash),
		attribute.Bool("slack.alternative_generation", alternative),
		attribute.Int("slack.previous_queries", len(req.PreviousQueries)),
		attribute.Int("slack.previous_results", req.PreviousResults),
	)

	userPrompt := g.buildUserPrompt(req, alternative)
	g.logger.Printf("QueryGenerator: generating queries alternative=%t hash=%s", alternative, queryHash)

	payload, err := g.invokeLLM(ctx, userPrompt)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "llm_invoke_failed")
		return nil, err
	}

	queries := normalizeQueries(payload.Queries, req.PreviousQueries)
	if len(queries) == 0 {
		queries = []string{strings.TrimSpace(req.UserQuery)}
	}
	if len(queries) > maxGeneratedQueries {
		queries = queries[:maxGeneratedQueries]
	}

	// Merge channel hints from LLM and heuristics
	channelHints := extractChannels(req.UserQuery)
	channelHints = append(channelHints, payload.Channels...)
	channelHints = uniqueStrings(channelHints)

	// Determine time range
	timeRange := g.determineTimeRange(payload.TimeFilter, req.UserQuery)

	response := &QueryGenerationResponse{
		Queries:       queries,
		TimeFilter:    timeRange,
		ChannelFilter: channelHints,
		Reasoning:     payload.Reasoning,
	}

	span.SetAttributes(
		attribute.Int("slack.generated_queries", len(response.Queries)),
		attribute.Int("slack.channel_filters", len(response.ChannelFilter)),
		attribute.Bool("slack.time_filter_present", response.TimeFilter != nil),
	)
	g.logger.Printf("QueryGenerator: completed hash=%s alternative=%t queries=%d channels=%d time_filter=%t",
		queryHash, alternative, len(response.Queries), len(response.ChannelFilter), response.TimeFilter != nil)

	return response, nil
}

func (g *QueryGenerator) buildUserPrompt(req *QueryGenerationRequest, alternative bool) []bedrock.ChatMessage {
	var sb strings.Builder
	sb.WriteString("User query: ")
	sb.WriteString(strconv.Quote(strings.TrimSpace(req.UserQuery)))
	sb.WriteString("\n")

	if len(req.ConversationHist) > 0 {
		sb.WriteString("Conversation history (most recent last):\n")
		for _, msg := range req.ConversationHist {
			sb.WriteString("- ")
			sb.WriteString(msg)
			sb.WriteString("\n")
		}
	}

	if len(req.PreviousQueries) > 0 {
		sb.WriteString("Previous queries attempted:\n")
		for _, pq := range req.PreviousQueries {
			sb.WriteString("- ")
			sb.WriteString(pq)
			sb.WriteString("\n")
		}
		sb.WriteString(fmt.Sprintf("Previous result count: %d\n", req.PreviousResults))
	}

	if alternative {
		sb.WriteString("Generate alternative queries that avoid repeating previous ones and explore different angles.\n")
	} else {
		sb.WriteString("Generate the best initial queries for the Slack search API.\n")
	}

	sb.WriteString("Focus on extracting relevant channels, dates, and keywords.\n")

	messages := []bedrock.ChatMessage{
		{Role: "system", Content: queryGeneratorSystemPrompt},
		{Role: "user", Content: sb.String()},
	}
	return messages
}

func (g *QueryGenerator) invokeLLM(ctx context.Context, messages []bedrock.ChatMessage) (*llmQueryPayload, error) {
	ctx, cancel := context.WithTimeout(ctx, g.llmTimeout)
	defer cancel()

	responseText, err := g.bedrockClient.GenerateChatResponse(ctx, messages)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("failed to invoke bedrock for query generation: LLMリクエストがタイムアウトしました (llmRequestTimeout=%s): %w", g.llmTimeout, err)
		}
		return nil, fmt.Errorf("failed to invoke bedrock for query generation: %w", err)
	}

	cleaned := cleanLLMJSON(responseText)
	g.logger.Printf("QueryGenerator: LLM response: %s", cleaned)

	var payload llmQueryPayload
	if err := json.Unmarshal([]byte(cleaned), &payload); err != nil {
		return nil, fmt.Errorf("failed to parse LLM query generation response: %w", err)
	}

	return &payload, nil
}

func (g *QueryGenerator) determineTimeRange(timeFilter *struct {
	Start string `json:"start"`
	End   string `json:"end"`
}, userQuery string) *TimeRange {
	if timeFilter != nil {
		start, errStart := time.Parse(time.RFC3339, timeFilter.Start)
		end, errEnd := time.Parse(time.RFC3339, timeFilter.End)
		if errStart == nil && errEnd == nil {
			return &TimeRange{Start: &start, End: &end}
		}
		g.logger.Printf("QueryGenerator: failed to parse LLM time filter, falling back to heuristics: startErr=%v endErr=%v", errStart, errEnd)
	}
	return g.extractTimeRange(userQuery)
}

func (g *QueryGenerator) extractTimeRange(text string) *TimeRange {
	lower := strings.ToLower(text)
	now := g.nowFunc().UTC()

	switch {
	case strings.Contains(lower, "today"):
		start := startOfDay(now)
		end := now
		return &TimeRange{Start: &start, End: &end}
	case strings.Contains(lower, "yesterday"):
		start := startOfDay(now.AddDate(0, 0, -1))
		end := endOfDay(start)
		return &TimeRange{Start: &start, End: &end}
	case strings.Contains(lower, "last week") || strings.Contains(lower, "past week"):
		start := startOfDay(now.AddDate(0, 0, -7))
		end := now
		return &TimeRange{Start: &start, End: &end}
	case strings.Contains(lower, "this week"):
		start := startOfWeek(now)
		end := now
		return &TimeRange{Start: &start, End: &end}
	case strings.Contains(lower, "last month") || strings.Contains(lower, "past month"):
		start := startOfDay(now.AddDate(0, 0, -30))
		end := now
		return &TimeRange{Start: &start, End: &end}
	case strings.Contains(lower, "last year"):
		start := startOfDay(now.AddDate(-1, 0, 0))
		end := now
		return &TimeRange{Start: &start, End: &end}
	}

	if match := lastNDaysRegex.FindStringSubmatch(lower); len(match) == 2 {
		if days, err := strconv.Atoi(match[1]); err == nil && days > 0 {
			start := startOfDay(now.AddDate(0, 0, -days))
			end := now
			return &TimeRange{Start: &start, End: &end}
		}
	}

	if match := lastNWeeksRegex.FindStringSubmatch(lower); len(match) == 2 {
		if weeks, err := strconv.Atoi(match[1]); err == nil && weeks > 0 {
			start := startOfDay(now.AddDate(0, 0, -7*weeks))
			end := now
			return &TimeRange{Start: &start, End: &end}
		}
	}

	return nil
}

func cleanLLMJSON(raw string) string {
	cleaned := strings.TrimSpace(raw)
	if strings.HasPrefix(cleaned, "```") {
		cleaned = strings.TrimPrefix(cleaned, "```json")
		cleaned = strings.TrimPrefix(cleaned, "```")
	}
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)
	if idx := strings.IndexRune(cleaned, '{'); idx >= 0 {
		if end := strings.LastIndex(cleaned, "}"); end >= idx {
			cleaned = cleaned[idx : end+1]
		}
	}
	return strings.TrimSpace(cleaned)
}

func normalizeQueries(queries []string, previous []string) []string {
	unique := make([]string, 0, len(queries))
	seen := make(map[string]struct{})
	for _, q := range previous {
		seen[strings.ToLower(strings.TrimSpace(q))] = struct{}{}
	}
	for _, q := range queries {
		trimmed := strings.TrimSpace(q)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, trimmed)
	}
	return unique
}

func extractChannels(text string) []string {
	matches := channelRegex.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}

	channels := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(match[1]))
		if name == "" {
			continue
		}
		channels = append(channels, name)
	}
	return uniqueStrings(channels)
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, v := range values {
		key := strings.ToLower(strings.TrimSpace(v))
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	return result
}

func startOfDay(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func endOfDay(t time.Time) time.Time {
	return startOfDay(t).Add(24*time.Hour - time.Nanosecond)
}

func startOfWeek(t time.Time) time.Time {
	// Slack defaults to Monday as start of week for international workspaces
	weekday := int(t.Weekday())
	if weekday == 0 { // Sunday
		weekday = 7
	}
	return startOfDay(t.AddDate(0, 0, -(weekday - 1)))
}
