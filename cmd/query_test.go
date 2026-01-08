package cmd

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ca-srg/ragent/internal/opensearch"
	"github.com/ca-srg/ragent/internal/slacksearch"
	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueryOnlySlackFlag(t *testing.T) {
	ResetQueryState()
	t.Cleanup(ResetQueryState)

	require.NoError(t, queryCmd.Flags().Set("only-slack", "true"))
	assert.True(t, queryOnlySlack)
	require.NoError(t, queryCmd.Flags().Set("only-slack", "false"))
}

func TestSanitizeSlackChannels(t *testing.T) {
	channels := []string{"#general", " random ", "", "##ops"}
	clean := sanitizeSlackChannels(channels)
	require.Equal(t, []string{"general", "random", "#ops"}, clean)
}

func TestPrintSlackResultsIncludesPermalink(t *testing.T) {
	result := &slacksearch.SlackSearchResult{
		IterationCount: 2,
		TotalMatches:   1,
		Queries:        []string{"initial"},
		EnrichedMessages: []slacksearch.EnrichedMessage{
			{
				OriginalMessage: slack.Message{
					Msg: slack.Msg{
						Channel:   "C123",
						Timestamp: "1700000000.000",
						User:      "U123",
						Text:      "Important update",
					},
				},
				Permalink: "https://example.com/thread",
				ThreadMessages: []slack.Message{
					{Msg: slack.Msg{Timestamp: "1700000001.000", Text: "Follow-up"}},
				},
			},
		},
	}

	output := captureOutput(t, func() {
		printSlackResults(result)
	})

	assert.Contains(t, output, "=== Slack Conversations ===")
	assert.Contains(t, output, "Permalink: https://example.com/thread")
	assert.Contains(t, output, "Iterations: 2")
}

func TestOutputCombinedResultsWithoutSlack(t *testing.T) {
	ResetQueryState()
	restoreQuery := queryText
	queryText = "hybrid test"
	t.Cleanup(func() {
		queryText = restoreQuery
	})

	sourceBytes, err := json.Marshal(map[string]string{
		"title":     "Doc Title",
		"category":  "Guides",
		"file_path": "docs/example.md",
	})
	require.NoError(t, err)

	result := &opensearch.HybridSearchResult{
		ExecutionTime: time.Millisecond * 25,
		FusionResult: &opensearch.FusionResult{
			TotalHits:     1,
			FusionType:    "rrf",
			BM25Results:   1,
			VectorResults: 1,
			Documents: []opensearch.ScoredDoc{
				{
					ID:          "doc-1",
					FusedScore:  0.92,
					BM25Score:   0.80,
					VectorScore: 0.75,
					SearchType:  "fusion",
					Source:      sourceBytes,
				},
			},
		},
	}

	output := captureOutput(t, func() {
		require.NoError(t, outputCombinedResults(result, nil, "hybrid"))
	})

	assert.Contains(t, output, "Query: hybrid test")
	assert.NotContains(t, output, "Slack Conversations")
}
