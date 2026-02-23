package bedrock

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupports1MContext(t *testing.T) {
	cases := []struct {
		name    string
		modelID string
		want    bool
	}{
		{
			name:    "sonnet 4.5 with global prefix",
			modelID: "global.anthropic.claude-sonnet-4-5-20250929-v1:0",
			want:    true,
		},
		{
			name:    "opus 4.5 with us prefix",
			modelID: "us.anthropic.claude-opus-4-5-20251101-v1:0",
			want:    true,
		},
		{
			name:    "opus 4.6",
			modelID: "anthropic.claude-opus-4-6-v1",
			want:    true,
		},
		{
			name:    "sonnet 4.7 with eu prefix",
			modelID: "eu.anthropic.claude-sonnet-4-7-20260101-v1:0",
			want:    true,
		},
		{
			name:    "sonnet 4.5 with ap prefix",
			modelID: "ap.anthropic.claude-sonnet-4-5-20260201-v2:0",
			want:    true,
		},
		{
			name:    "sonnet 4.0 not supported",
			modelID: "anthropic.claude-sonnet-4-20250514-v1:0",
			want:    false,
		},
		{
			name:    "claude 3.5 sonnet not supported",
			modelID: "anthropic.claude-3-5-sonnet-20240620-v1:0",
			want:    false,
		},
		{
			name:    "titan embed not claude",
			modelID: "amazon.titan-embed-text-v2:0",
			want:    false,
		},
		{
			name:    "empty string",
			modelID: "",
			want:    false,
		},
		{
			name:    "haiku 4.5 excluded",
			modelID: "anthropic.claude-haiku-4-5-20251001-v1:0",
			want:    false,
		},
		{
			name:    "non-claude model",
			modelID: "some-deepseek-model",
			want:    false,
		},
		{
			name:    "opus 4.0 not supported",
			modelID: "anthropic.claude-opus-4-0-v1",
			want:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Supports1MContext(tc.modelID)
			assert.Equal(t, tc.want, got, "modelID=%q", tc.modelID)
		})
	}
}

func TestChatRequestMarshalWithBeta(t *testing.T) {
	req := ChatRequest{
		AnthropicBeta: []string{"context-1m-2025-08-07"},
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)

	jsonStr := string(data)
	assert.True(t, strings.Contains(jsonStr, `"anthropic_beta":["context-1m-2025-08-07"]`),
		"expected anthropic_beta field in JSON, got: %s", jsonStr)
}

func TestChatRequestMarshalWithoutBeta(t *testing.T) {
	req := ChatRequest{}
	data, err := json.Marshal(req)
	require.NoError(t, err)

	jsonStr := string(data)
	assert.False(t, strings.Contains(jsonStr, `"anthropic_beta"`),
		"expected anthropic_beta to be omitted (omitempty), got: %s", jsonStr)
}
