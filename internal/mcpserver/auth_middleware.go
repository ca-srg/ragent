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

// UnifiedAuthMiddleware combines IP and OIDC authentication with bypass support
type UnifiedAuthMiddleware struct {
	ipAuth         *IPAuthMiddleware
	oidcAuth       *OIDCAuthMiddleware
	authMethod     AuthMethod
	enableLogging  bool
	bypassChecker  BypassIPChecker
	bypassLogger   BypassAuditLogger
	trustedProxies []string
}

// UnifiedAuthConfig contains configuration for unified authentication
type UnifiedAuthConfig struct {
	AuthMethod    AuthMethod      // Authentication method to use
	IPConfig      *IPAuthConfig   // Configuration for IP authentication
	OIDCConfig    *OIDCConfig     // Configuration for OIDC authentication
	EnableLogging bool            // Enable detailed logging
	BypassConfig  *BypassIPConfig // Configuration for IP bypass authentication
}

// IPAuthConfig contains configuration for IP authentication
type IPAuthConfig struct {
	AllowedIPs    []string // List of allowed IP addresses/CIDR blocks
	EnableLogging bool     // Enable detailed logging
}

// BypassIPConfig contains configuration for IP bypass authentication
type BypassIPConfig struct {
	BypassIPRanges []string // List of IP ranges that bypass authentication (CIDR format)
	VerboseLogging bool     // Enable verbose logging for bypass checks
	AuditLogging   bool     // Enable audit logging for bypass access
	TrustedProxies []string // List of trusted proxy IPs for X-Forwarded-For processing
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

	// Initialize bypass authentication if configured
	if config.BypassConfig != nil && len(config.BypassConfig.BypassIPRanges) > 0 {
		// Validate configuration: bypass with "either" auth method could create a security issue
		if config.AuthMethod == AuthMethodEither {
			return nil, fmt.Errorf("bypass authentication cannot be used with 'either' auth method (security risk)")
		}

		bypassChecker, err := NewBypassIPChecker(
			config.BypassConfig.BypassIPRanges,
			config.BypassConfig.VerboseLogging,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create bypass IP checker: %w", err)
		}
		middleware.bypassChecker = bypassChecker

		// Initialize audit logger if enabled
		if config.BypassConfig.AuditLogging {
			middleware.bypassLogger = NewBypassAuditLogger(true)
		}

		// Store trusted proxies for IP extraction
		middleware.trustedProxies = config.BypassConfig.TrustedProxies

		if middleware.enableLogging {
			log.Printf("Bypass authentication enabled with %d IP ranges", len(config.BypassConfig.BypassIPRanges))
		}
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
		if middleware.bypassChecker != nil {
			log.Printf("Bypass authentication is enabled for %d IP ranges", len(config.BypassConfig.BypassIPRanges))
		}
	}

	return middleware, nil
}

// Middleware returns the HTTP middleware function
func (m *UnifiedAuthMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for bypass authentication first
		if m.bypassChecker != nil {
			clientIP := ExtractClientIPFromRequest(r, m.trustedProxies)
			if m.bypassChecker.ShouldBypass(clientIP) {
				shouldLog := r.URL.Path != "/health"
				// Log bypass access if audit logging is enabled
				if shouldLog && m.bypassLogger != nil {
					entry := CreateBypassAuditEntry(clientIP, r.Method, r.URL.Path)
					entry = entry.WithUserAgent(r.Header.Get("User-Agent"))

					// Get matched range for audit logging
					bypassRanges := m.bypassChecker.GetBypassRanges()
					for _, rangeStr := range bypassRanges {
						// Find which range matched (simplified - actual implementation might need to track this)
						entry = entry.WithMatchedRange(rangeStr)
						break
					}

					if err := m.bypassLogger.LogBypassAccess(entry); err != nil && m.enableLogging {
						log.Printf("Failed to log bypass access: %v", err)
					}
				}

				if shouldLog && m.enableLogging {
					log.Printf("Bypassing authentication for IP %s (matched bypass range)", clientIP)
				}

				// Skip all authentication and proceed to next handler
				next.ServeHTTP(w, r)
				return
			}
		}

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
			ctx := context.WithValue(r.Context(), userContextKey, tokenInfo)
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
