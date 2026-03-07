package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ca-srg/ragent/internal/pkg/config"
)

// setRequiredEnvVars sets the minimum required env vars to pass Load() validation.
// OPENSEARCH_ENDPOINT and OPENSEARCH_INDEX remain required=true in the env tag.
func setRequiredEnvVars(t *testing.T) {
	t.Helper()
	t.Setenv("OPENSEARCH_ENDPOINT", "http://localhost:9200")
	t.Setenv("OPENSEARCH_INDEX", "test_index")
}

// TestLoadSQLiteBackendWithoutS3Vars verifies that sqlite backend loads successfully
// without AWS_S3_VECTOR_BUCKET or AWS_S3_VECTOR_INDEX being set.
func TestLoadSQLiteBackendWithoutS3Vars(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("VECTOR_DB_BACKEND", "sqlite")
	// Explicitly empty S3 vars to ensure they are not picked up from environment
	t.Setenv("AWS_S3_VECTOR_BUCKET", "")
	t.Setenv("AWS_S3_VECTOR_INDEX", "")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "sqlite", cfg.VectorDBBackend)
}

// TestLoadS3BackendRequiresBucket verifies that s3 backend returns an error
// when AWS_S3_VECTOR_BUCKET is not set.
func TestLoadS3BackendRequiresBucket(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("VECTOR_DB_BACKEND", "s3")
	t.Setenv("AWS_S3_VECTOR_BUCKET", "")
	t.Setenv("AWS_S3_VECTOR_INDEX", "")

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AWS_S3_VECTOR_BUCKET")
}

// TestLoadInvalidBackendReturnsError verifies that an unrecognized backend value
// causes Load() to return an error with a clear message.
func TestLoadInvalidBackendReturnsError(t *testing.T) {
	setRequiredEnvVars(t)
	t.Setenv("VECTOR_DB_BACKEND", "badvalue")
	// Provide S3 vars so the error comes from backend validation, not bucket check
	t.Setenv("AWS_S3_VECTOR_BUCKET", "my-bucket")
	t.Setenv("AWS_S3_VECTOR_INDEX", "my-index")

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "VECTOR_DB_BACKEND")
}
