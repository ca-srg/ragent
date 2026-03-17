package slacksearch

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectSlackSearchDirective_Disable(t *testing.T) {
	tests := []struct {
		name           string
		query          string
		wantDirective  SlackSearchDirective
		wantCleaned    string
		cleanedContain string
	}{
		{
			name:          "Slack検索を利用せず",
			query:         "Slack検索を利用せず、長谷川の自己紹介を教えて",
			wantDirective: SlackSearchExplicitDisable,
			wantCleaned:   "長谷川の自己紹介を教えて",
		},
		{
			name:          "Slack 検索を利用せず with space",
			query:         "Slack 検索を利用せず、長谷川の自己紹介を教えて",
			wantDirective: SlackSearchExplicitDisable,
			wantCleaned:   "長谷川の自己紹介を教えて",
		},
		{
			name:          "Slack検索を使用しないで",
			query:         "Slack検索を使用しないで、最新のデプロイ手順を教えて",
			wantDirective: SlackSearchExplicitDisable,
			wantCleaned:   "最新のデプロイ手順を教えて",
		},
		{
			name:          "Slack検索なしで",
			query:         "Slack検索なしで回答して",
			wantDirective: SlackSearchExplicitDisable,
			wantCleaned:   "回答して",
		},
		{
			name:          "Slackは使わず",
			query:         "Slackは使わず回答して",
			wantDirective: SlackSearchExplicitDisable,
			wantCleaned:   "回答して",
		},
		{
			name:          "Slack検索をスキップして",
			query:         "Slack検索をスキップして、ドキュメントだけ検索して",
			wantDirective: SlackSearchExplicitDisable,
			wantCleaned:   "ドキュメントだけ検索して",
		},
		{
			name:          "without slack search",
			query:         "without slack search, tell me about deployment",
			wantDirective: SlackSearchExplicitDisable,
			wantCleaned:   "tell me about deployment",
		},
		{
			name:          "don't use slack",
			query:         "don't use slack, find the runbook",
			wantDirective: SlackSearchExplicitDisable,
			wantCleaned:   "find the runbook",
		},
		{
			name:          "skip slack search",
			query:         "skip slack search and find the document",
			wantDirective: SlackSearchExplicitDisable,
			wantCleaned:   "and find the document",
		},
		{
			name:          "disable slack",
			query:         "disable slack, search for monitoring docs",
			wantDirective: SlackSearchExplicitDisable,
			wantCleaned:   "search for monitoring docs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectSlackSearchDirective(tt.query)
			assert.Equal(t, tt.wantDirective, result.Directive)
			if tt.wantCleaned != "" {
				assert.Equal(t, tt.wantCleaned, result.CleanedQuery)
			}
		})
	}
}

func TestDetectSlackSearchDirective_Enable(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		wantDirective SlackSearchDirective
		wantCleaned   string
	}{
		{
			name:          "Slack検索を利用して",
			query:         "Slack検索を利用して、最近の議論を教えて",
			wantDirective: SlackSearchExplicitEnable,
			wantCleaned:   "最近の議論を教えて",
		},
		{
			name:          "Slackも検索して",
			query:         "Slackも検索して、障害対応の情報を集めて",
			wantDirective: SlackSearchExplicitEnable,
			wantCleaned:   "障害対応の情報を集めて",
		},
		{
			name:          "Slack検索ありで",
			query:         "Slack検索ありで回答して",
			wantDirective: SlackSearchExplicitEnable,
			wantCleaned:   "回答して",
		},
		{
			name:          "with slack search",
			query:         "with slack search, find recent discussions",
			wantDirective: SlackSearchExplicitEnable,
			wantCleaned:   "find recent discussions",
		},
		{
			name:          "include slack",
			query:         "include slack in the results for incident timeline",
			wantDirective: SlackSearchExplicitEnable,
			wantCleaned:   "in the results for incident timeline",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectSlackSearchDirective(tt.query)
			assert.Equal(t, tt.wantDirective, result.Directive)
			if tt.wantCleaned != "" {
				assert.Equal(t, tt.wantCleaned, result.CleanedQuery)
			}
		})
	}
}

func TestDetectSlackSearchDirective_Unspecified(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{name: "empty", query: ""},
		{name: "no slack mention", query: "長谷川の自己紹介を教えて"},
		{name: "slack as topic", query: "Slackの使い方を教えて"},
		{name: "general question", query: "how to deploy the application"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectSlackSearchDirective(tt.query)
			assert.Equal(t, SlackSearchUnspecified, result.Directive)
			assert.Equal(t, tt.query, result.CleanedQuery)
		})
	}
}

func TestDetectSlackSearchDirective_DisableTakesPrecedence(t *testing.T) {
	result := DetectSlackSearchDirective("Slack検索を利用せず、Slackの使い方を教えて")
	require.Equal(t, SlackSearchExplicitDisable, result.Directive)
}

func TestNormalizeFullWidth(t *testing.T) {
	assert.Equal(t, "Slack", normalizeFullWidth("Ｓｌａｃｋ"))
	assert.Equal(t, "test abc", normalizeFullWidth("test\u3000abc"))
	assert.Equal(t, "normal", normalizeFullWidth("normal"))
}
