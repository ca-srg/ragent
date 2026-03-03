package unit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ca-srg/ragent/internal/mcpserver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOIDCAuthMiddleware_CustomEndpoints(t *testing.T) {
	tests := []struct {
		name          string
		config        *mcpserver.OIDCConfig
		expectError   bool
		errorContains string
	}{
		{
			name: "valid custom endpoints",
			config: &mcpserver.OIDCConfig{
				ClientID:         "test-client",
				ClientSecret:     "test-secret",
				AuthorizationURL: "https://auth.example.com/authorize",
				TokenURL:         "https://auth.example.com/token",
				SkipDiscovery:    true,
			},
			expectError: false,
		},
		{
			name: "missing authorization URL",
			config: &mcpserver.OIDCConfig{
				ClientID:      "test-client",
				TokenURL:      "https://auth.example.com/token",
				SkipDiscovery: true,
			},
			expectError:   true,
			errorContains: "authorization URL and token URL are required",
		},
		{
			name: "missing token URL",
			config: &mcpserver.OIDCConfig{
				ClientID:         "test-client",
				AuthorizationURL: "https://auth.example.com/authorize",
				SkipDiscovery:    true,
			},
			expectError:   true,
			errorContains: "authorization URL and token URL are required",
		},
		{
			name: "missing client ID",
			config: &mcpserver.OIDCConfig{
				AuthorizationURL: "https://auth.example.com/authorize",
				TokenURL:         "https://auth.example.com/token",
				SkipDiscovery:    true,
			},
			expectError:   true,
			errorContains: "client ID is required",
		},
		{
			name: "valid discovery with custom override",
			config: &mcpserver.OIDCConfig{
				Issuer:       "https://accounts.google.com",
				ClientID:     "test-client",
				ClientSecret: "test-secret",
				TokenURL:     "https://custom.token.endpoint/token",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware, err := mcpserver.NewOIDCAuthMiddleware(tt.config)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, middleware)
			}
		})
	}
}

func TestOIDCAuthMiddleware_GetAuthURL(t *testing.T) {
	config := &mcpserver.OIDCConfig{
		ClientID:         "test-client",
		ClientSecret:     "test-secret",
		AuthorizationURL: "https://auth.example.com/authorize",
		TokenURL:         "https://auth.example.com/token",
		SkipDiscovery:    true,
		Scopes:           []string{"openid", "profile"},
	}

	middleware, err := mcpserver.NewOIDCAuthMiddleware(config)
	require.NoError(t, err)

	authURL := middleware.GetAuthURL()
	assert.Contains(t, authURL, "https://auth.example.com/authorize")
	assert.Contains(t, authURL, "client_id=test-client")
	assert.Contains(t, authURL, "scope=openid+profile")
	assert.Contains(t, authURL, "response_type=code")
}

func TestOIDCAuthMiddleware_CustomUserInfo(t *testing.T) {
	// Create test server for custom userinfo endpoint
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		// Return user info
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{
        "sub": "test-user",
        "email": "test@example.com",
        "name": "Test User"
    }`)); err != nil {
			t.Fatalf("failed to write userinfo response: %v", err)
		}
	}))
	defer server.Close()

	config := &mcpserver.OIDCConfig{
		ClientID:         "test-client",
		ClientSecret:     "test-secret",
		AuthorizationURL: "https://auth.example.com/authorize",
		TokenURL:         "https://auth.example.com/token",
		UserInfoURL:      server.URL,
		SkipDiscovery:    true,
	}

	middleware, err := mcpserver.NewOIDCAuthMiddleware(config)
	require.NoError(t, err)

	ctx := context.Background()
	userInfo, err := middleware.GetUserInfo(ctx, "test-token")
	require.NoError(t, err)

	assert.Equal(t, "test-user", userInfo["sub"])
	assert.Equal(t, "test@example.com", userInfo["email"])
	assert.Equal(t, "Test User", userInfo["name"])
}

func TestUnifiedAuthMiddleware_Configuration(t *testing.T) {
	tests := []struct {
		name        string
		config      *mcpserver.UnifiedAuthConfig
		expectError bool
	}{
		{
			name: "OIDC with custom endpoints",
			config: &mcpserver.UnifiedAuthConfig{
				AuthMethod: mcpserver.AuthMethodOIDC,
				OIDCConfig: &mcpserver.OIDCConfig{
					ClientID:         "test-client",
					AuthorizationURL: "https://auth.example.com/authorize",
					TokenURL:         "https://auth.example.com/token",
					SkipDiscovery:    true,
				},
			},
			expectError: false,
		},
		{
			name: "Either mode with both IP and custom OIDC",
			config: &mcpserver.UnifiedAuthConfig{
				AuthMethod: mcpserver.AuthMethodEither,
				IPConfig: &mcpserver.IPAuthConfig{
					AllowedIPs: []string{"127.0.0.1"},
				},
				OIDCConfig: &mcpserver.OIDCConfig{
					ClientID:         "test-client",
					AuthorizationURL: "https://auth.example.com/authorize",
					TokenURL:         "https://auth.example.com/token",
					SkipDiscovery:    true,
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware, err := mcpserver.NewUnifiedAuthMiddleware(tt.config)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, middleware)
				assert.Equal(t, tt.config.AuthMethod, middleware.GetAuthMethod())
			}
		})
	}
}
