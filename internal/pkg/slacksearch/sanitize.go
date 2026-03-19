package slacksearch

import (
	"fmt"
	"regexp"
	"strings"
)

// SanitizeSlackChannels strips '#' prefix and whitespace from channel names.
// Returns nil for empty/nil input.
func SanitizeSlackChannels(channels []string) []string {
	if len(channels) == 0 {
		return nil
	}
	clean := make([]string, 0, len(channels))
	for _, ch := range channels {
		ch = strings.TrimSpace(ch)
		ch = strings.TrimPrefix(ch, "#")
		if ch != "" {
			clean = append(clean, ch)
		}
	}
	if len(clean) == 0 {
		return nil
	}
	return clean
}

// NormalizeSlackChannel strips whitespace and '#' prefix, then lowercases.
// This is the mcpserver-specific normalization that includes ToLower.
func NormalizeSlackChannel(value string) string {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.TrimPrefix(trimmed, "#")
	if trimmed == "" {
		return ""
	}
	return strings.ToLower(trimmed)
}

// NormalizeSlackChannels applies NormalizeSlackChannel (with ToLower) to a slice.
func NormalizeSlackChannels(channels []string) []string {
	result := make([]string, 0, len(channels))
	for _, ch := range channels {
		if normalized := NormalizeSlackChannel(ch); normalized != "" {
			result = append(result, normalized)
		}
	}
	return result
}

// Regex patterns for Slack mention markup: <@U123>, <@U123|name>, <!here>, <!subteam^S|@t>
var (
	slackUserMentionRe    = regexp.MustCompile(`<@([^>|]+)(?:\|([^>]*))?>`)
	slackSpecialMentionRe = regexp.MustCompile(`<!(here|channel|everyone)(?:\|([^>]*))?>`)
	slackSubteamMentionRe = regexp.MustCompile(`<!subteam\^[^|>]+(?:\|([^>]*))?>`)
)

// EscapeSlackMentions converts Slack mention markup to plain text so that
// reposting the text does not trigger notifications.
func EscapeSlackMentions(text string) string {
	if text == "" {
		return text
	}

	text = slackUserMentionRe.ReplaceAllStringFunc(text, func(match string) string {
		subs := slackUserMentionRe.FindStringSubmatch(match)
		if len(subs) >= 3 && subs[2] != "" {
			return "@" + subs[2]
		}
		if len(subs) >= 2 {
			return "@" + subs[1]
		}
		return match
	})

	text = slackSpecialMentionRe.ReplaceAllStringFunc(text, func(match string) string {
		subs := slackSpecialMentionRe.FindStringSubmatch(match)
		if len(subs) >= 3 && subs[2] != "" {
			return "@" + subs[2]
		}
		if len(subs) >= 2 {
			return "@" + subs[1]
		}
		return match
	})

	text = slackSubteamMentionRe.ReplaceAllStringFunc(text, func(match string) string {
		subs := slackSubteamMentionRe.FindStringSubmatch(match)
		if len(subs) >= 2 && subs[1] != "" {
			return subs[1]
		}
		return match
	})

	return text
}

// PrintSlackResults prints Slack search results to stdout.
func PrintSlackResults(result *SlackSearchResult) {
	fmt.Println("\n=== Slack Conversations ===")
	if result == nil || len(result.EnrichedMessages) == 0 {
		fmt.Println("  (no Slack messages found)")
		return
	}

	fmt.Printf("Iterations: %d | Total Matches: %d\n", result.IterationCount, result.TotalMatches)
	if len(result.Queries) > 0 {
		fmt.Printf("Queries tried: %s\n", strings.Join(result.Queries, ", "))
	}
	if !result.IsSufficient && len(result.MissingInfo) > 0 {
		fmt.Printf("Missing info: %s\n", strings.Join(result.MissingInfo, "; "))
	}

	for i, msg := range result.EnrichedMessages {
		orig := msg.OriginalMessage
		fmt.Printf("\n  %d. #%s | %s | %s\n", i+1, FormatSlackChannel(orig.Channel), FormatSlackTimestamp(orig.Timestamp), FormatSlackUser(orig.User, orig.Username))
		fmt.Printf("     %s\n", strings.TrimSpace(orig.Text))
		if msg.Permalink != "" {
			fmt.Printf("     Permalink: %s\n", msg.Permalink)
		}
		if len(msg.ThreadMessages) > 0 {
			fmt.Printf("     Thread replies (%d):\n", len(msg.ThreadMessages))
			for _, reply := range msg.ThreadMessages {
				fmt.Printf("       - [%s] %s\n", FormatSlackTimestamp(reply.Timestamp), strings.TrimSpace(reply.Text))
			}
		}
	}
}
