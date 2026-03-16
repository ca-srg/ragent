package slacksearch

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSanitizeSlackChannels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{name: "nil input returns nil", input: nil, expected: nil},
		{name: "empty input returns nil", input: []string{}, expected: nil},
		{name: "strips whitespace and leading hash", input: []string{"#general", " random ", "", "##ops"}, expected: []string{"general", "random", "#ops"}},
		{name: "all empty values return nil", input: []string{"", "   ", "#"}, expected: nil},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, SanitizeSlackChannels(tt.input))
		})
	}
}

func TestNormalizeSlackChannel(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "general", NormalizeSlackChannel(" #General "))
	assert.Equal(t, "#ops", NormalizeSlackChannel("##Ops"))
	assert.Equal(t, "", NormalizeSlackChannel("   "))
}

func TestNormalizeSlackChannels(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"general", "random", "#ops"}, NormalizeSlackChannels([]string{"#General", " random ", "", "##Ops"}))
	assert.Empty(t, NormalizeSlackChannels(nil))
	assert.Empty(t, NormalizeSlackChannels([]string{"", "   ", "#"}))
}

func TestPrintSlackResults(t *testing.T) {
	result := &SlackSearchResult{
		IterationCount: 2,
		TotalMatches:   1,
		Queries:        []string{"initial"},
		EnrichedMessages: []EnrichedMessage{
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

	output := captureStdout(t, func() {
		PrintSlackResults(result)
	})

	assert.Contains(t, output, "=== Slack Conversations ===")
	assert.Contains(t, output, "Permalink: https://example.com/thread")
	assert.Contains(t, output, "Iterations: 2")
	assert.Contains(t, output, "Thread replies (1):")
}

func TestPrintSlackResultsNilResult(t *testing.T) {
	output := captureStdout(t, func() {
		PrintSlackResults(nil)
	})

	assert.Contains(t, output, "=== Slack Conversations ===")
	assert.Contains(t, output, "(no Slack messages found)")
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	originalStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	defer func() {
		os.Stdout = originalStdout
	}()

	fn()
	require.NoError(t, w.Close())

	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	require.NoError(t, err)
	require.NoError(t, r.Close())

	return buf.String()
}
