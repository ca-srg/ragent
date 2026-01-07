package slacksearch

import (
	"fmt"
	"strings"
	"time"

	"github.com/slack-go/slack"
)

// SlackSearchConfig contains runtime configuration for Slack search operations.
type SlackSearchConfig struct {
	Enabled              bool   `json:"enabled"`
	BotToken             string `json:"bot_token"`
	UserToken            string `json:"user_token"`
	MaxResults           int    `json:"max_results"`
	MaxRetries           int    `json:"max_retries"`
	ContextWindowMinutes int    `json:"context_window_minutes"`
	MaxIterations        int    `json:"max_iterations"`
	MaxContextMessages   int    `json:"max_context_messages"`
	TimeoutSeconds       int    `json:"timeout_seconds"`
	LLMTimeoutSeconds    int    `json:"llm_timeout_seconds"`
}

// TimeRange represents optional temporal bounds for Slack queries.
type TimeRange struct {
	Start *time.Time `json:"start,omitempty"`
	End   *time.Time `json:"end,omitempty"`
}

// EnrichedMessage combines a Slack message with additional contextual data.
type EnrichedMessage struct {
	OriginalMessage  slack.Message   `json:"original_message"`
	ThreadMessages   []slack.Message `json:"thread_messages,omitempty"`
	PreviousMessages []slack.Message `json:"previous_messages,omitempty"`
	NextMessages     []slack.Message `json:"next_messages,omitempty"`
	Permalink        string          `json:"permalink,omitempty"`
}

// SlackSearchResult captures the output of the Slack search pipeline.
type SlackSearchResult struct {
	EnrichedMessages []EnrichedMessage `json:"enriched_messages"`
	Queries          []string          `json:"queries"`
	IterationCount   int               `json:"iteration_count"`
	TotalMatches     int               `json:"total_matches"`
	ExecutionTime    time.Duration     `json:"execution_time"`
	IsSufficient     bool              `json:"is_sufficient"`
	MissingInfo      []string          `json:"missing_info,omitempty"`
	Sources          map[string]string `json:"sources,omitempty"`
}

// ForPrompt returns a formatted string of Slack messages for LLM context.
func (r *SlackSearchResult) ForPrompt() string {
	if r == nil || len(r.EnrichedMessages) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Slack Conversations:\n")
	for _, msg := range r.EnrichedMessages {
		orig := msg.OriginalMessage
		sb.WriteString(fmt.Sprintf("- #%s at %s by %s: %s\n",
			orig.Channel,
			formatSlackTimestamp(orig.Timestamp),
			displaySlackUser(orig.User, orig.Username),
			strings.TrimSpace(orig.Text),
		))
		for _, reply := range msg.ThreadMessages {
			sb.WriteString(fmt.Sprintf("    • Reply at %s by %s: %s\n",
				formatSlackTimestamp(reply.Timestamp),
				displaySlackUser(reply.User, reply.Username),
				strings.TrimSpace(reply.Text),
			))
		}
	}
	return sb.String()
}

// formatSlackTimestamp converts a Slack timestamp to RFC3339 format.
func formatSlackTimestamp(ts string) string {
	if ts == "" {
		return "-"
	}
	var sb strings.Builder
	for _, c := range ts {
		if c == '.' {
			break
		}
		sb.WriteRune(c)
	}
	secs := sb.String()
	if secs == "" {
		return ts
	}
	var seconds int64
	for _, c := range secs {
		if c < '0' || c > '9' {
			return ts
		}
		seconds = seconds*10 + int64(c-'0')
	}
	return time.Unix(seconds, 0).Format(time.RFC3339)
}

// displaySlackUser returns a display name for a Slack user.
func displaySlackUser(userID, username string) string {
	if username != "" {
		return username
	}
	if userID != "" {
		return userID
	}
	return "unknown"
}

// Validate ensures the Slack search configuration values are within supported ranges.
func (c *SlackSearchConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("SlackSearchConfig cannot be nil")
	}

	if !c.Enabled {
		return nil
	}

	if strings.TrimSpace(c.UserToken) == "" {
		return fmt.Errorf("user_token must be provided when Slack search is enabled")
	}

	if c.MaxResults <= 0 || c.MaxResults > 100 {
		return fmt.Errorf("max_results must be between 1 and 100 (got %d)", c.MaxResults)
	}
	if c.MaxRetries < 0 || c.MaxRetries > 10 {
		return fmt.Errorf("max_retries must be between 0 and 10 (got %d)", c.MaxRetries)
	}
	if c.ContextWindowMinutes <= 0 || c.ContextWindowMinutes > 720 {
		return fmt.Errorf("context_window_minutes must be between 1 and 720 (got %d)", c.ContextWindowMinutes)
	}
	if c.MaxIterations <= 0 || c.MaxIterations > 10 {
		return fmt.Errorf("max_iterations must be between 1 and 10 (got %d)", c.MaxIterations)
	}
	if c.MaxContextMessages <= 0 || c.MaxContextMessages > 500 {
		return fmt.Errorf("max_context_messages must be between 1 and 500 (got %d)", c.MaxContextMessages)
	}
	if c.TimeoutSeconds <= 0 || c.TimeoutSeconds > 60 {
		return fmt.Errorf("timeout_seconds must be between 1 and 60 (got %d)", c.TimeoutSeconds)
	}
	if c.LLMTimeoutSeconds <= 0 || c.LLMTimeoutSeconds > 300 {
		return fmt.Errorf("llm_timeout_seconds must be between 1 and 300 (got %d)", c.LLMTimeoutSeconds)
	}

	return nil
}
