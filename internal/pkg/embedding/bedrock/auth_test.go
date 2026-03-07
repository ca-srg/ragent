package bedrock

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestBearerTokenTransport_SetsAuthHeader(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		assert.Equal(t, "Bearer test-api-key", req.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	transport := &bearerTokenTransport{token: "test-api-key"}
	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, resp.Body.Close())
	})

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestBearerTokenTransport_PreservesOtherHeaders(t *testing.T) {
	t.Parallel()

	transport := &bearerTokenTransport{
		token: "test-api-key",
		transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
			assert.Equal(t, "trace-123", req.Header.Get("X-Trace-ID"))
			assert.Equal(t, "Bearer test-api-key", req.Header.Get("Authorization"))

			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header)}, nil
		}),
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://example.com", nil)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Trace-ID", "trace-123")
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 prior-signature")

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, resp.Body.Close())
	})
}

func TestBearerTokenTransport_ClonesRequest(t *testing.T) {
	t.Parallel()

	var received *http.Request

	transport := &bearerTokenTransport{
		token: "test-api-key",
		transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			received = req
			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header)}, nil
		}),
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.com", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 original")
	req.Header.Set("X-Trace-ID", "trace-123")

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, resp.Body.Close())
	})

	require.NotNil(t, received)
	assert.NotSame(t, req, received)
	assert.Equal(t, "AWS4-HMAC-SHA256 original", req.Header.Get("Authorization"))
	assert.Equal(t, "trace-123", req.Header.Get("X-Trace-ID"))
	assert.Equal(t, "Bearer test-api-key", received.Header.Get("Authorization"))
}

func TestBuildBedrockAWSConfig_WithBearerToken(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "env-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "env-secret")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")

	cfg, err := BuildBedrockAWSConfig(context.Background(), "ap-northeast-1", "test-api-key")
	require.NoError(t, err)

	assert.Equal(t, "ap-northeast-1", cfg.Region)

	httpClient, ok := cfg.HTTPClient.(*http.Client)
	require.True(t, ok)

	bearerTransport, ok := httpClient.Transport.(*bearerTokenTransport)
	require.True(t, ok)
	assert.Equal(t, "test-api-key", bearerTransport.token)

	creds, err := cfg.Credentials.Retrieve(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "BEDROCK_BEARER", creds.AccessKeyID)
	assert.Equal(t, "BEDROCK_BEARER", creds.SecretAccessKey)
}

func TestBuildBedrockAWSConfig_WithoutBearerToken(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "env-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "env-secret")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")

	cfg, err := BuildBedrockAWSConfig(context.Background(), "us-east-1", "")
	require.NoError(t, err)

	creds, err := cfg.Credentials.Retrieve(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "env-key", creds.AccessKeyID)
	assert.Equal(t, "env-secret", creds.SecretAccessKey)

	httpClient, ok := cfg.HTTPClient.(*http.Client)
	if ok {
		_, isBearerTransport := httpClient.Transport.(*bearerTokenTransport)
		assert.False(t, isBearerTransport)
	}
}

func TestBuildBedrockAWSConfig_RegionSet(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "env-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "env-secret")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")

	cfg, err := BuildBedrockAWSConfig(context.Background(), "eu-west-1", "")
	require.NoError(t, err)

	assert.Equal(t, "eu-west-1", cfg.Region)
}
