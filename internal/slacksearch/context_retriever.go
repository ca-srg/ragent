package slacksearch

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ca-srg/ragent/internal/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/slackbot"
	"github.com/slack-go/slack"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	defaultContextWindowMinutes = 30
	selectionPromptHeader       = `You help decide which Slack messages need additional context.`
	maxMessagePreviewLength     = 200
)

type slackConversationClient interface {
	GetConversationRepliesContext(ctx context.Context, params *slack.GetConversationRepliesParameters) ([]slack.Message, bool, string, error)
	GetConversationHistoryContext(ctx context.Context, params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error)
	GetPermalinkContext(ctx context.Context, params *slack.PermalinkParameters) (string, error)
}

// ContextRequest describes which messages require contextual enrichment.
type ContextRequest struct {
	Messages      []slack.Message
	UserQuery     string
	ContextWindow time.Duration
}

// ContextResponse contains enriched Slack messages.
type ContextResponse struct {
	EnrichedMessages []EnrichedMessage
	TotalRetrieved   int
}

type ContextRetriever struct {
	client        slackConversationClient
	rateLimiter   *slackbot.RateLimiter
	bedrockClient bedrockChatClient
	logger        *log.Logger

	contextWindow   time.Duration
	maxContextMsgs  int
	slackAPITimeout time.Duration
}

// NewContextRetriever constructs a new ContextRetriever instance.
func NewContextRetriever(
	client *slack.Client,
	rateLimiter *slackbot.RateLimiter,
	bedrockClient *bedrock.BedrockClient,
	config *SlackSearchConfig,
	logger *log.Logger,
) (*ContextRetriever, error) {
	if client == nil {
		return nil, fmt.Errorf("slack client cannot be nil")
	}
	if config == nil {
		return nil, fmt.Errorf("slack search config cannot be nil")
	}
	if bedrockClient == nil {
		return nil, fmt.Errorf("bedrock client cannot be nil")
	}
	if logger == nil {
		logger = log.Default()
	}

	windowMinutes := config.ContextWindowMinutes
	if windowMinutes <= 0 {
		windowMinutes = defaultContextWindowMinutes
	}

	timeoutSeconds := config.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 5
	}

	maxContext := config.MaxContextMessages
	if maxContext <= 0 {
		maxContext = 100
	}

	return &ContextRetriever{
		client:          client,
		rateLimiter:     rateLimiter,
		bedrockClient:   bedrockClient,
		logger:          logger,
		contextWindow:   time.Duration(windowMinutes) * time.Minute,
		maxContextMsgs:  maxContext,
		slackAPITimeout: time.Duration(timeoutSeconds) * time.Second,
	}, nil
}

// RetrieveContext enriches Slack messages with thread and timeline data.
func (c *ContextRetriever) RetrieveContext(ctx context.Context, req *ContextRequest) (*ContextResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, span := slackSearchTracer.Start(ctx, "slacksearch.context.retrieve")
	defer span.End()

	if req == nil {
		err := fmt.Errorf("context request cannot be nil")
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid_request")
		return nil, err
	}

	queryHash := telemetryFingerprint(req.UserQuery)
	span.SetAttributes(
		attribute.String("slack.query_hash", queryHash),
		attribute.Int("slack.input_messages", len(req.Messages)),
		attribute.Int("slack.max_context_messages", c.maxContextMsgs),
	)

	if len(req.Messages) == 0 {
		span.SetAttributes(attribute.Bool("slack.has_input_messages", false))
		c.logger.Printf("ContextRetriever: no messages to enrich hash=%s", queryHash)
		return &ContextResponse{EnrichedMessages: nil, TotalRetrieved: 0}, nil
	}

	window := c.contextWindow
	if req.ContextWindow > 0 {
		window = req.ContextWindow
	}
	span.SetAttributes(attribute.Int("slack.context_window_minutes", int(window.Minutes())))
	c.logger.Printf("ContextRetriever: start hash=%s messages=%d window=%s", queryHash, len(req.Messages), window)

	selected := c.selectMessagesForContext(ctx, req)
	if len(selected) == 0 {
		span.AddEvent("selection_fallback")
		c.logger.Printf("ContextRetriever: selection fallback hash=%s", queryHash)
		// Fallback: enrich all messages when selection fails
		for i := range req.Messages {
			selected = append(selected, i)
		}
	}

	remaining := c.maxContextMsgs
	enriched := make([]EnrichedMessage, 0, len(selected))
	totalRetrieved := 0

	for _, idx := range selected {
		if idx < 0 || idx >= len(req.Messages) {
			continue
		}
		if remaining <= 0 {
			break
		}

		msg := req.Messages[idx]
		enrichedMsg, consumed, err := c.enrichMessage(ctx, msg, window, remaining)
		if err != nil {
			c.logger.Printf("ContextRetriever: failed to enrich message %s: %v", msg.Timestamp, err)
			span.AddEvent("enrich_message_error", trace.WithAttributes(
				attribute.String("slack.message_timestamp", msg.Timestamp),
				attribute.String("error", err.Error()),
			))
			continue
		}
		remaining -= consumed
		totalRetrieved += consumed
		c.logger.Printf("ContextRetriever: enriched message hash=%s ts=%s thread=%d prev=%d next=%d permalink=%t",
			queryHash,
			msg.Timestamp,
			len(enrichedMsg.ThreadMessages),
			len(enrichedMsg.PreviousMessages),
			len(enrichedMsg.NextMessages),
			enrichedMsg.Permalink != "")
		enriched = append(enriched, enrichedMsg)
	}

	resp := &ContextResponse{
		EnrichedMessages: enriched,
		TotalRetrieved:   totalRetrieved,
	}

	span.SetAttributes(
		attribute.Int("slack.context_enriched_messages", len(resp.EnrichedMessages)),
		attribute.Int("slack.context_total_retrieved", resp.TotalRetrieved),
	)
	c.logger.Printf("ContextRetriever: completed hash=%s enriched=%d retrieved=%d", queryHash, len(resp.EnrichedMessages), resp.TotalRetrieved)
	return resp, nil
}

func (c *ContextRetriever) selectMessagesForContext(ctx context.Context, req *ContextRequest) []int {
	if c.bedrockClient == nil {
		return nil
	}

	ctx, span := slackSearchTracer.Start(ctx, "slacksearch.context.select_messages")
	defer span.End()

	span.SetAttributes(
		attribute.String("slack.query_hash", telemetryFingerprint(req.UserQuery)),
		attribute.Int("slack.candidate_messages", len(req.Messages)),
	)

	payload := c.buildSelectionPrompt(req)
	messages := []bedrock.ChatMessage{
		{Role: "system", Content: selectionPromptHeader},
		{Role: "user", Content: payload},
	}

	ctx, cancel := context.WithTimeout(ctx, llmRequestTimeout)
	defer cancel()

	resp, err := c.bedrockClient.GenerateChatResponse(ctx, messages)
	if err != nil {
		c.logger.Printf("ContextRetriever: LLM selection failed: %v", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "llm_selection_failed")
		return nil
	}

	cleaned := cleanLLMJSON(resp)

	var parsed struct {
		Indices []int `json:"message_indices"`
	}
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		c.logger.Printf("ContextRetriever: failed to parse selection JSON: %v", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, "selection_parse_failed")
		return nil
	}

	span.SetAttributes(attribute.Int("slack.selected_indices", len(parsed.Indices)))
	return parsed.Indices
}

func (c *ContextRetriever) buildSelectionPrompt(req *ContextRequest) string {
	var sb strings.Builder
	sb.WriteString("User query:\n")
	sb.WriteString(req.UserQuery)
	sb.WriteString("\n\nMessages:\n")

	for idx, msg := range req.Messages {
		sb.WriteString(fmt.Sprintf("- Index %d | Channel: %s | User: %s | HasThread: %t | Text: %s\n",
			idx,
			defaultString(msg.Channel, "unknown"),
			defaultString(msg.User, msg.Username),
			msg.ThreadTimestamp != "",
			trimPreview(msg.Text)))
	}

	sb.WriteString("\nRespond with JSON: {\"message_indices\": [int,...]}")
	return sb.String()
}

func (c *ContextRetriever) enrichMessage(ctx context.Context, msg slack.Message, window time.Duration, remaining int) (EnrichedMessage, int, error) {
	var consumed int
	enriched := EnrichedMessage{
		OriginalMessage: msg,
	}

	if permalink, err := c.getPermalink(ctx, msg.Channel, msg.Timestamp); err == nil {
		enriched.Permalink = permalink
	} else {
		c.logger.Printf("ContextRetriever: permalink fetch failed for %s: %v", msg.Timestamp, err)
	}

	// Thread context first (higher priority)
	if msg.ThreadTimestamp != "" {
		replies, count, err := c.getThreadContext(ctx, msg, remaining)
		if err != nil {
			c.logger.Printf("ContextRetriever: thread context error for %s: %v", msg.Timestamp, err)
		} else {
			enriched.ThreadMessages = replies
			consumed += count
			remaining -= count
		}
	}

	if remaining > 0 {
		prev, next, count, err := c.getTimelineContext(ctx, msg, window, remaining)
		if err != nil {
			c.logger.Printf("ContextRetriever: timeline context error for %s: %v", msg.Timestamp, err)
		} else {
			enriched.PreviousMessages = prev
			enriched.NextMessages = next
			consumed += count
		}
	}

	return enriched, consumed, nil
}

func (c *ContextRetriever) getThreadContext(ctx context.Context, msg slack.Message, remaining int) ([]slack.Message, int, error) {
	if remaining <= 0 {
		return nil, 0, nil
	}

	if err := c.allowRate(); err != nil {
		return nil, 0, err
	}

	params := &slack.GetConversationRepliesParameters{
		ChannelID: msg.Channel,
		Timestamp: msg.ThreadTimestamp,
		Limit:     remaining + 1, // include original post
	}

	ctx, cancel := context.WithTimeout(ctx, c.slackAPITimeout)
	defer cancel()

	replies, _, _, err := c.client.GetConversationRepliesContext(ctx, params)
	if err != nil {
		return nil, 0, err
	}

	threadMessages := make([]slack.Message, 0, len(replies))
	for _, reply := range replies {
		if reply.Timestamp == msg.Timestamp {
			continue // skip original to avoid duplication
		}
		threadMessages = append(threadMessages, reply)
		if len(threadMessages) >= remaining {
			break
		}
	}

	return threadMessages, len(threadMessages), nil
}

func (c *ContextRetriever) getTimelineContext(ctx context.Context, msg slack.Message, window time.Duration, remaining int) ([]slack.Message, []slack.Message, int, error) {
	if remaining <= 0 {
		return nil, nil, 0, nil
	}

	oldest, err := adjustSlackTimestamp(msg.Timestamp, -window)
	if err != nil {
		return nil, nil, 0, err
	}
	latest, err := adjustSlackTimestamp(msg.Timestamp, window)
	if err != nil {
		return nil, nil, 0, err
	}

	if err := c.allowRate(); err != nil {
		return nil, nil, 0, err
	}

	params := &slack.GetConversationHistoryParameters{
		ChannelID: msg.Channel,
		Oldest:    oldest,
		Latest:    latest,
		Limit:     remaining + 1, // include primary message
		Inclusive: false,
	}

	ctx, cancel := context.WithTimeout(ctx, c.slackAPITimeout)
	defer cancel()

	history, err := c.client.GetConversationHistoryContext(ctx, params)
	if err != nil {
		return nil, nil, 0, err
	}

	targetTS := msg.Timestamp
	previous := make([]slack.Message, 0)
	next := make([]slack.Message, 0)

	for _, candidate := range history.Messages {
		if candidate.Timestamp == targetTS {
			continue
		}
		if isEarlier(candidate.Timestamp, targetTS) {
			previous = append(previous, candidate)
		} else if isLater(candidate.Timestamp, targetTS) {
			next = append(next, candidate)
		}
	}

	sort.Slice(previous, func(i, j int) bool {
		return isEarlier(previous[i].Timestamp, previous[j].Timestamp)
	})
	sort.Slice(next, func(i, j int) bool {
		return isEarlier(next[i].Timestamp, next[j].Timestamp)
	})

	allowed := remaining
	prevLimited, consumedPrev := limitMessages(previous, allowed)
	allowed -= consumedPrev
	nextLimited, consumedNext := limitMessages(next, allowed)

	return prevLimited, nextLimited, consumedPrev + consumedNext, nil
}

func (c *ContextRetriever) getPermalink(ctx context.Context, channelID, ts string) (string, error) {
	if channelID == "" || ts == "" {
		return "", fmt.Errorf("channel and timestamp required for permalink")
	}
	if err := c.allowRate(); err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, c.slackAPITimeout)
	defer cancel()

	return c.client.GetPermalinkContext(ctx, &slack.PermalinkParameters{
		Channel: channelID,
		Ts:      ts,
	})
}

func (c *ContextRetriever) allowRate() error {
	if c.rateLimiter == nil {
		return nil
	}
	if !c.rateLimiter.Allow(slackSearchRateLimitKey, slackSearchRateLimitKey) {
		return fmt.Errorf("slack API rate limit exceeded")
	}
	return nil
}

func adjustSlackTimestamp(ts string, delta time.Duration) (string, error) {
	base, err := strconv.ParseFloat(ts, 64)
	if err != nil {
		return "", fmt.Errorf("invalid slack timestamp %s: %w", ts, err)
	}
	result := base + delta.Seconds()
	return fmt.Sprintf("%.6f", result), nil
}

func isEarlier(tsA, tsB string) bool {
	a, _ := strconv.ParseFloat(tsA, 64)
	b, _ := strconv.ParseFloat(tsB, 64)
	return a < b
}

func isLater(tsA, tsB string) bool {
	a, _ := strconv.ParseFloat(tsA, 64)
	b, _ := strconv.ParseFloat(tsB, 64)
	return a > b
}

func limitMessages(msgs []slack.Message, limit int) ([]slack.Message, int) {
	if limit <= 0 || len(msgs) == 0 {
		return nil, 0
	}
	if len(msgs) <= limit {
		return msgs, len(msgs)
	}
	return msgs[:limit], limit
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func trimPreview(text string) string {
	clean := strings.TrimSpace(text)
	if len(clean) <= maxMessagePreviewLength {
		return clean
	}
	return clean[:maxMessagePreviewLength] + "..."
}
