package voyage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// VoyageClient implements the EmbeddingClient interface for Voyage AI
type VoyageClient struct {
	apiKey     string
	apiURL     string
	model      string
	httpClient *http.Client
}

// VoyageRequest represents the request structure for Voyage AI API
type VoyageRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

// VoyageResponse represents the response structure from Voyage AI API
type VoyageResponse struct {
	Object string `json:"object"`
	Data   []struct {
		Object    string    `json:"object"`
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

// VoyageErrorResponse represents error response from Voyage AI API
type VoyageErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// NewVoyageClient creates a new Voyage AI client
func NewVoyageClient(apiKey, apiURL, model string) *VoyageClient {
	return &VoyageClient{
		apiKey: apiKey,
		apiURL: apiURL,
		model:  model,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GenerateEmbedding creates an embedding vector from the given text
func (c *VoyageClient) GenerateEmbedding(ctx context.Context, text string) ([]float64, error) {
	if text == "" {
		return nil, fmt.Errorf("text cannot be empty")
	}

	// Prepare request
	request := VoyageRequest{
		Input: []string{text},
		Model: c.model,
	}

	requestBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", c.apiURL, bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	// Make request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Handle error responses
	if resp.StatusCode != http.StatusOK {
		var errorResp VoyageErrorResponse
		if err := json.Unmarshal(body, &errorResp); err == nil {
			return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, errorResp.Error.Message)
		}
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(body))
	}

	// Parse successful response
	var voyageResp VoyageResponse
	if err := json.Unmarshal(body, &voyageResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Validate response
	if len(voyageResp.Data) == 0 {
		return nil, fmt.Errorf("no embedding data in response")
	}

	return voyageResp.Data[0].Embedding, nil
}

// ValidateConnection checks if the embedding service is accessible
func (c *VoyageClient) ValidateConnection(ctx context.Context) error {
	// Test with a simple text
	testText := "test connection"

	_, err := c.GenerateEmbedding(ctx, testText)
	if err != nil {
		return fmt.Errorf("connection validation failed: %w", err)
	}

	return nil
}

// GetModelInfo returns information about the embedding model being used
func (c *VoyageClient) GetModelInfo() (string, int, error) {
	// Voyage-3-large has 1024 dimensions
	// This could be made configurable or fetched from API in the future
	dimensions := map[string]int{
		"voyage-3-large": 1024,
		"voyage-3":       1024,
		"voyage-2":       1024,
		"voyage-large-2": 1536,
		"voyage-code-2":  1536,
	}

	dim, exists := dimensions[c.model]
	if !exists {
		// Default dimension for unknown models
		dim = 1024
	}

	return c.model, dim, nil
}

// GenerateEmbeddings creates embedding vectors for multiple texts (batch processing)
func (c *VoyageClient) GenerateEmbeddings(ctx context.Context, texts []string) ([][]float64, error) {
	if len(texts) == 0 {
		return nil, fmt.Errorf("texts cannot be empty")
	}

	// Prepare request
	request := VoyageRequest{
		Input: texts,
		Model: c.model,
	}

	requestBody, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", c.apiURL, bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	// Make request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Handle error responses
	if resp.StatusCode != http.StatusOK {
		var errorResp VoyageErrorResponse
		if err := json.Unmarshal(body, &errorResp); err == nil {
			return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, errorResp.Error.Message)
		}
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(body))
	}

	// Parse successful response
	var voyageResp VoyageResponse
	if err := json.Unmarshal(body, &voyageResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Validate response
	if len(voyageResp.Data) != len(texts) {
		return nil, fmt.Errorf("response data count mismatch: expected %d, got %d", len(texts), len(voyageResp.Data))
	}

	// Extract embeddings in correct order
	embeddings := make([][]float64, len(texts))
	for _, data := range voyageResp.Data {
		if data.Index >= 0 && data.Index < len(embeddings) {
			embeddings[data.Index] = data.Embedding
		}
	}

	return embeddings, nil
}

// SetTimeout sets the HTTP client timeout
func (c *VoyageClient) SetTimeout(timeout time.Duration) {
	c.httpClient.Timeout = timeout
}

// GetUsageInfo returns usage information from the last request (if available)
func (c *VoyageClient) GetUsageInfo(ctx context.Context, text string) (int, error) {
	// This could be implemented to return token usage information
	// For now, we'll estimate based on text length
	// Rough estimation: 1 token ≈ 4 characters
	return len(text) / 4, nil
}
