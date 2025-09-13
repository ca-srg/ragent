package mcpserver

import (
	"context"
	"fmt"
	"log"
	"net/http"
)

// AuthMethod represents the authentication method
type AuthMethod string

const (
	// AuthMethodIP uses IP address based authentication
	AuthMethodIP AuthMethod = "ip"
	// AuthMethodOIDC uses OpenID Connect authentication
	AuthMethodOIDC AuthMethod = "oidc"
	// AuthMethodBoth requires both IP and OIDC authentication
	AuthMethodBoth AuthMethod = "both"
	// AuthMethodEither allows either IP or OIDC authentication
	AuthMethodEither AuthMethod = "either"
)

// UnifiedAuthMiddleware combines IP and OIDC authentication
type UnifiedAuthMiddleware struct {
	ipAuth        *IPAuthMiddleware
	oidcAuth      *OIDCAuthMiddleware
	authMethod    AuthMethod
	enableLogging bool
}

// UnifiedAuthConfig contains configuration for unified authentication
type UnifiedAuthConfig struct {
	AuthMethod    AuthMethod    // Authentication method to use
	IPConfig      *IPAuthConfig // Configuration for IP authentication
	OIDCConfig    *OIDCConfig   // Configuration for OIDC authentication
	EnableLogging bool          // Enable detailed logging
}

// IPAuthConfig contains configuration for IP authentication
type IPAuthConfig struct {
	AllowedIPs    []string // List of allowed IP addresses/CIDR blocks
	EnableLogging bool     // Enable detailed logging
}

// NewUnifiedAuthMiddleware creates a new unified authentication middleware
func NewUnifiedAuthMiddleware(config *UnifiedAuthConfig) (*UnifiedAuthMiddleware, error) {
	if config == nil {
		return nil, fmt.Errorf("configuration is required")
	}

	middleware := &UnifiedAuthMiddleware{
		authMethod:    config.AuthMethod,
		enableLogging: config.EnableLogging,
	}

	// Initialize IP authentication if needed
	if config.AuthMethod == AuthMethodIP || config.AuthMethod == AuthMethodBoth || config.AuthMethod == AuthMethodEither {
		if config.IPConfig == nil || len(config.IPConfig.AllowedIPs) == 0 {
			return nil, fmt.Errorf("IP configuration is required for method %s", config.AuthMethod)
		}

		ipAuth, err := NewIPAuthMiddleware(config.IPConfig.AllowedIPs, config.IPConfig.EnableLogging)
		if err != nil {
			return nil, fmt.Errorf("failed to create IP auth middleware: %w", err)
		}
		middleware.ipAuth = ipAuth
	}

	// Initialize OIDC authentication if needed
	if config.AuthMethod == AuthMethodOIDC || config.AuthMethod == AuthMethodBoth || config.AuthMethod == AuthMethodEither {
		if config.OIDCConfig == nil {
			return nil, fmt.Errorf("OIDC configuration is required for method %s", config.AuthMethod)
		}

		oidcAuth, err := NewOIDCAuthMiddleware(config.OIDCConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create OIDC auth middleware: %w", err)
		}
		middleware.oidcAuth = oidcAuth
	}

	if middleware.enableLogging {
		log.Printf("Unified Auth Middleware initialized with method: %s", config.AuthMethod)
	}

	return middleware, nil
}

// Middleware returns the HTTP middleware function
func (m *UnifiedAuthMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Bypass authentication for OAuth2 paths when OIDC is configured
		if (r.URL.Path == "/callback" || r.URL.Path == "/login") && m.oidcAuth != nil {
			next.ServeHTTP(w, r)
			return
		}
		switch m.authMethod {
		case AuthMethodIP:
			m.ipAuth.Middleware(next).ServeHTTP(w, r)

		case AuthMethodOIDC:
			m.oidcAuth.Middleware(next).ServeHTTP(w, r)

		case AuthMethodBoth:
			// Require both IP and OIDC authentication
			m.ipAuth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				m.oidcAuth.Middleware(next).ServeHTTP(w, r)
			})).ServeHTTP(w, r)

		case AuthMethodEither:
			// Allow either IP or OIDC authentication
			m.handleEitherAuth(next, w, r)

		default:
			// No authentication
			next.ServeHTTP(w, r)
		}
	})
}

// handleEitherAuth handles the case where either IP or OIDC authentication is acceptable
func (m *UnifiedAuthMiddleware) handleEitherAuth(next http.Handler, w http.ResponseWriter, r *http.Request) {
	// First try IP authentication
	clientIP := m.ipAuth.extractClientIP(r)
	if m.ipAuth.IsIPAllowed(clientIP) {
		if m.enableLogging {
			log.Printf("Access granted via IP authentication for IP: %s", clientIP)
		}
		next.ServeHTTP(w, r)
		return
	}

	// IP auth failed, try OIDC authentication
	token := m.oidcAuth.extractToken(r)
	if token != "" {
		tokenInfo, err := m.oidcAuth.validateToken(token)
		if err == nil {
			if m.enableLogging {
				log.Printf("Access granted via OIDC authentication for user: %s", tokenInfo.Email)
			}
			// Add user info to request context
			ctx := context.WithValue(r.Context(), "user", tokenInfo)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
	}

	// Both authentication methods failed
	if m.enableLogging {
		log.Printf("Access denied: Neither IP (%s) nor OIDC authentication succeeded", clientIP)
	}

	// Send authentication required response
	m.oidcAuth.sendAuthenticationRequired(w, r)
}

// GetAuthMethod returns the current authentication method
func (m *UnifiedAuthMiddleware) GetAuthMethod() AuthMethod {
	return m.authMethod
}

// SetAuthMethod updates the authentication method
func (m *UnifiedAuthMiddleware) SetAuthMethod(method AuthMethod) error {
	// Validate that required middleware is available
	switch method {
	case AuthMethodIP:
		if m.ipAuth == nil {
			return fmt.Errorf("IP authentication is not configured")
		}
	case AuthMethodOIDC:
		if m.oidcAuth == nil {
			return fmt.Errorf("OIDC authentication is not configured")
		}
	case AuthMethodBoth, AuthMethodEither:
		if m.ipAuth == nil || m.oidcAuth == nil {
			return fmt.Errorf("both IP and OIDC authentication must be configured")
		}
	}

	m.authMethod = method
	if m.enableLogging {
		log.Printf("Authentication method changed to: %s", method)
	}
	return nil
}

// GetIPAuthMiddleware returns the IP authentication middleware
func (m *UnifiedAuthMiddleware) GetIPAuthMiddleware() *IPAuthMiddleware {
	return m.ipAuth
}

// GetOIDCAuthMiddleware returns the OIDC authentication middleware
func (m *UnifiedAuthMiddleware) GetOIDCAuthMiddleware() *OIDCAuthMiddleware {
	return m.oidcAuth
}

// StartOIDCAuthFlow starts the OIDC authentication flow
func (m *UnifiedAuthMiddleware) StartOIDCAuthFlow(ctx context.Context) (*TokenInfo, error) {
	if m.oidcAuth == nil {
		return nil, fmt.Errorf("OIDC authentication is not configured")
	}
	return m.oidcAuth.StartAuthFlow(ctx)
}
