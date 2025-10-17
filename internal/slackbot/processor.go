package slackbot

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/slack-go/slack"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

var slackTracer = otel.Tracer("ragent/slackbot")

// Processor orchestrates detection, extraction, search, and formatting
type Processor struct {
	detector      *MentionDetector
	extractor     *QueryExtractor
	search        SearchAdapter
	format        *Formatter
	threadBuilder *ThreadContextBuilder
}

func NewProcessor(detector *MentionDetector, extractor *QueryExtractor, search SearchAdapter, formatter *Formatter, threadBuilder *ThreadContextBuilder) *Processor {
	return &Processor{
		detector:      detector,
		extractor:     extractor,
		search:        search,
		format:        formatter,
		threadBuilder: threadBuilder,
	}
}

// IsMentionOrDM reports whether the message targets the bot or is a DM
func (p *Processor) IsMentionOrDM(botUserID string, msg *slack.MessageEvent) bool {
	if msg == nil {
		return false
	}
	if p.detector.IsMentionToBot(botUserID, msg) {
		return true
	}
	return strings.HasPrefix(msg.Channel, "D")
}

// Reply is a transport-agnostic response later turned to slack.MsgOption
type Reply struct {
	Channel    string
	MsgOptions []slack.MsgOption
}

// ProcessMessage handles a Slack message and returns a reply or nil
func (p *Processor) ProcessMessage(ctx context.Context, botUserID string, msg *slack.MessageEvent) *Reply {
	if msg == nil {
		return nil
	}
	// Ignore messages from the bot itself
	if msg.User == botUserID {
		return nil
	}

	// Detect mention or DM
	isMention := p.detector.IsMentionToBot(botUserID, msg)
	isDM := strings.HasPrefix(msg.Channel, "D") // direct message channel ids start with D
	if !isMention && !isDM {
		return nil
	}

	ctx, span := slackTracer.Start(ctx, "slackbot.process_message")
	defer span.End()
	span.SetAttributes(otelTraceAttributes(msg, isMention, isDM)...)

	start := time.Now()
	status := "ok"
	hadError := false
	resultCount := 0
	channelType := "channel"
	if isDM {
		channelType = "dm"
	}
	attrs := []attribute.KeyValue{
		attribute.String("slack.channel", msg.Channel),
		attribute.Bool("slack.is_dm", isDM),
		attribute.Bool("slack.is_mention", isMention),
		attribute.String("slack.channel_type", channelType),
	}
	if msg.ThreadTimestamp != "" {
		attrs = append(attrs, attribute.Bool("slack.is_thread", true))
	}

	defer func() {
		attrs = append(attrs,
			attribute.String("slack.result.status", status),
			attribute.Int("slack.results.total", resultCount),
		)
		recordSlackMetrics(ctx, attrs, time.Since(start), hadError)
	}()

	// Extract query
	query := p.extractor.ExtractQuery(botUserID, msg.Text)
	if strings.TrimSpace(query) != "" {
		span.SetAttributes(attribute.String("slack.query.original", truncateForAttribute(query)))
	}
	if strings.TrimSpace(query) == "" {
		opts := p.format.BuildUsage("質問内容が見つかりませんでした。例: @bot 今日は何をすべき？")
		status = "error"
		hadError = true
		span.SetStatus(codes.Error, "empty query")
		return &Reply{Channel: msg.Channel, MsgOptions: []slack.MsgOption{opts}}
	}

	searchQuery := query
	if p.threadBuilder != nil {
		enhancedQuery, err := p.threadBuilder.Build(ctx, msg.Channel, msg.ThreadTimestamp, query)
		if err != nil {
			log.Printf("thread_context_build_error channel=%s thread_ts=%s err=%v", msg.Channel, msg.ThreadTimestamp, err)
			span.RecordError(err)
			hadError = true
		}
		if strings.TrimSpace(enhancedQuery) != "" {
			searchQuery = enhancedQuery
		}
	}
	if searchQuery != query {
		span.SetAttributes(attribute.String("slack.query.enhanced", truncateForAttribute(searchQuery)))
	}

	// Perform search
	result := p.search.Search(ctx, searchQuery)
	if result != nil {
		span.SetAttributes(
			attribute.Int("slack.search.results", result.Total),
			attribute.String("slack.search.method", result.SearchMethod),
		)
		if result.FallbackReason != "" {
			span.SetAttributes(attribute.String("slack.search.fallback_reason", result.FallbackReason))
		}
		if result.URLDetected {
			span.SetAttributes(attribute.Bool("slack.search.url_detected", true))
		}
		resultCount = result.Total
	}
	if result == nil {
		hadError = true
		status = "error"
	}

	// Format
	opts := p.format.BuildSearchResult(query, result)
	return &Reply{Channel: msg.Channel, MsgOptions: []slack.MsgOption{opts}}
}

func otelTraceAttributes(msg *slack.MessageEvent, isMention, isDM bool) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("slack.channel", msg.Channel),
		attribute.String("slack.user_id", msg.User),
		attribute.Bool("slack.is_mention", isMention),
		attribute.Bool("slack.is_dm", isDM),
	}
	if msg.ThreadTimestamp != "" {
		attrs = append(attrs, attribute.String("slack.thread_ts", msg.ThreadTimestamp))
	}
	return attrs
}

func truncateForAttribute(input string) string {
	const maxAttributeLength = 120
	trimmed := strings.TrimSpace(input)
	if len([]rune(trimmed)) <= maxAttributeLength {
		return trimmed
	}
	runes := []rune(trimmed)
	return string(runes[:maxAttributeLength]) + "…"
}
