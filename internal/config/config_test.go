package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadSlackVectorizeConfig(t *testing.T) {
	requiredEnv := map[string]string{
		"AWS_S3_VECTOR_BUCKET": "test-bucket",
		"AWS_S3_VECTOR_INDEX":  "test-index",
		"OPENSEARCH_ENDPOINT":  "https://opensearch.example.com",
		"OPENSEARCH_INDEX":     "vectors",
		"OPENSEARCH_REGION":    "us-east-1",
	}

	t.Run("parses slack vectorization overrides", func(t *testing.T) {
		for key, value := range requiredEnv {
			t.Setenv(key, value)
		}

		t.Setenv("SLACK_VECTORIZE_ENABLED", "true")
		t.Setenv("SLACK_VECTORIZE_CHANNELS", "C123 , C456 ,,")
		t.Setenv("SLACK_EXCLUDE_BOTS", "false")
		t.Setenv("SLACK_VECTORIZE_MIN_LENGTH", "25")

		cfg, err := Load()
		require.NoError(t, err)

		require.True(t, cfg.SlackVectorizeEnabled)
		require.Equal(t, []string{"C123", "C456"}, cfg.SlackVectorizeChannels)
		require.False(t, cfg.SlackExcludeBots)
		require.Equal(t, 25, cfg.SlackVectorizeMinLength)
	})

	t.Run("normalizes defaults when env not provided", func(t *testing.T) {
		for key, value := range requiredEnv {
			t.Setenv(key, value)
		}

		t.Setenv("SLACK_VECTORIZE_CHANNELS", "")
		t.Setenv("SLACK_VECTORIZE_MIN_LENGTH", "-5")

		cfg, err := Load()
		require.NoError(t, err)

		require.Empty(t, cfg.SlackVectorizeChannels)
		require.True(t, cfg.SlackExcludeBots, "default should keep bot exclusion enabled")
		require.Equal(t, 0, cfg.SlackVectorizeMinLength, "negative values should normalize to zero")
	})
}
