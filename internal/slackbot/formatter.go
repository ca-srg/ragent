package slackbot

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/slack-go/slack"
)

const (
	maxSlackBlocks   = 45
	maxSlackMessages = 15
)

// Formatter builds Slack responses (Block Kit)
type Formatter struct{}

// BuildUsage creates a simple tip message with section block
func (f *Formatter) BuildUsage(tip string) slack.MsgOption {
	blocks := []slack.Block{
		slack.NewSectionBlock(&slack.TextBlockObject{Type: slack.MarkdownType, Text: ":information_source: 使い方"}, nil, nil),
		slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, tip, false, false), nil, nil),
	}
	return slack.MsgOptionBlocks(blocks...)
}

// BuildSearchResult formats search results into blocks
func (f *Formatter) BuildSearchResult(query string, result *SearchResult) slack.MsgOption {
	// If we have a generated response (chat-style), use that format
	if result.GeneratedResponse != "" {
		return f.BuildChatResponse(query, result)
	}

	// Otherwise, use the traditional search result format
	header := slack.NewHeaderBlock(slack.NewTextBlockObject(slack.PlainTextType, fmt.Sprintf("検索結果: %s", truncate(query, 60)), false, false))
	metaParts := []string{
		fmt.Sprintf("%d 件", result.Total),
		fmt.Sprintf("%.0f ms", result.Elapsed.Seconds()*1000),
	}
	if result.SearchMethod != "" {
		metaParts = append(metaParts, fmt.Sprintf("モード: %s", result.SearchMethod))
	}
	if result.FallbackReason != "" {
		metaParts = append(metaParts, fmt.Sprintf("フォールバック: %s", result.FallbackReason))
	}
	intro := slack.NewContextBlock("", slack.NewTextBlockObject(slack.MarkdownType, strings.Join(metaParts, " / "), false, false))

	blocks := []slack.Block{header, intro}
	for i, item := range result.Items {
		var titleText string
		if item.Link != "" {
			titleText = fmt.Sprintf("%d. <%s|%s>", i+1, item.Link, escapeTitle(item.Title))
		} else {
			titleText = fmt.Sprintf("%d. %s", i+1, item.Title)
		}
		md := fmt.Sprintf("*%s*\n%s", titleText, item.Snippet)
		blocks = append(blocks, slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, md, false, false), nil, nil))
		// context line with category/path if available
		ctxParts := []string{}
		if item.Category != "" {
			ctxParts = append(ctxParts, fmt.Sprintf("カテゴリ: %s", item.Category))
		}
		if item.FilePath != "" {
			ctxParts = append(ctxParts, fmt.Sprintf("パス: %s", item.FilePath))
		}
		if len(ctxParts) > 0 {
			blocks = append(blocks, slack.NewContextBlock(
				fmt.Sprintf("ctx-meta-%d", i),
				slack.NewTextBlockObject(slack.MarkdownType, strings.Join(ctxParts, " · "), false, false),
			))
		}
	}
	if len(blocks) == 2 {
		blocks = append(blocks, slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, "該当する結果が見つかりませんでした。", false, false), nil, nil))
	}
	footer := slack.NewContextBlock("", slack.NewTextBlockObject(slack.MarkdownType, fmt.Sprintf("%sにより生成", "RAGent"), false, false))
	blocks = append(blocks, footer)
	return slack.MsgOptionBlocks(blocks...)
}

// BuildChatResponse formats LLM-generated response into blocks
func (f *Formatter) BuildChatResponse(query string, result *SearchResult) slack.MsgOption {
	response := result.GeneratedResponse
	if response == "" {
		response = "回答を生成できませんでした。"
	}
	// Header with query
	header := slack.NewHeaderBlock(slack.NewTextBlockObject(slack.PlainTextType, fmt.Sprintf("質問: %s", truncate(query, 60)), false, false))

	// Context info including search method
	metaParts := []string{
		fmt.Sprintf("%d 件の参考文献から生成", result.Total),
		fmt.Sprintf("%.0f ms", result.Elapsed.Seconds()*1000),
	}
	if result.ChatModel != "" {
		metaParts = append(metaParts, fmt.Sprintf("モデル: %s", result.ChatModel))
	}
	if result.SearchMethod != "" {
		metaParts = append(metaParts, fmt.Sprintf("モード: %s", result.SearchMethod))
	}
	if result.FallbackReason != "" {
		metaParts = append(metaParts, fmt.Sprintf("フォールバック: %s", result.FallbackReason))
	}
	intro := slack.NewContextBlock("", slack.NewTextBlockObject(slack.MarkdownType, strings.Join(metaParts, " / "), false, false))

	// Main response in sections (split if needed for Slack's character limits)
	blocks := []slack.Block{header, intro}

	// Split response by newlines to preserve formatting
	lines := strings.Split(response, "\n")
	currentSection := ""

	for _, line := range lines {
		// Check if this is a header line (## 参考文献)
		if strings.HasPrefix(line, "## ") {
			// Add current section if exists
			if currentSection != "" {
				normalized := normalizeMarkdownForSlack(currentSection)
				if strings.TrimSpace(normalized) != "" {
					blocks = append(blocks, slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, normalized, false, false), nil, nil))
				}
				currentSection = ""
			}
			// Add header as divider
			blocks = append(blocks, slack.NewDividerBlock())
			blocks = append(blocks, slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, "*"+strings.TrimPrefix(line, "## ")+"*", false, false), nil, nil))
		} else {
			// Build up section content
			if currentSection != "" {
				currentSection += "\n"
			}
			currentSection += line

			// If section gets too long, flush it (Slack has a 3000 char limit per text block)
			if len(currentSection) > 2500 {
				normalized := normalizeMarkdownForSlack(currentSection)
				if strings.TrimSpace(normalized) != "" {
					blocks = append(blocks, slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, normalized, false, false), nil, nil))
				}
				currentSection = ""
			}
		}
	}

	// Add remaining section
	if currentSection != "" {
		normalized := normalizeMarkdownForSlack(currentSection)
		if strings.TrimSpace(normalized) != "" {
			blocks = append(blocks, slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, normalized, false, false), nil, nil))
		}
	}

	if result != nil && result.Slack != nil {
		blocks = append(blocks, buildSlackResultBlocks(result.Slack)...)
	}

	// Footer
	footer := slack.NewContextBlock("", slack.NewTextBlockObject(slack.MarkdownType, fmt.Sprintf("%sにより生成", "RAGent"), false, false))
	blocks = append(blocks, footer)

	return slack.MsgOptionBlocks(blocks...)
}

// BuildError renders an error message in a block
func (f *Formatter) BuildError(message string) slack.MsgOption {
	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, ":warning: エラーが発生しました", false, false),
			nil, nil,
		),
		slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, message, false, false),
			nil, nil,
		),
	}
	return slack.MsgOptionBlocks(blocks...)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}

func escapeTitle(s string) string {
	// Slack link title escaping is minimal here; ensure no '|' characters
	return strings.ReplaceAll(s, "|", "-")
}

func channelName(id string) string {
	if id == "" {
		return "-"
	}
	return id
}

func displayUser(userID, username string) string {
	if username != "" {
		return username
	}
	if userID != "" {
		return userID
	}
	return "unknown"
}

func normalizeMarkdownForSlack(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return text
	}

	normalized := make([]string, 0, len(lines))
	insideCode := false

	for _, line := range lines {
		trimmedLeft := strings.TrimLeft(line, " \t")

		if strings.HasPrefix(trimmedLeft, "```") {
			insideCode = !insideCode
			normalized = append(normalized, strings.TrimRight(trimmedLeft, " \t"))
			continue
		}

		if insideCode {
			normalized = append(normalized, line)
			continue
		}

		if trimmedLeft == "" {
			normalized = append(normalized, "")
			continue
		}

		if level, heading := parseHeading(trimmedLeft); level > 0 {
			normalized = append(normalized, formatHeading(level, heading))
			continue
		}

		indent := len(line) - len(trimmedLeft)
		if bullet := formatBulletLine(trimmedLeft, indent); bullet != "" {
			normalized = append(normalized, bullet)
			continue
		}

		if strings.HasPrefix(trimmedLeft, ">") {
			normalized = append(normalized, convertMarkdownBoldForSlack("> "+strings.TrimSpace(strings.TrimPrefix(trimmedLeft, ">"))))
			continue
		}

		normalized = append(normalized, convertMarkdownBoldForSlack(strings.TrimRight(line, " \t")))
	}

	return strings.Join(normalized, "\n")
}

func parseHeading(line string) (int, string) {
	if !strings.HasPrefix(line, "#") {
		return 0, ""
	}

	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}

	if level == 0 || level >= len(line) || line[level] != ' ' {
		return 0, ""
	}

	text := strings.TrimSpace(line[level+1:])
	if text == "" {
		return 0, ""
	}
	return level, text
}

func formatHeading(level int, text string) string {
	switch {
	case text == "":
		return ""
	case level <= 3:
		return fmt.Sprintf("*%s*", convertMarkdownBoldForSlack(text))
	default:
		return fmt.Sprintf("_%s_", convertMarkdownBoldForSlack(text))
	}
}

func formatBulletLine(line string, indent int) string {
	if len(line) >= 2 {
		if strings.HasPrefix(line, "- [") || strings.HasPrefix(line, "* [") {
			closing := strings.Index(line, "]")
			if closing > 1 {
				label := strings.TrimSpace(line[closing+1:])
				if label != "" {
					checkbox := strings.TrimSpace(line[2:closing])
					emoji := ":white_large_square:"
					if strings.EqualFold(checkbox, "[x") {
						emoji = ":white_check_mark:"
					}
					return strings.Repeat(" ", indent) + emoji + " " + convertMarkdownBoldForSlack(label)
				}
			}
		}

		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "+ ") {
			content := strings.TrimSpace(line[2:])
			if content == "" {
				return ""
			}
			return strings.Repeat(" ", indent) + "• " + convertMarkdownBoldForSlack(content)
		}
	}

	if dot := strings.Index(line, "."); dot > 0 && isDigits(line[:dot]) {
		content := strings.TrimSpace(line[dot+1:])
		prefix := strings.Repeat(" ", indent)
		if content == "" {
			return prefix + line[:dot] + "."
		}
		return prefix + line[:dot] + ". " + convertMarkdownBoldForSlack(content)
	}

	return ""
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func humanTimestamp(ts string) string {
	if ts == "" {
		return "-"
	}
	seconds, err := strconv.ParseFloat(ts, 64)
	if err != nil {
		return ts
	}
	secs := int64(seconds)
	nsecs := int64((seconds - math.Floor(seconds)) * 1e9)
	return time.Unix(secs, nsecs).Format(time.RFC3339)
}

// SearchResult is a simplified structure for Slack formatting
type SearchResult struct {
	Items             []SearchItem
	Total             int
	Elapsed           time.Duration
	GeneratedResponse string // LLM generated response (for chat-style responses)
	ChatModel         string
	SearchMethod      string
	URLDetected       bool
	FallbackReason    string
	Slack             *SlackConversationResult
}

type SearchItem struct {
	Title    string
	Snippet  string
	Score    float64
	Source   string
	Link     string
	Category string
	FilePath string
}

func buildSlackResultBlocks(result *SlackConversationResult) []slack.Block {
	blocks := []slack.Block{
		slack.NewDividerBlock(),
		slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, "*Conversations from Slack:*", false, false),
			nil,
			nil,
		),
	}

	if result == nil || len(result.Messages) == 0 {
		blocks = append(blocks, slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, "関連するSlackメッセージは見つかりませんでした。", false, false), nil, nil))
		return blocks
	}

	meta := fmt.Sprintf("%d件 / %d回の探索", result.TotalMatches, result.IterationCount)
	if !result.IsSufficient && len(result.MissingInfo) > 0 {
		meta += fmt.Sprintf(" / 不足情報: %s", strings.Join(result.MissingInfo, ", "))
	}
	blocks = append(blocks, slack.NewContextBlock("slack-meta", slack.NewTextBlockObject(slack.MarkdownType, meta, false, false)))

	displayCount := 0
	truncated := false
	totalMessages := len(result.Messages)
	for i, msg := range result.Messages {
		if displayCount >= maxSlackMessages {
			truncated = true
			break
		}
		channel := channelName(msg.Channel)
		stamp := humanTimestamp(msg.Timestamp)
		user := displayUser(msg.User, msg.Username)
		body := strings.TrimSpace(msg.Text)
		if body == "" {
			continue
		}

		header := fmt.Sprintf("*#%s* • %s • %s", channel, stamp, user)
		blockBudget := maxSlackBlocks - len(blocks)
		if blockBudget <= 2 { // ensure space for context + section at least
			truncated = true
			break
		}
		blocks = append(blocks, slack.NewContextBlock(
			fmt.Sprintf("slack-msg-meta-%d", i),
			slack.NewTextBlockObject(slack.MarkdownType, header, false, false),
		))

		text := truncateSlackText(body, 600)
		section := slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, text, false, false), nil, nil)
		if msg.Permalink != "" {
			btn := slack.NewButtonBlockElement(fmt.Sprintf("slack-msg-%d", i), "view", slack.NewTextBlockObject(slack.PlainTextType, "View", false, false))
			btn.URL = msg.Permalink
			section.Accessory = slack.NewAccessory(btn)
		}
		blocks = append(blocks, section)

		if len(msg.Thread) > 0 {
			if len(blocks) >= maxSlackBlocks {
				truncated = true
				break
			}
			var lines []string
			for _, reply := range msg.Thread {
				stamp := humanTimestamp(reply.Timestamp)
				threadUser := displayUser(reply.User, reply.Username)
				lines = append(lines, fmt.Sprintf("• %s • %s • %s", stamp, threadUser, truncateSlackText(strings.TrimSpace(reply.Text), 200)))
			}
			blocks = append(blocks, slack.NewContextBlock(fmt.Sprintf("slack-thread-%d", i), slack.NewTextBlockObject(slack.MarkdownType, strings.Join(lines, "\n"), false, false)))
		}

		displayCount++
	}

	if truncated {
		remaining := totalMessages - displayCount
		if remaining > 0 && len(blocks) < maxSlackBlocks {
			notice := fmt.Sprintf("… Slackメッセージが多いため %d 件のみ表示しています (残り %d 件)", displayCount, remaining)
			blocks = append(blocks, slack.NewContextBlock("slack-truncated", slack.NewTextBlockObject(slack.MarkdownType, notice, false, false)))
		}
	}

	return blocks
}

// BuildSlackResultBlocksForTest exposes Slack result blocks for testing and validation purposes.
func BuildSlackResultBlocksForTest(result *SlackConversationResult) []slack.Block {
	return buildSlackResultBlocks(result)
}

func truncateSlackText(value string, limit int) string {
	if limit <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}

var boldMarkdownPattern = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)

func convertMarkdownBoldForSlack(s string) string {
	if !strings.Contains(s, "**") {
		return s
	}
	return boldMarkdownPattern.ReplaceAllString(s, "*$1*")
}
