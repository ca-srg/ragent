package gemini

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type embeddingClientIface interface {
	GenerateEmbedding(ctx context.Context, text string) ([]float64, error)
	ValidateConnection(ctx context.Context) error
	GetModelInfo() (string, int, error)
}

func TestGeminiEmbeddingClientImplementsInterface(t *testing.T) {
	var _ embeddingClientIface = &GeminiEmbeddingClient{}
}

func TestNewGeminiEmbeddingClientValidation(t *testing.T) {
	client, err := NewGeminiEmbeddingClient("", "", "", "", 0)
	require.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "gemini credentials not configured")
}

func TestFloat32ToFloat64Conversion(t *testing.T) {
	values := []float32{0.1, -1.5, 3.1415927}

	converted := float32SliceToFloat64(values)

	require.Len(t, converted, len(values))
	for i, value := range values {
		assert.Equal(t, float64(value), converted[i])
	}
}

func TestGetModelInfo(t *testing.T) {
	client := &GeminiEmbeddingClient{
		model:     "custom-embedding-model",
		dimension: 1536,
	}

	model, dimension, err := client.GetModelInfo()

	require.NoError(t, err)
	assert.Equal(t, "custom-embedding-model", model)
	assert.Equal(t, 1536, dimension)
}

func TestGetModelInfoDefaultDimensions(t *testing.T) {
	for model, expectedDimension := range defaultDimensions {
		t.Run(model, func(t *testing.T) {
			client := &GeminiEmbeddingClient{model: model}

			gotModel, dimension, err := client.GetModelInfo()

			require.NoError(t, err)
			assert.Equal(t, model, gotModel)
			assert.Equal(t, expectedDimension, dimension)
		})
	}
}
