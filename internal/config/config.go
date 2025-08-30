package config

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/ca-srg/mdrag/internal/types"
	env "github.com/netflix/go-env"
)

// Type alias for Config
type Config = types.Config

// Load loads configuration from environment variables
func Load() (*Config, error) {
	var config Config

	_, err := env.UnmarshalFromEnviron(&config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse environment variables: %w", err)
	}

	// Parse ExcludeCategories from pipe-separated string
	if config.ExcludeCategoriesStr != "" {
		config.ExcludeCategories = strings.Split(config.ExcludeCategoriesStr, "|")
	}

	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return &config, nil
}

// validateConfig validates configuration values and adjusts them to safe ranges
func validateConfig(config *Config) error {
	// Validate concurrency limits
	if config.Concurrency < 1 {
		config.Concurrency = 1
	}
	if config.Concurrency > 20 {
		config.Concurrency = 20
	}

	// Validate retry attempts
	if config.RetryAttempts < 0 {
		config.RetryAttempts = 0
	}
	if config.RetryAttempts > 10 {
		config.RetryAttempts = 10
	}

	// Validate OpenSearch configuration if endpoint is provided
	if config.OpenSearchEndpoint != "" {
		if err := validateOpenSearchConfig(config); err != nil {
			return fmt.Errorf("OpenSearch configuration validation failed: %w", err)
		}
	}

	return nil
}

// validateOpenSearchConfig validates OpenSearch-specific configuration
func validateOpenSearchConfig(config *Config) error {
	// Validate OpenSearch endpoint URL format
	if config.OpenSearchEndpoint == "" {
		return fmt.Errorf("OPENSEARCH_ENDPOINT is required when OpenSearch is enabled")
	}

	// Parse and validate URL format
	parsedURL, err := url.Parse(config.OpenSearchEndpoint)
	if err != nil {
		return fmt.Errorf("invalid OPENSEARCH_ENDPOINT URL format: %w", err)
	}

	// Check for required URL components
	if parsedURL.Scheme == "" {
		return fmt.Errorf("OPENSEARCH_ENDPOINT must include scheme (http:// or https://)")
	}

	if !strings.HasPrefix(parsedURL.Scheme, "http") {
		return fmt.Errorf("OPENSEARCH_ENDPOINT scheme must be http or https")
	}

	if parsedURL.Host == "" {
		return fmt.Errorf("OPENSEARCH_ENDPOINT must include a valid host")
	}

	// Validate OpenSearch region
	if config.OpenSearchRegion == "" {
		return fmt.Errorf("OPENSEARCH_REGION is required when OpenSearch is enabled")
	}

	// Validate rate limiting configuration
	if config.OpenSearchRateLimit <= 0 {
		return fmt.Errorf("OPENSEARCH_RATE_LIMIT must be greater than 0")
	}
	if config.OpenSearchRateLimit > 1000 {
		return fmt.Errorf("OPENSEARCH_RATE_LIMIT cannot exceed 1000 requests/second")
	}

	if config.OpenSearchRateBurst <= 0 {
		return fmt.Errorf("OPENSEARCH_RATE_BURST must be greater than 0")
	}
	if config.OpenSearchRateBurst > int(config.OpenSearchRateLimit*10) {
		return fmt.Errorf("OPENSEARCH_RATE_BURST should not exceed 10x the rate limit")
	}

	// Validate timeout values
	if config.OpenSearchConnectionTimeout <= 0 {
		return fmt.Errorf("OPENSEARCH_CONNECTION_TIMEOUT must be greater than 0")
	}
	if config.OpenSearchRequestTimeout <= 0 {
		return fmt.Errorf("OPENSEARCH_REQUEST_TIMEOUT must be greater than 0")
	}

	// Validate retry configuration
	if config.OpenSearchMaxRetries < 0 {
		return fmt.Errorf("OPENSEARCH_MAX_RETRIES cannot be negative")
	}
	if config.OpenSearchMaxRetries > 10 {
		return fmt.Errorf("OPENSEARCH_MAX_RETRIES cannot exceed 10")
	}

	if config.OpenSearchRetryDelay <= 0 {
		return fmt.Errorf("OPENSEARCH_RETRY_DELAY must be greater than 0")
	}

	// Validate connection pool settings
	if config.OpenSearchMaxConnections <= 0 {
		return fmt.Errorf("OPENSEARCH_MAX_CONNECTIONS must be greater than 0")
	}
	if config.OpenSearchMaxConnections > 100 {
		return fmt.Errorf("OPENSEARCH_MAX_CONNECTIONS cannot exceed 100")
	}

	if config.OpenSearchMaxIdleConns <= 0 {
		return fmt.Errorf("OPENSEARCH_MAX_IDLE_CONNS must be greater than 0")
	}
	if config.OpenSearchMaxIdleConns > config.OpenSearchMaxConnections {
		return fmt.Errorf("OPENSEARCH_MAX_IDLE_CONNS cannot exceed OPENSEARCH_MAX_CONNECTIONS")
	}

	if config.OpenSearchIdleConnTimeout <= 0 {
		return fmt.Errorf("OPENSEARCH_IDLE_CONN_TIMEOUT must be greater than 0")
	}

	return nil
}
