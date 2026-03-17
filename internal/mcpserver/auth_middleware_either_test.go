package mcpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newEitherAuthMiddleware(t *testing.T) *UnifiedAuthMiddleware {
	t.Helper()
	config := &UnifiedAuthConfig{
		AuthMethod: AuthMethodEither,
		IPConfig: &IPAuthConfig{
			AllowedIPs: []string{"127.0.0.1"},
		},
		OIDCConfig: &OIDCConfig{
			ClientID:         "test-client",
			AuthorizationURL: "http://localhost/auth",
			TokenURL:         "http://localhost/token",
			SkipDiscovery:    true,
		},
	}

	middleware, err := NewUnifiedAuthMiddleware(config)
	require.NoError(t, err)
	return middleware
}

func storeTestToken(middleware *UnifiedAuthMiddleware, token string) {
	middleware.oidcAuth.tokenStore.mutex.Lock()
	middleware.oidcAuth.tokenStore.tokens[token] = &TokenInfo{
		IDToken:   token,
		Subject:   "test-user",
		Email:     "test@example.com",
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	middleware.oidcAuth.tokenStore.mutex.Unlock()
}

func TestHandleEitherAuth_OIDCPriorityOverIP(t *testing.T) {
	middleware := newEitherAuthMiddleware(t)
	testToken := "valid-oidc-token"
	storeTestToken(middleware, testToken)

	tests := []struct {
		name              string
		clientIP          string
		bearerToken       string
		expectStatus      int
		expectAuthMethod  string
		expectUserContext bool
	}{
		{
			name:              "OIDC token + allowed IP → OIDC wins, user context set",
			clientIP:          "127.0.0.1",
			bearerToken:       testToken,
			expectStatus:      http.StatusOK,
			expectAuthMethod:  string(AuthMethodOIDC),
			expectUserContext: true,
		},
		{
			name:              "no OIDC token + allowed IP → IP fallback, no user context",
			clientIP:          "127.0.0.1",
			bearerToken:       "",
			expectStatus:      http.StatusOK,
			expectAuthMethod:  string(AuthMethodIP),
			expectUserContext: false,
		},
		{
			name:              "invalid OIDC token + allowed IP → IP fallback",
			clientIP:          "127.0.0.1",
			bearerToken:       "invalid-token",
			expectStatus:      http.StatusOK,
			expectAuthMethod:  string(AuthMethodIP),
			expectUserContext: false,
		},
		{
			name:              "OIDC token + disallowed IP → OIDC wins",
			clientIP:          "8.8.8.8",
			bearerToken:       testToken,
			expectStatus:      http.StatusOK,
			expectAuthMethod:  string(AuthMethodOIDC),
			expectUserContext: true,
		},
		{
			name:              "no OIDC + disallowed IP → denied",
			clientIP:          "8.8.8.8",
			bearerToken:       "",
			expectStatus:      http.StatusUnauthorized,
			expectAuthMethod:  "",
			expectUserContext: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedAuthMethod string
			var capturedHasUser bool

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if method, ok := r.Context().Value(authMethodContextKey).(string); ok {
					capturedAuthMethod = method
				}
				if _, ok := r.Context().Value(userContextKey).(*TokenInfo); ok {
					capturedHasUser = true
				}
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest("GET", "/api/test", nil)
			req.RemoteAddr = tt.clientIP + ":12345"
			if tt.bearerToken != "" {
				req.Header.Set("Authorization", "Bearer "+tt.bearerToken)
			}

			rr := httptest.NewRecorder()
			middleware.Middleware(handler).ServeHTTP(rr, req)

			assert.Equal(t, tt.expectStatus, rr.Code)
			if tt.expectStatus == http.StatusOK {
				assert.Equal(t, tt.expectAuthMethod, capturedAuthMethod)
				assert.Equal(t, tt.expectUserContext, capturedHasUser)
			}
		})
	}
}

func TestHandleEitherAuth_OIDCEnablesSecretAccess(t *testing.T) {
	middleware := newEitherAuthMiddleware(t)
	testToken := "oidc-secret-token"
	storeTestToken(middleware, testToken)

	adapter := &HybridSearchToolAdapter{}

	tests := []struct {
		name                  string
		bearerToken           string
		expectedExcludeSecret bool
	}{
		{
			name:                  "OIDC token present → secret access allowed",
			bearerToken:           testToken,
			expectedExcludeSecret: false,
		},
		{
			name:                  "no OIDC token (IP only) → secret access denied",
			bearerToken:           "",
			expectedExcludeSecret: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedCtx context.Context

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedCtx = r.Context()
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest("GET", "/api/test", nil)
			req.RemoteAddr = "127.0.0.1:12345"
			if tt.bearerToken != "" {
				req.Header.Set("Authorization", "Bearer "+tt.bearerToken)
			}

			rr := httptest.NewRecorder()
			middleware.Middleware(handler).ServeHTTP(rr, req)
			require.Equal(t, http.StatusOK, rr.Code)

			request := &HybridSearchRequest{}
			adapter.applySecretPolicyFromContext(capturedCtx, request)
			assert.Equal(t, tt.expectedExcludeSecret, request.ExcludeSecret)
		})
	}
}
