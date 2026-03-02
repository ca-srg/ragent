package opensearch

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	opensearch "github.com/opensearch-project/opensearch-go/v4"
	"github.com/opensearch-project/opensearch-go/v4/opensearchapi"
	requestsigner "github.com/opensearch-project/opensearch-go/v4/signer/awsv2"
	"golang.org/x/time/rate"
)

type Client struct {
	client      *opensearchapi.Client
	rateLimiter *rate.Limiter
	config      *Config
}

type Config struct {
	Endpoint          string
	Region            string
	InsecureSkipTLS   bool
	RateLimit         float64
	RateBurst         int
	ConnectionTimeout time.Duration
	RequestTimeout    time.Duration
	MaxRetries        int
	RetryDelay        time.Duration
	MaxConnections    int
	MaxIdleConns      int
	IdleConnTimeout   time.Duration
}

func NewClient(cfg *Config) (*Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("endpoint is required")
	}

	if cfg.Region == "" {
		return nil, fmt.Errorf("region is required")
	}

	if cfg.RateLimit <= 0 {
		cfg.RateLimit = 10.0
	}
	if cfg.RateBurst <= 0 {
		cfg.RateBurst = 20
	}
	if cfg.ConnectionTimeout == 0 {
		cfg.ConnectionTimeout = 30 * time.Second
	}
	if cfg.RequestTimeout == 0 {
		cfg.RequestTimeout = 60 * time.Second
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryDelay == 0 {
		cfg.RetryDelay = 1 * time.Second
	}
	if cfg.MaxConnections <= 0 {
		cfg.MaxConnections = 100
	}
	if cfg.MaxIdleConns <= 0 {
		cfg.MaxIdleConns = 10
	}
	if cfg.IdleConnTimeout == 0 {
		cfg.IdleConnTimeout = 90 * time.Second
	}

	awsConfig, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(cfg.Region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	signer, err := requestsigner.NewSignerWithService(awsConfig, "es")
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS signer: %w", err)
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: cfg.InsecureSkipTLS,
		},
		MaxConnsPerHost:       cfg.MaxConnections,
		MaxIdleConns:          cfg.MaxIdleConns,
		MaxIdleConnsPerHost:   cfg.MaxIdleConns / 2,
		IdleConnTimeout:       cfg.IdleConnTimeout,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: cfg.RequestTimeout,
	}

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   cfg.ConnectionTimeout,
	}

	osClient, err := opensearchapi.NewClient(opensearchapi.Config{
		Client: opensearch.Config{
			Addresses: []string{cfg.Endpoint},
			Signer:    signer,
			Transport: httpClient.Transport,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create OpenSearch client: %w", err)
	}

	rateLimiter := rate.NewLimiter(rate.Limit(cfg.RateLimit), cfg.RateBurst)

	return &Client{
		client:      osClient,
		rateLimiter: rateLimiter,
		config:      cfg,
	}, nil
}

func (c *Client) HealthCheck(ctx context.Context) error {
	if err := c.rateLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limit exceeded: %w", err)
	}

	resp, err := c.client.Cluster.Health(ctx, &opensearchapi.ClusterHealthReq{})
	if err != nil {
		log.Printf("OpenSearch health check failed: %v", err)
		return fmt.Errorf("health check failed: %w", err)
	}

	if resp != nil {
		log.Printf("OpenSearch health check successful")
	}
	return nil
}

func (c *Client) GetClient() *opensearchapi.Client {
	return c.client
}

func (c *Client) WaitForRateLimit(ctx context.Context) error {
	return c.rateLimiter.Wait(ctx)
}

// RetryableOperation defines a function that can be retried
type RetryableOperation func() error

// ExecuteWithRetry executes an operation with exponential backoff retry logic
func (c *Client) ExecuteWithRetry(ctx context.Context, operation RetryableOperation, operationName string) error {
	var lastErr error

	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(math.Pow(2, float64(attempt-1))) * c.config.RetryDelay
			log.Printf("Retrying %s operation after %v (attempt %d/%d)",
				operationName, delay, attempt, c.config.MaxRetries)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
				// Continue with retry
			}
		}

		// Execute the operation
		if err := operation(); err != nil {
			lastErr = err

			// Check if error is retryable
			if searchErr, ok := err.(*SearchError); ok {
				if !searchErr.IsRetryable() {
					log.Printf("%s operation failed with non-retryable error: %v", operationName, err)
					return err
				}
				log.Printf("%s operation failed (attempt %d/%d): %v",
					operationName, attempt+1, c.config.MaxRetries+1, err)
			} else {
				log.Printf("%s operation failed with unknown error (attempt %d/%d): %v",
					operationName, attempt+1, c.config.MaxRetries+1, err)
			}
			continue
		}

		// Success
		if attempt > 0 {
			log.Printf("%s operation succeeded after %d retries", operationName, attempt)
		}
		return nil
	}

	return fmt.Errorf("%s operation failed after %d attempts, last error: %w",
		operationName, c.config.MaxRetries+1, lastErr)
}

func (c *Client) SearchTermQuery(ctx context.Context, indexName string, query *TermQuery) (*TermQueryResponse, error) {
	if query == nil {
		return nil, NewSearchError("validation", "query cannot be nil")
	}
	if query.Field == "" {
		return nil, NewSearchError("validation", "field cannot be empty")
	}
	if len(query.Values) == 0 {
		return nil, NewSearchError("validation", "values cannot be empty")
	}

	normalized := TermQuery{
		Field: query.Field,
		Size:  query.Size,
		From:  query.From,
	}

	seen := make(map[string]struct{}, len(query.Values))
	for _, value := range query.Values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized.Values = append(normalized.Values, trimmed)
	}

	if len(normalized.Values) == 0 {
		return nil, NewSearchError("validation", "no valid values provided")
	}

	if normalized.Size <= 0 {
		normalized.Size = 10
	}
	if normalized.Size > 100 {
		normalized.Size = 100
	}
	if normalized.From < 0 {
		normalized.From = 0
	}

	operationCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	startTime := time.Now()
	var result *TermQueryResponse

	operation := func() error {
		if err := c.WaitForRateLimit(operationCtx); err != nil {
			return fmt.Errorf("rate limit error: %w", err)
		}

		body := BuildTermQueryBody(&normalized)
		bodyJSON, err := json.Marshal(body)
		if err != nil {
			return NewSearchError("validation", fmt.Sprintf("failed to marshal term query body: %v", err))
		}

		req := &opensearchapi.SearchReq{
			Indices: []string{indexName},
			Body:    bytes.NewReader(bodyJSON),
		}

		searchResp, err := c.client.Search(operationCtx, req)
		if err != nil {
			return ClassifyConnectionError(err)
		}
		if searchResp == nil {
			return NewSearchError("response", "received nil response from OpenSearch")
		}

		response := BuildTermQueryResponse(searchResp)
		if response == nil {
			return NewSearchError("response", "failed to parse term query response")
		}

		result = response
		return nil
	}

	err := c.ExecuteWithRetry(operationCtx, operation, "SearchTermQuery")

	duration := time.Since(startTime)
	c.RecordRequest(duration, err == nil)

	if err == nil && result != nil {
		log.Printf("Term query completed in %v, field=%s, hits=%d", duration, normalized.Field, result.TotalHits)
	}

	return result, err
}

func BuildTermQueryBody(query *TermQuery) map[string]interface{} {
	if query == nil {
		return map[string]interface{}{}
	}

	body := map[string]interface{}{
		"size": query.Size,
		"query": map[string]interface{}{
			"terms": map[string]interface{}{
				query.Field: query.Values,
			},
		},
	}

	if query.From > 0 {
		body["from"] = query.From
	} else {
		body["from"] = 0
	}

	return body
}

func BuildTermQueryResponse(searchResp *opensearchapi.SearchResp) *TermQueryResponse {
	if searchResp == nil {
		return nil
	}

	response := &TermQueryResponse{
		Took:      searchResp.Took,
		TimedOut:  searchResp.Timeout,
		TotalHits: int(searchResp.Hits.Total.Value),
		Results:   make([]TermQueryResult, len(searchResp.Hits.Hits)),
	}

	for i, hit := range searchResp.Hits.Hits {
		response.Results[i] = TermQueryResult{
			Index:  hit.Index,
			ID:     hit.ID,
			Score:  float64(hit.Score),
			Source: hit.Source,
		}
	}

	return response
}

// PerformanceMetrics holds performance statistics
type PerformanceMetrics struct {
	RequestCount    int64
	SuccessCount    int64
	ErrorCount      int64
	TotalDuration   time.Duration
	AverageLatency  time.Duration
	LastRequestTime time.Time
}

var globalMetrics = &PerformanceMetrics{}

// RecordRequest records request metrics
func (c *Client) RecordRequest(duration time.Duration, success bool) {
	globalMetrics.RequestCount++
	globalMetrics.TotalDuration += duration
	globalMetrics.LastRequestTime = time.Now()

	if success {
		globalMetrics.SuccessCount++
	} else {
		globalMetrics.ErrorCount++
	}

	if globalMetrics.RequestCount > 0 {
		globalMetrics.AverageLatency = globalMetrics.TotalDuration / time.Duration(globalMetrics.RequestCount)
	}
}

// GetMetrics returns current performance metrics
func (c *Client) GetMetrics() *PerformanceMetrics {
	return globalMetrics
}

// LogMetrics logs current performance metrics
func (c *Client) LogMetrics() {
	log.Printf("OpenSearch Client Metrics - Requests: %d, Success: %d, Errors: %d, Avg Latency: %v",
		globalMetrics.RequestCount, globalMetrics.SuccessCount,
		globalMetrics.ErrorCount, globalMetrics.AverageLatency)
}
