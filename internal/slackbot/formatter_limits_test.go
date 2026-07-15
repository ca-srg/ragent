package slackbot

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/slack-go/slack"
)

const (
	slackMaxMessageBlocks     = 50
	slackMaxSectionTextRunes  = 3000
	slackMaxContextTextRunes  = 3000
	slackMaxHeaderTextRunes   = 150
	longParagraphRuneCount    = 2600
	longParagraphRepeatCount  = 10
	slackConversationMessages = 15
)

func TestFormatterBuildErrorKeepsLongMessageWithinSlackBlockKitLimits(t *testing.T) {
	formatter := &Formatter{}
	blocks := blocksFromMsgOption(t, formatter.BuildError(strings.Repeat("エラー", 1200)))

	assertSlackMessageBlockCount(t, blocks)
	assertSlackBlockTextRuneLimits(t, blocks)
}

func TestFormatterBuildUsageKeepsLongTipWithinSlackBlockKitLimits(t *testing.T) {
	formatter := &Formatter{}
	blocks := blocksFromMsgOption(t, formatter.BuildUsage(strings.Repeat("使い方", 1200)))

	assertSlackMessageBlockCount(t, blocks)
	assertSlackBlockTextRuneLimits(t, blocks)
}

func TestFormatterBuildChatResponseSplitsSingleLongLineWithinSlackSectionTextLimit(t *testing.T) {
	blocks := buildChatResponseBlocks(t, &SearchResult{
		GeneratedResponse: strings.Repeat("あ", 3500),
		Total:             1,
		Elapsed:           10 * time.Millisecond,
	})

	assertSlackMessageBlockCount(t, blocks)
	assertSlackBlockTextRuneLimits(t, blocks)
}

func TestFormatterBuildChatResponseCapsBlocksWithLongAnswerAndSlackConversationResult(t *testing.T) {
	paragraphs := make([]string, longParagraphRepeatCount)
	for i := range paragraphs {
		paragraphs[i] = strings.Repeat("長", longParagraphRuneCount)
	}

	blocks := buildChatResponseBlocks(t, &SearchResult{
		GeneratedResponse: strings.Join(paragraphs, "\n"),
		Total:             slackConversationMessages,
		Elapsed:           25 * time.Millisecond,
		Slack:             buildSlackConversationResultForBlockLimitTest(),
	})

	assertSlackBlockTextRuneLimits(t, blocks)
	assertSlackMessageBlockCount(t, blocks)
}

func TestFormatterBuildChatResponseKeepsSlackMissingInfoContextWithinSlackLimit(t *testing.T) {
	blocks := buildChatResponseBlocks(t, &SearchResult{
		GeneratedResponse: "Slack検索結果を含む回答です。",
		Total:             1,
		Elapsed:           10 * time.Millisecond,
		Slack: &SlackConversationResult{
			IterationCount: 3,
			TotalMatches:   1,
			IsSufficient:   false,
			MissingInfo:    []string{strings.Repeat("不足情報", 800)},
			Messages: []SlackConversationMessage{{
				Channel:   "general",
				Timestamp: "1710000000.000000",
				User:      "U01",
				Username:  "user-01",
				Text:      "関連するSlackメッセージ",
				Permalink: "https://example.com/archives/C111/p1710000000000000",
			}},
		},
	})

	assertSlackBlockTextRuneLimits(t, blocks)
}

func TestFormatterBuildSearchResultCapsBlocksAndTextWithinSlackLimits(t *testing.T) {
	items := make([]SearchItem, 60)
	for i := range items {
		items[i] = SearchItem{
			Title:    fmt.Sprintf("検索結果 %02d", i+1),
			Snippet:  strings.Repeat("本文", 1800),
			Category: strings.Repeat("カテゴリ", 800),
			FilePath: "/docs/" + strings.Repeat("長いパス", 800),
			Link:     fmt.Sprintf("https://example.com/docs/%02d", i+1),
		}
	}

	blocks := buildSearchResultBlocks(t, &SearchResult{
		Items:        items,
		Total:        len(items),
		Elapsed:      25 * time.Millisecond,
		SearchMethod: "hybrid",
	})

	t.Run("block count", func(t *testing.T) {
		assertSlackMessageBlockCount(t, blocks)
	})
	t.Run("text rune limits", func(t *testing.T) {
		assertSlackBlockTextRuneLimits(t, blocks)
	})
}

func buildChatResponseBlocks(t *testing.T, result *SearchResult) slack.Blocks {
	t.Helper()

	formatter := &Formatter{}
	option := formatter.BuildChatResponse("Block Kit 制約テスト", result)
	return blocksFromMsgOption(t, option)
}

func buildSearchResultBlocks(t *testing.T, result *SearchResult) slack.Blocks {
	t.Helper()

	formatter := &Formatter{}
	option := formatter.BuildSearchResult("Block Kit constraint test", result)
	return blocksFromMsgOption(t, option)
}

func blocksFromMsgOption(t *testing.T, option slack.MsgOption) slack.Blocks {
	t.Helper()

	blocksJSON := blocksJSONFromMsgOption(t, option)

	var blocks slack.Blocks
	if err := json.Unmarshal([]byte(blocksJSON), &blocks); err != nil {
		t.Fatalf("failed to unmarshal blocks JSON: %v", err)
	}
	return blocks
}

func blocksJSONFromMsgOption(t *testing.T, option slack.MsgOption) string {
	t.Helper()

	_, values, err := slack.UnsafeApplyMsgOptions("test-token", "C111", "https://slack.com/api/", option)
	if err != nil {
		t.Fatalf("failed to apply Slack message options: %v", err)
	}

	if blocksJSON := values.Get("blocks"); blocksJSON != "" {
		return blocksJSON
	}
	return blocksJSONFromPostMessageRequest(t, option)
}

func blocksJSONFromPostMessageRequest(t *testing.T, option slack.MsgOption) string {
	t.Helper()

	blocksJSONCh := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			blocksJSONCh <- ""
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		blocksJSONCh <- r.Form.Get("blocks")
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"ok":true,"channel":"C111","ts":"1710000000.000000"}`)); err != nil {
			t.Errorf("failed to write Slack API response: %v", err)
		}
	}))
	defer server.Close()

	api := slack.New("test-token", slack.OptionAPIURL(server.URL+"/"))
	if _, _, err := api.PostMessage("C111", option); err != nil {
		t.Fatalf("failed to capture Slack message request: %v", err)
	}
	blocksJSON := <-blocksJSONCh
	if blocksJSON == "" {
		t.Fatalf("expected blocks JSON in Slack message options")
	}
	return blocksJSON
}

func assertSlackMessageBlockCount(t *testing.T, blocks slack.Blocks) {
	t.Helper()

	if got := len(blocks.BlockSet); got > slackMaxMessageBlocks {
		t.Fatalf("message has %d blocks, exceeds Slack Block Kit limit %d", got, slackMaxMessageBlocks)
	}
}

func assertSlackBlockTextRuneLimits(t *testing.T, blocks slack.Blocks) {
	t.Helper()

	for i, block := range blocks.BlockSet {
		switch b := block.(type) {
		case *slack.SectionBlock:
			if b.Text != nil {
				assertSlackTextRuneLimit(t, i, "section text", b.Text.Text, slackMaxSectionTextRunes)
			}
		case *slack.ContextBlock:
			for j, element := range b.ContextElements.Elements {
				text, ok := element.(*slack.TextBlockObject)
				if !ok {
					continue
				}
				assertSlackTextRuneLimit(t, i, fmt.Sprintf("context text %d", j), text.Text, slackMaxContextTextRunes)
			}
		case *slack.HeaderBlock:
			if b.Text != nil {
				assertSlackTextRuneLimit(t, i, "header text", b.Text.Text, slackMaxHeaderTextRunes)
			}
		}
	}
}

func assertSlackTextRuneLimit(t *testing.T, blockIndex int, label string, text string, limit int) {
	t.Helper()

	if got := utf8.RuneCountInString(text); got > limit {
		t.Fatalf("block %d %s has %d runes, exceeds Slack Block Kit limit %d", blockIndex, label, got, limit)
	}
}

func buildSlackConversationResultForBlockLimitTest() *SlackConversationResult {
	messages := make([]SlackConversationMessage, slackConversationMessages)
	for i := range messages {
		messages[i] = SlackConversationMessage{
			Channel:   "general",
			Timestamp: fmt.Sprintf("17100000%02d.000000", i),
			User:      fmt.Sprintf("U%02d", i),
			Username:  fmt.Sprintf("user-%02d", i),
			Text:      fmt.Sprintf("関連するSlackメッセージ %d", i+1),
			Permalink: fmt.Sprintf("https://example.com/archives/C111/p17100000%02d000000", i),
			Thread: []SlackThreadMessage{{
				Timestamp: fmt.Sprintf("17100001%02d.000000", i),
				User:      fmt.Sprintf("U%02d", i+1),
				Username:  fmt.Sprintf("thread-user-%02d", i),
				Text:      fmt.Sprintf("スレッド返信 %d", i+1),
			}},
		}
	}

	return &SlackConversationResult{
		IterationCount: 2,
		TotalMatches:   len(messages),
		IsSufficient:   true,
		Messages:       messages,
	}
}
