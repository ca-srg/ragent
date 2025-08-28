package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/ca-srg/kiberag/internal/types"
)

// Type alias for Config
type Config = types.Config

// Load loads configuration from environment variables
func Load() (*Config, error) {
	// Load .env file if it exists
	_ = godotenv.Load()

	config := &Config{

		// AWS S3 Vectors configuration
		AWSS3VectorBucket: getEnvString("AWS_S3_VECTOR_BUCKET", ""),
		AWSS3VectorIndex:  getEnvString("AWS_S3_VECTOR_INDEX", ""),
		AWSS3Region:       getEnvString("AWS_S3_REGION", "us-east-1"),

		// Chat configuration
		ChatModel: getEnvString("CHAT_MODEL", "anthropic.claude-3-5-sonnet-20240620-v1:0"),

		// Processing configuration
		Concurrency:   getEnvInt("VECTORIZER_CONCURRENCY", 3), // 3 is the default value for concurrency because of the rate limit of S3
		RetryAttempts: getEnvInt("VECTORIZER_RETRY_ATTEMPTS", 3),
		RetryDelay:    getEnvDuration("VECTORIZER_RETRY_DELAY", 2*time.Second),

		// Filter configuration
		ExcludeCategories: getEnvStringSlice("EXCLUDE_CATEGORIES", []string{"個人メモ", "日報"}),
	}

	if err := validateConfig(config); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return config, nil
}

// validateConfig validates that all required configuration is present
func validateConfig(config *Config) error {
	var missingFields []string

	if config.AWSS3VectorBucket == "" {
		missingFields = append(missingFields, "AWS_S3_VECTOR_BUCKET")
	}

	if config.AWSS3VectorIndex == "" {
		missingFields = append(missingFields, "AWS_S3_VECTOR_INDEX")
	}

	if len(missingFields) > 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missingFields, ", "))
	}

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

	// Validate retry delay
	if config.RetryDelay < time.Second {
		config.RetryDelay = time.Second
	}
	if config.RetryDelay > 30*time.Second {
		config.RetryDelay = 30 * time.Second
	}

	return nil
}

// GenerateConfigGuide returns a configuration guide for users
func GenerateConfigGuide() string {
	return `
Vectorizer Configuration Guide
=============================

Required Environment Variables:
-------------------------------

1. AWS S3 Configuration:
   AWS_S3_BUCKET          - S3 bucket name for storing vectors (required)
   AWS_S3_REGION          - AWS region (optional, default: us-east-1)

3. Processing Configuration:
   VECTORIZER_CONCURRENCY     - Max concurrent operations (optional, default: 5, range: 1-20)
   VECTORIZER_RETRY_ATTEMPTS  - Number of retry attempts (optional, default: 3, range: 0-10)
   VECTORIZER_RETRY_DELAY     - Delay between retries (optional, default: 2s, range: 1s-30s)

Example .env file:
-----------------
AWS_S3_BUCKET=your-vector-bucket
AWS_S3_REGION=us-east-1
VECTORIZER_CONCURRENCY=5

AWS Authentication:
------------------
This application uses AWS SDK's default credential chain for authentication.
You can configure AWS credentials using any of these methods:

1. AWS credentials file (~/.aws/credentials)
2. AWS config file (~/.aws/config)
3. Environment variables (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY)
4. IAM roles (for EC2 instances)
5. AWS profiles

Setup Instructions:
------------------
1. Create an S3 bucket for storing vectors
2. Configure AWS credentials using your preferred method (see AWS Authentication above)
3. Add the required environment variables to your .env file
4. Run 'kiberag vectorize --directory ./markdown' to start processing
`
}

// getEnvString returns environment variable value or default
func getEnvString(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvInt returns environment variable as integer or default
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// getEnvDuration returns environment variable as duration or default
func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

func getEnvStringSlice(key string, defaultValue []string) []string {
	if value := os.Getenv(key); value != "" {
		// Split by comma and trim spaces
		parts := strings.Split(value, ",")
		result := make([]string, 0, len(parts))
		for _, part := range parts {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				result = append(result, trimmed)
			}
		}
		return result
	}
	return defaultValue
}

// GetConfigExample returns an example configuration for testing
func GetConfigExample() *Config {
	return &Config{
		AWSS3VectorBucket: "test-vector-bucket",
		AWSS3VectorIndex:  "test-index",
		AWSS3Region:       "us-east-1",
		ChatModel:         "anthropic.claude-3-5-sonnet-20240620-v1:0",
		Concurrency:       3,
		RetryAttempts:     2,
		RetryDelay:        time.Second,
		ExcludeCategories: []string{"個人メモ", "日報"},
	}
}
