package slacksearch

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// SlackURLInfo contains parsed Slack URL information.
type SlackURLInfo struct {
	ChannelID   string
	MessageTS   string
	ThreadTS    string
	OriginalURL string
}

// Slack URL patterns:
// Standard: https://workspace.slack.com/archives/C01234567/p1234567890123456
// With thread: https://workspace.slack.com/archives/C01234567/p1234567890123456?thread_ts=1234567890.123456
// App client format: https://app.slack.com/client/T01234567/C01234567/thread/C01234567-1234567890.123456
var (
	// Pattern for standard Slack archive URLs
	slackArchiveURLPattern = regexp.MustCompile(
		`https?://[a-zA-Z0-9\-_.]+\.slack\.com/archives/([A-Z0-9]+)/p(\d{10})(\d{6})`,
	)

	// Pattern for detecting any Slack URL in text
	slackURLDetectPattern = regexp.MustCompile(
		`https?://[a-zA-Z0-9\-_.]+\.slack\.com/archives/[A-Z0-9]+/p\d{16}[^\s]*`,
	)
)

// ParseSlackURL extracts channel ID and timestamp from a Slack URL.
// Returns nil and an error if the URL is not a valid Slack message URL.
func ParseSlackURL(rawURL string) (*SlackURLInfo, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("empty URL")
	}

	// Parse URL to handle query parameters
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL format: %w", err)
	}

	// Check if it's a Slack URL
	if !strings.Contains(parsedURL.Host, "slack.com") {
		return nil, fmt.Errorf("not a Slack URL: %s", parsedURL.Host)
	}

	// Match against archive URL pattern
	matches := slackArchiveURLPattern.FindStringSubmatch(rawURL)
	if len(matches) < 4 {
		return nil, fmt.Errorf("URL does not match Slack message format: %s", rawURL)
	}

	info := &SlackURLInfo{
		ChannelID:   matches[1],
		MessageTS:   fmt.Sprintf("%s.%s", matches[2], matches[3]),
		OriginalURL: rawURL,
	}

	// Extract thread_ts from query parameters if present
	threadTS := parsedURL.Query().Get("thread_ts")
	if threadTS != "" {
		info.ThreadTS = threadTS
	}

	return info, nil
}

// DetectSlackURLs finds all Slack message URLs in the given text.
// Returns a slice of SlackURLInfo for each detected URL.
// Invalid URLs are silently skipped.
func DetectSlackURLs(text string) []*SlackURLInfo {
	if strings.TrimSpace(text) == "" {
		return nil
	}

	matches := slackURLDetectPattern.FindAllString(text, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	results := make([]*SlackURLInfo, 0, len(matches))

	for _, match := range matches {
		// Normalize URL by trimming common trailing characters
		normalized := normalizeSlackURL(match)

		// Skip duplicates
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}

		// Parse the URL
		info, err := ParseSlackURL(normalized)
		if err != nil {
			continue // Skip invalid URLs
		}

		results = append(results, info)
	}

	return results
}

// HasSlackURL checks if the text contains any Slack message URLs.
func HasSlackURL(text string) bool {
	return slackURLDetectPattern.MatchString(text)
}

// ConvertSlackTimestamp converts a Slack p-timestamp (p1234567890123456) to
// standard timestamp format (1234567890.123456).
func ConvertSlackTimestamp(pTimestamp string) string {
	// Remove 'p' prefix if present
	ts := strings.TrimPrefix(pTimestamp, "p")
	if len(ts) != 16 {
		return pTimestamp // Return as-is if not 16 digits
	}

	// Split into seconds and microseconds
	seconds := ts[:10]
	micros := ts[10:]

	return fmt.Sprintf("%s.%s", seconds, micros)
}

// normalizeSlackURL cleans up a Slack URL by removing trailing punctuation
// that might have been captured during regex matching.
func normalizeSlackURL(rawURL string) string {
	// Trim common trailing characters that might be captured
	trimChars := `>)]\'",.;:!?`
	normalized := strings.TrimRight(rawURL, trimChars)

	// Handle markdown-style links: [text](url)
	if idx := strings.Index(normalized, ")"); idx > 0 {
		// Check if this looks like a closing paren from markdown
		if !strings.Contains(normalized[:idx], "(") {
			normalized = normalized[:idx]
		}
	}

	return normalized
}

// ExtractQueryWithoutURLs removes Slack URLs from the query text,
// returning the cleaned query for search purposes.
func ExtractQueryWithoutURLs(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}

	// Remove all Slack URLs from the text
	cleaned := slackURLDetectPattern.ReplaceAllString(text, "")

	// Clean up extra whitespace
	cleaned = strings.Join(strings.Fields(cleaned), " ")

	return strings.TrimSpace(cleaned)
}
