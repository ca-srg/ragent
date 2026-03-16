package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ca-srg/ragent/internal/pkg/slacksearch"
)

func TestSanitizeSlackChannelsNilInput(t *testing.T) {
	require.Nil(t, sanitizeSlackChannels(nil))
}

func TestSanitizeSlackChannelsEmptySlice(t *testing.T) {
	require.Nil(t, sanitizeSlackChannels([]string{}))
}

func TestPrintSlackResultsNilResult(t *testing.T) {
	output := captureOutput(t, func() {
		printSlackResults(nil)
	})

	assert.Contains(t, output, "=== Slack Conversations ===")
	assert.Contains(t, output, "(no Slack messages found)")
}

func TestPrintSlackResultsEmptyMessages(t *testing.T) {
	result := &slacksearch.SlackSearchResult{}

	output := captureOutput(t, func() {
		printSlackResults(result)
	})

	assert.Contains(t, output, "=== Slack Conversations ===")
	assert.Contains(t, output, "(no Slack messages found)")
}
