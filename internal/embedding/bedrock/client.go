package bedrock

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

// BedrockClient implements the EmbeddingClient interface for AWS Bedrock
type BedrockClient struct {
	client  *bedrockruntime.Client
	modelID string
	region  string
}

// TitanEmbeddingRequest represents the request structure for Titan embedding models
type TitanEmbeddingRequest struct {
	InputText   string                `json:"inputText"`
	Dimensions  int                   `json:"dimensions,omitempty"`
	Normalize   bool                  `json:"normalize,omitempty"`
	EmbedConfig *TitanEmbeddingConfig `json:"embeddingConfig,omitempty"`
}

// TitanEmbeddingConfig contains additional configuration for embedding
type TitanEmbeddingConfig struct {
	OutputEmbeddingLength int `json:"outputEmbeddingLength,omitempty"`
}

// TitanEmbeddingResponse represents the response structure from Titan embedding models
type TitanEmbeddingResponse struct {
	Embedding           []float64 `json:"embedding"`
	InputTextTokenCount int       `json:"inputTextTokenCount"`
}

// ChatMessage represents a chat message with role and content
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest represents the request payload for chat models like DeepSeek R1
type ChatRequest struct {
	Messages         []ChatMessage `json:"messages"`
	MaxTokens        int           `json:"max_tokens,omitempty"`
	Temperature      float64       `json:"temperature,omitempty"`
	StopSequences    []string      `json:"stop_sequences,omitempty"`
	AnthropicVersion string        `json:"anthropic_version,omitempty"`
	System           string        `json:"system,omitempty"`
}

// ChatResponse represents the response from chat models
type ChatResponse struct {
	Content []ChatContent `json:"content"`
	Usage   ChatUsage     `json:"usage,omitempty"`
}

// ChatContent represents the content in chat response
type ChatContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ChatUsage represents token usage information
type ChatUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// NewBedrockClient creates a new AWS Bedrock client for embeddings
func NewBedrockClient(awsConfig aws.Config, modelID string) *BedrockClient {
	client := bedrockruntime.NewFromConfig(awsConfig)

	// Default to Titan v2 model if not specified
	if modelID == "" {
		modelID = "amazon.titan-embed-text-v2:0"
	}

	return &BedrockClient{
		client:  client,
		modelID: modelID,
		region:  awsConfig.Region,
	}
}

// GenerateEmbedding creates an embedding vector from the given text using AWS Bedrock
func (c *BedrockClient) GenerateEmbedding(ctx context.Context, text string) ([]float64, error) {
	if text == "" {
		return nil, fmt.Errorf("text cannot be empty")
	}

	// Log text length for debugging
	log.Printf("Generating embedding for text with length: %d characters", len(text))

	// Prepare request payload for Titan v2
	request := TitanEmbeddingRequest{
		InputText:  text,
		Dimensions: 1024, // Titan v2 default dimension
		Normalize:  true,
	}

	requestBody, err := json.Marshal(request)
	if err != nil {
		log.Printf("ERROR: Failed to marshal request: %v", err)
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Invoke model
	input := &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(c.modelID),
		ContentType: aws.String("application/json"),
		Accept:      aws.String("application/json"),
		Body:        requestBody,
	}

	log.Printf("Invoking Bedrock model: %s", c.modelID)
	result, err := c.client.InvokeModel(ctx, input)
	if err != nil {
		log.Printf("ERROR: Failed to invoke bedrock model: %v", err)
		return nil, fmt.Errorf("failed to invoke bedrock model: %w", err)
	}

	// Parse response
	var response TitanEmbeddingResponse
	if err := json.Unmarshal(result.Body, &response); err != nil {
		log.Printf("ERROR: Failed to parse response: %v, Body: %s", err, string(result.Body))
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Validate response
	if len(response.Embedding) == 0 {
		log.Printf("ERROR: No embedding data in response, token count: %d", response.InputTextTokenCount)
		return nil, fmt.Errorf("no embedding data in response")
	}

	log.Printf("Successfully generated embedding with %d dimensions, token count: %d",
		len(response.Embedding), response.InputTextTokenCount)

	return response.Embedding, nil
}

// GenerateChatResponse generates a chat response using the configured chat model
func (c *BedrockClient) GenerateChatResponse(ctx context.Context, messages []ChatMessage) (string, error) {
	if len(messages) == 0 {
		return "", fmt.Errorf("messages cannot be empty")
	}

	var systemPrompts []string
	sanitized := make([]ChatMessage, 0, len(messages))
	for _, msg := range messages {
		switch strings.ToLower(msg.Role) {
		case "system":
			systemPrompts = append(systemPrompts, msg.Content)
		case "user", "assistant":
			sanitized = append(sanitized, msg)
		default:
			sanitized = append(sanitized, ChatMessage{Role: msg.Role, Content: msg.Content})
		}
	}
	if len(sanitized) == 0 {
		return "", fmt.Errorf("chat messages must include at least one user or assistant message")
	}

	// Prepare request payload for Claude models in AWS Bedrock format
	request := ChatRequest{
		Messages:         sanitized,
		MaxTokens:        4000,
		Temperature:      0.7,
		AnthropicVersion: "bedrock-2023-05-31",
	}
	if len(systemPrompts) > 0 {
		request.System = strings.Join(systemPrompts, "\n\n")
	}

	requestBody, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Invoke model
	input := &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(c.modelID),
		ContentType: aws.String("application/json"),
		Accept:      aws.String("application/json"),
		Body:        requestBody,
	}

	result, err := c.client.InvokeModel(ctx, input)
	if err != nil {
		return "", fmt.Errorf("failed to invoke bedrock model: %w", err)
	}

	// Parse response
	var response ChatResponse
	if err := json.Unmarshal(result.Body, &response); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	// Validate response
	if len(response.Content) == 0 {
		return "", fmt.Errorf("no content in response")
	}

	return response.Content[0].Text, nil
}

// ValidateConnection checks if the Bedrock service is accessible
func (c *BedrockClient) ValidateConnection(ctx context.Context) error {
	// Check if this is a chat model (Claude) or embedding model (Titan)
	if strings.Contains(strings.ToLower(c.modelID), "claude") {
		// For Claude models, test with a simple chat request
		testMessages := []ChatMessage{
			{
				Role:    "user",
				Content: "Hello",
			},
		}

		_, err := c.GenerateChatResponse(ctx, testMessages)
		if err != nil {
			return fmt.Errorf("connection validation failed: %w", err)
		}
	} else {
		// For embedding models (like Titan), test with embedding generation
		testText := "test connection"

		_, err := c.GenerateEmbedding(ctx, testText)
		if err != nil {
			return fmt.Errorf("connection validation failed: %w", err)
		}
	}

	return nil
}

// GetModelInfo returns information about the embedding model being used
func (c *BedrockClient) GetModelInfo() (string, int, error) {
	// Titan v2 model dimensions based on model ID
	dimensions := map[string]int{
		"amazon.titan-embed-text-v2:0": 1024, // Titan v2 default dimension
		"amazon.titan-embed-text-v1":   1536,
	}

	dim, exists := dimensions[c.modelID]
	if !exists {
		// Default to Titan v2 dimensions for unknown models
		dim = 1024
	}

	return c.modelID, dim, nil
}

// GetUsageInfo returns usage information (token count) for the given text
func (c *BedrockClient) GetUsageInfo(ctx context.Context, text string) (int, error) {
	// For Bedrock, we can get actual token count from the response
	// This is a simplified implementation - in practice, you might want to cache this
	// or make it part of the GenerateEmbedding response

	// Rough estimation: 1 token ≈ 4 characters for Titan models
	return len(text) / 4, nil
}

// GetRegion returns the AWS region being used
func (c *BedrockClient) GetRegion() string {
	return c.region
}

// SetModelID allows changing the model ID after client creation
func (c *BedrockClient) SetModelID(modelID string) {
	c.modelID = modelID
}

// ListAvailableModels returns a list of available Titan embedding models
func (c *BedrockClient) ListAvailableModels() []string {
	return []string{
		"amazon.titan-embed-text-v2:0",
		"amazon.titan-embed-text-v1",
	}
}

// GetModelCapabilities returns capabilities of the current model
func (c *BedrockClient) GetModelCapabilities() map[string]interface{} {
	capabilities := map[string]interface{}{
		"model_id":            c.modelID,
		"max_input_tokens":    8000,  // Titan v2 limit
		"output_dimensions":   1024,  // Titan v2 default dimension
		"supports_batch":      false, // Titan doesn't support batch processing in single call
		"supports_normalize":  true,
		"supports_dimensions": true,
	}

	if c.modelID == "amazon.titan-embed-text-v1" {
		capabilities["output_dimensions"] = 1536
		capabilities["max_input_tokens"] = 2048
	}

	return capabilities
}
