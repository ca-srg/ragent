package mcpserver

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// IPAuthMiddlewareAdapter adapts RAGent's IP authentication middleware for SDK server integration
// Maintains identical security behavior and logging while providing SDK-compatible middleware
type IPAuthMiddlewareAdapter struct {
	ipAuth *IPAuthMiddleware
	logger *log.Logger
}

// NewIPAuthMiddlewareAdapter creates a new IP authentication middleware adapter for SDK integration
func NewIPAuthMiddlewareAdapter(allowedIPs []string, enableLogging bool) (*IPAuthMiddlewareAdapter, error) {
	// Create the existing IP authentication middleware
	ipAuth, err := NewIPAuthMiddleware(allowedIPs, enableLogging)
	if err != nil {
		return nil, fmt.Errorf("failed to create IP auth middleware: %w", err)
	}

	adapter := &IPAuthMiddlewareAdapter{
		ipAuth: ipAuth,
		logger: log.New(log.Writer(), "[IPAuthMiddlewareAdapter] ", log.LstdFlags),
	}

	if enableLogging {
		adapter.logger.Printf("IP Auth Middleware Adapter initialized with %d allowed IP ranges", len(allowedIPs))
	}

	return adapter, nil
}

// NewIPAuthMiddlewareAdapterFromExisting creates an adapter from an existing IP authentication middleware
func NewIPAuthMiddlewareAdapterFromExisting(ipAuth *IPAuthMiddleware) *IPAuthMiddlewareAdapter {
	if ipAuth == nil {
		return nil
	}

	return &IPAuthMiddlewareAdapter{
		ipAuth: ipAuth,
		logger: log.New(log.Writer(), "[IPAuthMiddlewareAdapter] ", log.LstdFlags),
	}
}

// ToSDKMiddleware converts the IP authentication middleware to SDK-compatible middleware
// This is the primary method for integrating with the SDK server
func (adapter *IPAuthMiddlewareAdapter) ToSDKMiddleware() mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			// Extract client IP from request context or headers if available
			clientIP := adapter.extractClientIPFromContext(ctx)

			// If we couldn't extract IP from context, allow request to proceed
			// This maintains compatibility with different transport types (stdio, etc.)
			if clientIP == "" {
				if adapter.ipAuth.enableLogging {
					adapter.logger.Printf("No client IP available for method %s - allowing request (non-HTTP transport)", method)
				}
				return next(ctx, method, req)
			}

			// Check if the IP is allowed using existing validation logic
			if !adapter.ipAuth.IsIPAllowed(clientIP) {
				if adapter.ipAuth.enableLogging {
					adapter.logger.Printf("Access denied for IP: %s (Method: %s)", clientIP, method)
				}

				// Return standard error for unauthorized access
				// The SDK will handle the appropriate MCP protocol error formatting
				return nil, fmt.Errorf("access denied: IP not authorized")
			}

			if adapter.ipAuth.enableLogging {
				adapter.logger.Printf("Access granted for IP: %s (Method: %s)", clientIP, method)
			}

			// IP is authorized, proceed with the request
			return next(ctx, method, req)
		}
	}
}

// extractClientIPFromContext extracts client IP from the request context
// This handles different transport types and maintains compatibility
func (adapter *IPAuthMiddlewareAdapter) extractClientIPFromContext(ctx context.Context) string {
	// Try to get HTTP request from context (for HTTP transports like SSE)
	if req, ok := ctx.Value(http.Request{}).(*http.Request); ok {
		return adapter.ipAuth.extractClientIP(req)
	}

	// Try to get client IP directly from context (if transport provides it)
	if clientIP, ok := ctx.Value(clientIPContextKey).(string); ok {
		return clientIP
	}

	// Try alternative context keys that transports might use
	if clientAddr, ok := ctx.Value("remote_addr").(string); ok {
		return clientAddr
	}

	// For other transport types (stdio, etc.), no IP validation is possible
	// This maintains compatibility while providing security where applicable
	return ""
}

// GetIPAuthMiddleware returns the underlying IP authentication middleware
// This maintains API compatibility and allows access to existing functionality
func (adapter *IPAuthMiddlewareAdapter) GetIPAuthMiddleware() *IPAuthMiddleware {
	return adapter.ipAuth
}

// GetAllowedIPs returns the list of allowed IP addresses/ranges
// Delegates to the existing IP authentication middleware
func (adapter *IPAuthMiddlewareAdapter) GetAllowedIPs() []string {
	return adapter.ipAuth.GetAllowedIPs()
}

// UpdateAllowedIPs updates the allowed IP list
// Delegates to the existing IP authentication middleware
func (adapter *IPAuthMiddlewareAdapter) UpdateAllowedIPs(allowedIPs []string) error {
	err := adapter.ipAuth.UpdateAllowedIPs(allowedIPs)
	if err != nil {
		return err
	}

	if adapter.ipAuth.enableLogging {
		adapter.logger.Printf("IP Auth Middleware Adapter updated with %d allowed IP ranges", len(allowedIPs))
	}

	return nil
}

// IsIPAllowed checks if the given IP is in the allowed list
// Delegates to the existing IP authentication middleware
func (adapter *IPAuthMiddlewareAdapter) IsIPAllowed(ipStr string) bool {
	return adapter.ipAuth.IsIPAllowed(ipStr)
}

// LogSecurityEvent logs a custom security event
// Delegates to the existing IP authentication middleware
func (adapter *IPAuthMiddlewareAdapter) LogSecurityEvent(event, clientIP, details string) {
	adapter.ipAuth.LogSecurityEvent(event, clientIP, details)
}

// SetLogger sets a custom logger for the adapter
func (adapter *IPAuthMiddlewareAdapter) SetLogger(logger *log.Logger) {
	if logger != nil {
		adapter.logger = logger
	}
}

// HTTPMiddleware returns the original HTTP middleware for non-SDK HTTP servers
// This maintains compatibility with existing HTTP-based implementations
func (adapter *IPAuthMiddlewareAdapter) HTTPMiddleware() func(http.Handler) http.Handler {
	return adapter.ipAuth.Middleware
}

// Common middleware creation functions for convenience

// NewLocalhostOnlyMiddleware creates middleware that only allows localhost connections
func NewLocalhostOnlyMiddleware(enableLogging bool) (*IPAuthMiddlewareAdapter, error) {
	return NewIPAuthMiddlewareAdapter(LocalhostIPs, enableLogging)
}

// NewPrivateNetworkMiddleware creates middleware that allows private network connections
func NewPrivateNetworkMiddleware(enableLogging bool) (*IPAuthMiddlewareAdapter, error) {
	allowedIPs := make([]string, 0, len(LocalhostIPs)+len(PrivateNetworkIPs))
	allowedIPs = append(allowedIPs, LocalhostIPs...)
	allowedIPs = append(allowedIPs, PrivateNetworkIPs...)

	return NewIPAuthMiddlewareAdapter(allowedIPs, enableLogging)
}

// NewDockerFriendlyMiddleware creates middleware that allows Docker network connections
func NewDockerFriendlyMiddleware(enableLogging bool) (*IPAuthMiddlewareAdapter, error) {
	allowedIPs := make([]string, 0, len(LocalhostIPs)+len(PrivateNetworkIPs)+len(DockerDefaultIPs))
	allowedIPs = append(allowedIPs, LocalhostIPs...)
	allowedIPs = append(allowedIPs, PrivateNetworkIPs...)
	allowedIPs = append(allowedIPs, DockerDefaultIPs...)

	return NewIPAuthMiddlewareAdapter(allowedIPs, enableLogging)
}
