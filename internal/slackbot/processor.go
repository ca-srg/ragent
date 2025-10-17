package slackbot

import (
	"context"
	"log"
	"strings"

	"github.com/slack-go/slack"
)

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

	// Extract query
	query := p.extractor.ExtractQuery(botUserID, msg.Text)
	if strings.TrimSpace(query) == "" {
		opts := p.format.BuildUsage("質問内容が見つかりませんでした。例: @bot 今日は何をすべき？")
		return &Reply{Channel: msg.Channel, MsgOptions: []slack.MsgOption{opts}}
	}

	searchQuery := query
	if p.threadBuilder != nil {
		enhancedQuery, err := p.threadBuilder.Build(ctx, msg.Channel, msg.ThreadTimestamp, query)
		if err != nil {
			log.Printf("thread_context_build_error channel=%s thread_ts=%s err=%v", msg.Channel, msg.ThreadTimestamp, err)
		}
		if strings.TrimSpace(enhancedQuery) != "" {
			searchQuery = enhancedQuery
		}
	}

	// Perform search
	result := p.search.Search(ctx, searchQuery)
	// Format
	opts := p.format.BuildSearchResult(query, result)
	return &Reply{Channel: msg.Channel, MsgOptions: []slack.MsgOption{opts}}
}
