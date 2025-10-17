package slackbot

import (
	"context"
	"log"
	"os"
	"strings"

	"github.com/ca-srg/ragent/internal/config"
	"github.com/slack-go/slack"
)

// ThreadContextBuilder constructs context-aware queries using Slack thread history.
type ThreadContextBuilder struct {
	client      SlackClient
	maxMessages int
	enabled     bool
	logger      *log.Logger
}

// NewThreadContextBuilder creates a ThreadContextBuilder configured from SlackConfig.
func NewThreadContextBuilder(client SlackClient, cfg *config.SlackConfig, logger *log.Logger) *ThreadContextBuilder {
	if logger == nil {
		logger = log.New(os.Stdout, "thread-context ", log.LstdFlags)
	}
	builder := &ThreadContextBuilder{
		client:      client,
		maxMessages: 10,
		enabled:     true,
		logger:      logger,
	}
	if cfg != nil {
		builder.enabled = cfg.ThreadContextEnabled
		builder.maxMessages = cfg.ThreadContextMaxMessages
	}
	if builder.maxMessages <= 0 {
		builder.maxMessages = 10
	}
	return builder
}

// Build returns a query enhanced with relevant thread history.
func (b *ThreadContextBuilder) Build(ctx context.Context, channel, threadTS, currentQuery string) (string, error) {
	if !b.enabled || threadTS == "" || b.client == nil {
		return currentQuery, nil
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return currentQuery, ctx.Err()
		default:
		}
	}

	params := &slack.GetConversationRepliesParameters{
		ChannelID: channel,
		Timestamp: threadTS,
		Limit:     b.maxMessages,
	}

	var collected []slack.Message
	for {
		messages, hasMore, cursor, err := b.client.GetConversationReplies(params)
		if err != nil {
			b.logf("thread_context_fetch_error channel=%s thread_ts=%s err=%v", channel, threadTS, err)
			return currentQuery, err
		}
		collected = append(collected, messages...)
		if len(collected) >= b.maxMessages {
			break
		}
		if !hasMore || cursor == "" {
			break
		}
		params.Cursor = cursor
		remaining := b.maxMessages - len(collected)
		if remaining <= 0 {
			break
		}
		params.Limit = remaining
	}

	if len(collected) == 0 {
		return currentQuery, nil
	}
	if len(collected) > b.maxMessages {
		collected = collected[len(collected)-b.maxMessages:]
	}

	history := b.formatThreadHistory(collected)
	if history == "" {
		return currentQuery, nil
	}

	var builder strings.Builder
	builder.WriteString(history)
	builder.WriteString("\n\n[現在の質問]\n")
	builder.WriteString(currentQuery)

	return builder.String(), nil
}

func (b *ThreadContextBuilder) formatThreadHistory(messages []slack.Message) string {
	if len(messages) == 0 {
		return ""
	}

	var buf strings.Builder
	buf.WriteString("[過去の会話]\n")
	for _, msg := range messages {
		text := strings.TrimSpace(msg.Text)
		if text == "" {
			continue
		}

		role := "ユーザー"
		if msg.BotID != "" || msg.SubType == "bot_message" || strings.EqualFold(msg.Username, "RAGent") {
			role = "BOT"
			if len([]rune(text)) > 200 {
				text = string([]rune(text)[:200])
			}
		}

		buf.WriteString(role)
		buf.WriteString(": ")
		buf.WriteString(text)
		buf.WriteString("\n")
	}

	formatted := strings.TrimSpace(buf.String())
	if formatted == "[過去の会話]" {
		return ""
	}
	return formatted
}

func (b *ThreadContextBuilder) logf(format string, args ...interface{}) {
	if b.logger != nil {
		b.logger.Printf(format, args...)
	}
}
