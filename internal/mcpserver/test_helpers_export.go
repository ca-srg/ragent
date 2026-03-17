package mcpserver

import (
	"context"
	"net/http"
)

// NewTestOIDCContextInjector creates an HTTP middleware that injects OIDC user context for E2E testing.
// It simulates OIDC authentication by setting userContextKey in the request context, which causes
// applySecretPolicyFromContext to set ExcludeSecret=false (allowing access to secret documents).
func NewTestOIDCContextInjector(subject, email string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenInfo := &TokenInfo{Subject: subject, Email: email}
			ctx := context.WithValue(r.Context(), userContextKey, tokenInfo)
			ctx = context.WithValue(ctx, authMethodContextKey, string(AuthMethodOIDC))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
