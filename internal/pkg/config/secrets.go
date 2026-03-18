package config

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// Environment variable names for Secrets Manager configuration.
const (
	envSecretManagerSecretID = "SECRET_MANAGER_SECRET_ID"
	envSecretManagerRegion   = "SECRET_MANAGER_REGION"
)

// SecretsManagerClient is an interface for AWS Secrets Manager operations.
// It exists to allow test injection of mock clients.
type SecretsManagerClient interface {
	GetSecretValue(ctx context.Context, params *secretsmanager.GetSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

// smClientFactory creates a SecretsManagerClient for the given region.
// Overriding this variable in tests allows injection of mock clients.
var smClientFactory func(ctx context.Context, region string) (SecretsManagerClient, error)

// secretsOnce ensures SM fetch happens at most once per process lifetime.
var secretsOnce sync.Once

// secretsErr stores the error from the one-time SM fetch (used with secretsOnce).
var secretsErr error

// LoadSecretsIntoEnv fetches secrets from AWS Secrets Manager and injects them
// as environment variables for keys not already set. It is safe to call multiple
// times; subsequent calls are no-ops due to sync.Once.
//
// If SECRET_MANAGER_SECRET_ID is not set, this function does nothing.
func LoadSecretsIntoEnv(ctx context.Context) error {
	secretsOnce.Do(func() {
		secretsErr = loadSecretsOnce(ctx)
	})
	return secretsErr
}

// loadSecretsOnce performs the actual one-time fetch from Secrets Manager.
func loadSecretsOnce(ctx context.Context) error {
	secretID := os.Getenv(envSecretManagerSecretID)
	if secretID == "" {
		// Not configured — behave identically to the pre-SM baseline.
		return nil
	}

	region := os.Getenv(envSecretManagerRegion)
	if region == "" {
		region = "us-east-1"
	}

	factory := smClientFactory
	if factory == nil {
		factory = newDefaultSMClient
	}

	client, err := factory(ctx, region)
	if err != nil {
		return fmt.Errorf("failed to create Secrets Manager client: %w", err)
	}

	output, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretID),
	})
	if err != nil {
		return fmt.Errorf("failed to get secret value from Secrets Manager: %w", err)
	}

	if output.SecretString == nil {
		return nil
	}

	var secrets map[string]interface{}
	if err := json.Unmarshal([]byte(*output.SecretString), &secrets); err != nil {
		return fmt.Errorf("failed to parse secret JSON from Secrets Manager: %w", err)
	}

	injected := 0
	skipped := 0
	for key, val := range secrets {
		strVal, ok := val.(string)
		if !ok {
			// Skip non-string values (objects, arrays, numbers).
			continue
		}
		if existing, exists := os.LookupEnv(key); exists {
			// Existing env var takes priority; never overwrite — even if empty.
			skipped++
			log.Printf("Secrets Manager: skipping %s (already set in environment to %q)", key, maskValue(key, existing))
			continue
		}
		if err := os.Setenv(key, strVal); err != nil {
			return fmt.Errorf("failed to set environment variable %s: %w", key, err)
		}
		log.Printf("Secrets Manager: injected %s", key)
		injected++
	}

	log.Printf("Secrets Manager: injected %d key(s), skipped %d key(s) (already set in env)", injected, skipped)

	return nil
}

var sensitiveKeySubstrings = []string{"TOKEN", "SECRET", "KEY", "PASSWORD", "BEARER"}

func maskValue(key, val string) string {
	upper := strings.ToUpper(key)
	for _, sub := range sensitiveKeySubstrings {
		if strings.Contains(upper, sub) {
			if len(val) <= 4 {
				return "***"
			}
			return val[:4] + "***"
		}
	}
	return val
}

// newDefaultSMClient builds a real AWS Secrets Manager client using the
// default credential chain for the given region.
func newDefaultSMClient(ctx context.Context, region string) (SecretsManagerClient, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config for Secrets Manager: %w", err)
	}
	return secretsmanager.NewFromConfig(cfg), nil
}

// ResetSecretsLoaderForTest resets the sync.Once, cached error, and client
// factory so that LoadSecretsIntoEnv behaves as if it has never been called.
// Call this in t.Cleanup to prevent state pollution between tests.
//
// WARNING: Do NOT call this function outside of tests.
func ResetSecretsLoaderForTest() {
	secretsOnce = sync.Once{}
	secretsErr = nil
	smClientFactory = nil
}
