package slackbot

import (
	"fmt"
	"strings"
)

// QueryExtractor extracts user question text from a message
type QueryExtractor struct{}

func (e *QueryExtractor) ExtractQuery(botUserID string, text string) string {
	if text == "" {
		return ""
	}
	mention := fmt.Sprintf("<@%s>", botUserID)
	cleaned := strings.TrimSpace(strings.ReplaceAll(text, mention, ""))
	if cleaned == "" {
		return ""
	}
	// collapse whitespace
	fields := strings.Fields(cleaned)
	return strings.Join(fields, " ")
}
