package gemini

import (
	"context"
	"fmt"

	"google.golang.org/genai"
)

const defaultGeminiEmbeddingModel = "text-embedding-004"

var defaultDimensions = map[string]int{
	"text-embedding-004":         768,
	"text-embedding-005":         768,
	"gemini-embedding-001":       768,
	"gemini-embedding-2-preview": 768,
}

type GeminiEmbeddingClient struct {
	genaiClient *genai.Client
	model       string
	dimension   int
}

func NewGeminiEmbeddingClient(apiKey, gcpProject, gcpLocation, model string, dimension int) (*GeminiEmbeddingClient, error) {
	if model == "" {
		model = defaultGeminiEmbeddingModel
	}

	if apiKey == "" && gcpProject == "" && gcpLocation == "" {
		return nil, fmt.Errorf("gemini credentials not configured: provide an API key or GCP project/location for Vertex AI")
	}

	var clientConfig *genai.ClientConfig
	if apiKey != "" {
		clientConfig = &genai.ClientConfig{
			APIKey:  apiKey,
			Backend: genai.BackendGeminiAPI,
		}
	} else {
		clientConfig = &genai.ClientConfig{
			Backend:  genai.BackendVertexAI,
			Project:  gcpProject,
			Location: gcpLocation,
		}
	}

	client, err := genai.NewClient(context.Background(), clientConfig)
	if err != nil {
		if apiKey == "" {
			return nil, fmt.Errorf("failed to create Gemini embedding client with ADC (Vertex AI): %w", err)
		}
		return nil, fmt.Errorf("failed to create Gemini embedding client: %w", err)
	}

	if dimension <= 0 {
		dimension = modelDimension(model)
	}

	return &GeminiEmbeddingClient{
		genaiClient: client,
		model:       model,
		dimension:   dimension,
	}, nil
}

func (c *GeminiEmbeddingClient) GenerateEmbedding(ctx context.Context, text string) ([]float64, error) {
	if c == nil || c.genaiClient == nil {
		return nil, fmt.Errorf("gemini client is not initialized")
	}
	if text == "" {
		return nil, fmt.Errorf("text cannot be empty")
	}

	config := &genai.EmbedContentConfig{
		TaskType: "RETRIEVAL_DOCUMENT",
	}
	if c.dimension > 0 {
		dim := int32(c.dimension)
		config.OutputDimensionality = &dim
	}

	resp, err := c.genaiClient.Models.EmbedContent(ctx, c.model, genai.Text(text), config)
	if err != nil {
		return nil, fmt.Errorf("failed to generate Gemini embedding: %w", err)
	}
	if resp == nil || len(resp.Embeddings) == 0 || len(resp.Embeddings[0].Values) == 0 {
		return nil, fmt.Errorf("gemini returned no embedding values")
	}

	return float32SliceToFloat64(resp.Embeddings[0].Values), nil
}

func (c *GeminiEmbeddingClient) ValidateConnection(ctx context.Context) error {
	_, err := c.GenerateEmbedding(ctx, "test connection")
	if err != nil {
		return fmt.Errorf("gemini connection validation failed: %w", err)
	}
	return nil
}

func (c *GeminiEmbeddingClient) GetModelInfo() (string, int, error) {
	if c == nil {
		return "", 0, fmt.Errorf("gemini client is not initialized")
	}

	dimension := c.dimension
	if dimension <= 0 {
		dimension = modelDimension(c.model)
	}

	return c.model, dimension, nil
}

func modelDimension(model string) int {
	if dim, ok := defaultDimensions[model]; ok {
		return dim
	}
	return 0
}

func float32SliceToFloat64(values []float32) []float64 {
	converted := make([]float64, len(values))
	for i, value := range values {
		converted[i] = float64(value)
	}
	return converted
}
