package mcpserver

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
)

// IPAuthMiddleware provides IP-based access control for MCP server
type IPAuthMiddleware struct {
	allowedIPs    []string
	allowedNets   []*net.IPNet
	enableLogging bool
}

// NewIPAuthMiddleware creates a new IP authentication middleware
func NewIPAuthMiddleware(allowedIPs []string, enableLogging bool) (*IPAuthMiddleware, error) {
	if len(allowedIPs) == 0 {
		return nil, fmt.Errorf("no allowed IPs specified")
	}

	middleware := &IPAuthMiddleware{
		allowedIPs:    allowedIPs,
		allowedNets:   make([]*net.IPNet, 0),
		enableLogging: enableLogging,
	}

	// Parse CIDR blocks and individual IPs
	for _, ipStr := range allowedIPs {
		ipStr = strings.TrimSpace(ipStr)
		if ipStr == "" {
			continue
		}

		// Check if it's a CIDR block
		if strings.Contains(ipStr, "/") {
			_, network, err := net.ParseCIDR(ipStr)
			if err != nil {
				return nil, fmt.Errorf("invalid CIDR block %s: %v", ipStr, err)
			}
			middleware.allowedNets = append(middleware.allowedNets, network)
		} else {
			// Individual IP address - convert to /32 or /128 CIDR
			ip := net.ParseIP(ipStr)
			if ip == nil {
				return nil, fmt.Errorf("invalid IP address: %s", ipStr)
			}

			var cidr string
			if ip.To4() != nil {
				cidr = ipStr + "/32"
			} else {
				cidr = ipStr + "/128"
			}

			_, network, err := net.ParseCIDR(cidr)
			if err != nil {
				return nil, fmt.Errorf("failed to create CIDR for IP %s: %v", ipStr, err)
			}
			middleware.allowedNets = append(middleware.allowedNets, network)
		}
	}

	if middleware.enableLogging {
		log.Printf("IP Auth Middleware initialized with %d allowed IP ranges", len(middleware.allowedNets))
	}

	return middleware, nil
}

// Middleware returns the HTTP middleware function
func (m *IPAuthMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always allow OAuth2 paths to pass through (handled by OIDC layer)
		if r.URL.Path == "/callback" || r.URL.Path == "/login" {
			next.ServeHTTP(w, r)
			return
		}
		clientIP := extractClientIPFromRequest(r)

		if !m.isIPAllowed(clientIP) {
			if m.enableLogging {
				log.Printf("Access denied for IP: %s (Path: %s, Method: %s, User-Agent: %s)",
					clientIP, r.URL.Path, r.Method, r.Header.Get("User-Agent"))
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			if _, err := w.Write([]byte(`{"error": {"code": -32603, "message": "Access denied: IP not authorized"}}`)); err != nil {
				log.Printf("Failed to write error response: %v", err)
			}
			return
		}

		if m.enableLogging {
			log.Printf("Access granted for IP: %s (Path: %s, Method: %s)",
				clientIP, r.URL.Path, r.Method)
		}

		ctx := context.WithValue(r.Context(), clientIPContextKey, clientIP)
		ctx = context.WithValue(ctx, authMethodContextKey, string(AuthMethodIP))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// extractClientIP extracts the real client IP from the HTTP request
func (m *IPAuthMiddleware) extractClientIP(r *http.Request) string {
	return extractClientIPFromRequest(r)
}

func extractClientIPFromRequest(r *http.Request) string {
	// Check X-Forwarded-For header first (for proxy/load balancer scenarios)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For can contain multiple IPs, use the first one
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			clientIP := strings.TrimSpace(ips[0])
			if clientIP != "" {
				return clientIP
			}
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fallback to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// If no port is present, use the whole string
		return r.RemoteAddr
	}
	return ip

}

// isIPAllowed checks if the given IP is in the allowed list
func (m *IPAuthMiddleware) isIPAllowed(ipStr string) bool {
	if ipStr == "" {
		return false
	}

	clientIP := net.ParseIP(ipStr)
	if clientIP == nil {
		if m.enableLogging {
			log.Printf("Failed to parse client IP: %s", ipStr)
		}
		return false
	}

	// Check against all allowed networks
	for _, network := range m.allowedNets {
		if network.Contains(clientIP) {
			return true
		}
	}

	return false
}

// GetAllowedIPs returns the list of allowed IP addresses/ranges
func (m *IPAuthMiddleware) GetAllowedIPs() []string {
	return m.allowedIPs
}

// UpdateAllowedIPs updates the allowed IP list (useful for dynamic updates)
func (m *IPAuthMiddleware) UpdateAllowedIPs(allowedIPs []string) error {
	newMiddleware, err := NewIPAuthMiddleware(allowedIPs, m.enableLogging)
	if err != nil {
		return err
	}

	m.allowedIPs = newMiddleware.allowedIPs
	m.allowedNets = newMiddleware.allowedNets

	if m.enableLogging {
		log.Printf("IP Auth Middleware updated with %d allowed IP ranges", len(m.allowedNets))
	}

	return nil
}

// IsIPAllowed provides a public method to check if an IP is allowed
func (m *IPAuthMiddleware) IsIPAllowed(ipStr string) bool {
	return m.isIPAllowed(ipStr)
}

// LogSecurityEvent logs a custom security event
func (m *IPAuthMiddleware) LogSecurityEvent(event, clientIP, details string) {
	if m.enableLogging {
		log.Printf("Security Event [%s]: IP=%s, Details=%s", event, clientIP, details)
	}
}

// Common IP ranges for convenience
var (
	// LocalhostIPs contains common localhost IP addresses
	LocalhostIPs = []string{"127.0.0.1", "::1"}

	// PrivateNetworkIPs contains RFC 1918 private network ranges
	PrivateNetworkIPs = []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
	}

	// DockerDefaultIPs contains common Docker network ranges
	DockerDefaultIPs = []string{
		"172.17.0.0/16",
		"172.18.0.0/16",
		"172.19.0.0/16",
	}
)
