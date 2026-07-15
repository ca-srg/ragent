package slackbot

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/slack-go/slack"

	"github.com/ca-srg/ragent/internal/pkg/slacksearch"
)

const (
	maxSlackBlocks                = 45
	maxSlackMessages              = 15
	slackMessageBlockLimit        = 50
	slackSectionTextLimit         = 3000
	slackContextTextLimit         = 3000
	slackResponseSectionSoftLimit = 2500
)

// Formatter builds Slack responses (Block Kit)
type Formatter struct{}

// BuildUsage creates a simple tip message with section block
func (f *Formatter) BuildUsage(tip string) slack.MsgOption {
	blocks := appendSectionTextBlocks(nil, ":information_source: 使い方")
	blocks = appendSectionTextBlocks(blocks, tip)
	blocks = limitBlocksWithinMessageLimit(blocks)
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
	intro := newContextTextBlock("", strings.Join(metaParts, " / "))

	blocks := []slack.Block{header, intro}
	for i, item := range result.Items {
		var titleText string
		if item.Link != "" {
			titleText = fmt.Sprintf("%d. <%s|%s>", i+1, item.Link, escapeTitle(item.Title))
		} else {
			titleText = fmt.Sprintf("%d. %s", i+1, item.Title)
		}
		md := fmt.Sprintf("*%s*\n%s", titleText, item.Snippet)
		blocks = appendSectionTextBlocks(blocks, md)
		// context line with category/path if available
		ctxParts := []string{}
		if item.Category != "" {
			ctxParts = append(ctxParts, fmt.Sprintf("カテゴリ: %s", item.Category))
		}
		if item.FilePath != "" {
			ctxParts = append(ctxParts, fmt.Sprintf("パス: %s", item.FilePath))
		}
		if len(ctxParts) > 0 {
			blocks = append(blocks, newContextTextBlock(fmt.Sprintf("ctx-meta-%d", i), strings.Join(ctxParts, " · ")))
		}
	}
	if len(blocks) == 2 {
		blocks = append(blocks, slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, "該当する結果が見つかりませんでした。", false, false), nil, nil))
	}
	footer := newContextTextBlock("", fmt.Sprintf("%sにより生成", "RAGent"))
	blocks = appendFooterWithinBlockLimit(blocks, footer)
	return slack.MsgOptionBlocks(blocks...)
}

// BuildChatResponse formats LLM-generated response into blocks
func (f *Formatter) BuildChatResponse(query string, result *SearchResult) slack.MsgOption {
	response := slacksearch.EscapeSlackMentions(result.GeneratedResponse)
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
	intro := newContextTextBlock("", strings.Join(metaParts, " / "))

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
					blocks = appendSectionTextBlocks(blocks, normalized)
				}
				currentSection = ""
			}
			// Add header as divider
			blocks = append(blocks, slack.NewDividerBlock())
			blocks = appendSectionTextBlocks(blocks, "*"+strings.TrimPrefix(line, "## ")+"*")
		} else {
			// Build up section content
			if currentSection != "" {
				currentSection += "\n"
			}
			currentSection += line

			// If section gets too long, flush it (Slack has a 3000 char limit per text block)
			if len([]rune(currentSection)) > slackResponseSectionSoftLimit {
				normalized := normalizeMarkdownForSlack(currentSection)
				if strings.TrimSpace(normalized) != "" {
					blocks = appendSectionTextBlocks(blocks, normalized)
				}
				currentSection = ""
			}
		}
	}

	// Add remaining section
	if currentSection != "" {
		normalized := normalizeMarkdownForSlack(currentSection)
		if strings.TrimSpace(normalized) != "" {
			blocks = appendSectionTextBlocks(blocks, normalized)
		}
	}

	if result != nil && result.Slack != nil {
		blocks = append(blocks, buildSlackResultBlocks(result.Slack)...)
	}

	// Footer
	footer := newContextTextBlock("", fmt.Sprintf("%sにより生成", "RAGent"))
	blocks = appendFooterWithinBlockLimit(blocks, footer)

	return slack.MsgOptionBlocks(blocks...)
}

func appendSectionTextBlocks(blocks []slack.Block, text string) []slack.Block {
	for _, chunk := range splitTextByRuneLimit(text, slackSectionTextLimit) {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		blocks = append(blocks, slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, chunk, false, false), nil, nil))
	}
	return blocks
}

func newContextTextBlock(blockID string, text string) *slack.ContextBlock {
	return slack.NewContextBlock(blockID, slack.NewTextBlockObject(slack.MarkdownType, truncateTextByRuneLimit(text, slackContextTextLimit), false, false))
}

func truncateTextByRuneLimit(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	if limit == 1 {
		return "…"
	}
	return string(runes[:limit-1]) + "…"
}

func splitTextByRuneLimit(text string, limit int) []string {
	if limit <= 0 {
		return []string{text}
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return []string{text}
	}

	chunks := make([]string, 0, (len(runes)+limit-1)/limit)
	for len(runes) > limit {
		chunks = append(chunks, string(runes[:limit]))
		runes = runes[limit:]
	}
	if len(runes) > 0 {
		chunks = append(chunks, string(runes))
	}
	return chunks
}

func appendFooterWithinBlockLimit(blocks []slack.Block, footer slack.Block) []slack.Block {
	if len(blocks) < slackMessageBlockLimit {
		return append(blocks, footer)
	}

	notice := newContextTextBlock("", "… 返信が長いため一部を省略しました。")
	keep := slackMessageBlockLimit - 2
	if keep > len(blocks) {
		keep = len(blocks)
	}
	limited := make([]slack.Block, 0, slackMessageBlockLimit)
	limited = append(limited, blocks[:keep]...)
	limited = append(limited, notice, footer)
	return limited
}

func limitBlocksWithinMessageLimit(blocks []slack.Block) []slack.Block {
	if len(blocks) <= slackMessageBlockLimit {
		return blocks
	}

	notice := newContextTextBlock("", "… 返信が長いため一部を省略しました。")
	keep := slackMessageBlockLimit - 1
	limited := make([]slack.Block, 0, slackMessageBlockLimit)
	limited = append(limited, blocks[:keep]...)
	limited = append(limited, notice)
	return limited
}

// BuildError renders an error message in a block
func (f *Formatter) BuildError(message string) slack.MsgOption {
	blocks := appendSectionTextBlocks(nil, ":warning: エラーが発生しました")
	blocks = appendSectionTextBlocks(blocks, message)
	blocks = limitBlocksWithinMessageLimit(blocks)
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

func normalizeMarkdownForSlack(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return text
	}

	normalized := make([]string, 0, len(lines))
	insideCode := false

	for _, line := range lines {
		trimmedLeft := strings.TrimLeft(line, " 	")

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
	blocks = append(blocks, newContextTextBlock("slack-meta", meta))

	displayCount := 0
	truncated := false
	totalMessages := len(result.Messages)
	for i, msg := range result.Messages {
		if displayCount >= maxSlackMessages {
			truncated = true
			break
		}

		channel := slacksearch.FormatSlackChannel(msg.Channel)
		stamp := slacksearch.FormatSlackTimestamp(msg.Timestamp)
		user := slacksearch.FormatSlackUser(msg.User, msg.Username)
		header := fmt.Sprintf("*#%s* • %s • %s", channel, stamp, user)
		blocks = append(blocks, newContextTextBlock(fmt.Sprintf("slack-msg-meta-%d", i), header))

		body := slacksearch.EscapeSlackMentions(strings.TrimSpace(msg.Text))
		if body == "" {
			body = "(本文なし)"
		}

		blockBudget := maxSlackBlocks - len(blocks)
		minBlocks := 2 // meta context + action/section block
		if len(msg.Thread) > 0 {
			minBlocks++
		}
		if blockBudget < minBlocks {
			truncated = true
			break
		}

		text := slack.NewTextBlockObject(slack.MarkdownType, truncateSlackText(body, 600), false, false)
		section := slack.NewSectionBlock(text, nil, nil)
		if msg.Permalink != "" {
			btn := slack.NewButtonBlockElement(fmt.Sprintf("slack-msg-%d", i), "view", slack.NewTextBlockObject(slack.PlainTextType, "Open in Slack", false, false))
			btn.URL = msg.Permalink
			section.Accessory = slack.NewAccessory(btn)
		} else {
			text.Text = fmt.Sprintf("%s\n_(リンクを表示できませんでした)_", text.Text)
		}
		blocks = append(blocks, section)

		if len(msg.Thread) > 0 {
			threadNote := fmt.Sprintf("スレッドの回答: %d （未展開）", len(msg.Thread))
			blocks = append(blocks, newContextTextBlock(fmt.Sprintf("slack-thread-%d", i), threadNote))
		}

		displayCount++
	}

	if truncated {
		remaining := totalMessages - displayCount
		if remaining > 0 && len(blocks) < maxSlackBlocks {
			notice := fmt.Sprintf("… Slackメッセージが多いため %d 件のみ表示しています (残り %d 件)", displayCount, remaining)
			blocks = append(blocks, newContextTextBlock("slack-truncated", notice))
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
