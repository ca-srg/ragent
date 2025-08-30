package opensearch

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"math"
	"net/http"
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
