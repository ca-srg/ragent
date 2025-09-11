package slackbot

import (
	"fmt"
	"strings"
	"time"

	"github.com/slack-go/slack"
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
		return f.BuildChatResponse(query, result.GeneratedResponse, result.Total, result.Elapsed)
	}

	// Otherwise, use the traditional search result format
	header := slack.NewHeaderBlock(slack.NewTextBlockObject(slack.PlainTextType, fmt.Sprintf("検索結果: %s", truncate(query, 60)), false, false))
	intro := slack.NewContextBlock("", slack.NewTextBlockObject(slack.MarkdownType, fmt.Sprintf("%d 件 / %.0f ms", result.Total, result.Elapsed.Seconds()*1000), false, false))

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
func (f *Formatter) BuildChatResponse(query string, response string, total int, elapsed time.Duration) slack.MsgOption {
	// Header with query
	header := slack.NewHeaderBlock(slack.NewTextBlockObject(slack.PlainTextType, fmt.Sprintf("質問: %s", truncate(query, 60)), false, false))

	// Context info (number of documents found and time taken)
	intro := slack.NewContextBlock("", slack.NewTextBlockObject(slack.MarkdownType, fmt.Sprintf("%d 件の参考文献から生成 / %.0f ms", total, elapsed.Seconds()*1000), false, false))

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
				blocks = append(blocks, slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, currentSection, false, false), nil, nil))
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
				blocks = append(blocks, slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, currentSection, false, false), nil, nil))
				currentSection = ""
			}
		}
	}

	// Add remaining section
	if currentSection != "" {
		blocks = append(blocks, slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, currentSection, false, false), nil, nil))
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

// SearchResult is a simplified structure for Slack formatting
type SearchResult struct {
	Items             []SearchItem
	Total             int
	Elapsed           time.Duration
	GeneratedResponse string // LLM generated response (for chat-style responses)
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
