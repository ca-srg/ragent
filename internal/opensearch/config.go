package opensearch

import (
	"fmt"
	"time"

	"github.com/ca-srg/kiberag/internal/types"
)

func NewConfigFromTypes(cfg *types.Config) (*Config, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	return &Config{
		Endpoint:          cfg.OpenSearchEndpoint,
		Region:            cfg.OpenSearchRegion,
		InsecureSkipTLS:   cfg.OpenSearchInsecureSkipTLS,
		RateLimit:         cfg.OpenSearchRateLimit,
		RateBurst:         cfg.OpenSearchRateBurst,
		ConnectionTimeout: cfg.OpenSearchConnectionTimeout,
		RequestTimeout:    cfg.OpenSearchRequestTimeout,
		MaxRetries:        cfg.OpenSearchMaxRetries,
		RetryDelay:        cfg.OpenSearchRetryDelay,
		MaxConnections:    cfg.OpenSearchMaxConnections,
		MaxIdleConns:      cfg.OpenSearchMaxIdleConns,
		IdleConnTimeout:   cfg.OpenSearchIdleConnTimeout,
	}, nil
}

func (c *Config) Validate() error {
	if c.Endpoint == "" {
		return fmt.Errorf("endpoint is required")
	}

	if c.Region == "" {
		return fmt.Errorf("region is required")
	}

	if c.RateLimit <= 0 {
		c.RateLimit = 10.0
	}
	if c.RateLimit > 1000 {
		c.RateLimit = 1000.0
	}

	if c.RateBurst <= 0 {
		c.RateBurst = 20
	}
	if c.RateBurst > 10000 {
		c.RateBurst = 10000
	}

	if c.ConnectionTimeout <= 0 {
		c.ConnectionTimeout = 30 * time.Second
	}
	if c.ConnectionTimeout > 300*time.Second {
		c.ConnectionTimeout = 300 * time.Second
	}

	if c.RequestTimeout <= 0 {
		c.RequestTimeout = 60 * time.Second
	}
	if c.RequestTimeout > 600*time.Second {
		c.RequestTimeout = 600 * time.Second
	}

	return nil
}
