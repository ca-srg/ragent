package embedding

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ca-srg/ragent/internal/pkg/config"
	"github.com/ca-srg/ragent/internal/pkg/embedding/bedrock"
)

func TestNewEmbeddingClientDefault(t *testing.T) {
	client, err := NewEmbeddingClient(&config.Config{
		BedrockRegion: "us-east-1",
	})

	require.NoError(t, err)
	assert.IsType(t, &bedrock.BedrockClient{}, client)
}

func TestNewEmbeddingClientBedrock(t *testing.T) {
	client, err := NewEmbeddingClient(&config.Config{
		EmbeddingProvider: "bedrock",
		BedrockRegion:     "us-east-1",
	})

	require.NoError(t, err)
	assert.IsType(t, &bedrock.BedrockClient{}, client)
}

func TestNewEmbeddingClientInvalidProvider(t *testing.T) {
	client, err := NewEmbeddingClient(&config.Config{
		EmbeddingProvider: "invalid",
	})

	require.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "unsupported embedding provider")
	assert.Contains(t, err.Error(), `"invalid"`)
}

func TestNewEmbeddingClientGeminiMissingCredentials(t *testing.T) {
	client, err := NewEmbeddingClient(&config.Config{
		EmbeddingProvider: "gemini",
	})

	require.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "gemini credentials not configured")
}
