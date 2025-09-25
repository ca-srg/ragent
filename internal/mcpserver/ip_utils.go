package mcpserver

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
)

// ExtractClientIP extracts the client IP from the HTTP request
// It supports X-Forwarded-For and X-Real-IP headers with trusted proxy validation
func ExtractClientIP(r interface {
	Header(string) string
	RemoteAddr() string
}, trustedProxies []string) string {
	// Helper function to check if an IP is in trusted proxies list
	isTrustedProxy := func(ip string) bool {
		if len(trustedProxies) == 0 {
			// If no trusted proxies configured, trust all proxies (backward compatibility)
			return true
		}

		parsedIP := net.ParseIP(ip)
		if parsedIP == nil {
			return false
		}

		for _, trusted := range trustedProxies {
			// Check if it's a CIDR
			if strings.Contains(trusted, "/") {
				_, network, err := net.ParseCIDR(trusted)
				if err == nil && network.Contains(parsedIP) {
					return true
				}
			} else {
				// Single IP comparison
				trustedIP := net.ParseIP(trusted)
				if trustedIP != nil && trustedIP.Equal(parsedIP) {
					return true
				}
			}
		}
		return false
	}

	// Get the direct connection IP
	remoteAddr := r.RemoteAddr()
	directIP, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// If no port is present, use the whole string
		directIP = remoteAddr
	}

	// Only trust proxy headers if the direct connection is from a trusted proxy
	if isTrustedProxy(directIP) {
		// Check X-Forwarded-For header first (for proxy/load balancer scenarios)
		if xff := r.Header("X-Forwarded-For"); xff != "" {
			// X-Forwarded-For can contain multiple IPs
			// Format: client, proxy1, proxy2, ...
			ips := strings.Split(xff, ",")

			// If we have a list of trusted proxies, validate the chain
			if len(trustedProxies) > 0 && len(ips) > 1 {
				// Walk the chain from right to left to find the first untrusted IP
				for i := len(ips) - 1; i >= 0; i-- {
					ip := strings.TrimSpace(ips[i])
					if ip != "" && !isTrustedProxy(ip) {
						// This is the first untrusted IP (the client)
						return ip
					}
				}
			}

			// Fallback to the first IP in the chain
			if len(ips) > 0 {
				clientIP := strings.TrimSpace(ips[0])
				if clientIP != "" {
					return clientIP
				}
			}
		}

		// Check X-Real-IP header
		if xri := r.Header("X-Real-IP"); xri != "" {
			return strings.TrimSpace(xri)
		}
	}

	// Return the direct connection IP
	return directIP
}

// HTTPRequest is a minimal interface for HTTP requests
type HTTPRequest interface {
	Header(string) string
	RemoteAddr() string
}

// HTTPRequestAdapter adapts http.Request to our interface
type HTTPRequestAdapter struct {
	R *http.Request
}

func (a *HTTPRequestAdapter) Header(key string) string {
	return a.R.Header.Get(key)
}

func (a *HTTPRequestAdapter) RemoteAddr() string {
	return a.R.RemoteAddr
}

// ExtractClientIPFromRequest is a convenience function for standard http.Request
func ExtractClientIPFromRequest(r *http.Request, trustedProxies []string) string {
	adapter := &HTTPRequestAdapter{R: r}
	return ExtractClientIP(adapter, trustedProxies)
}

// ValidateIPFormat checks if the given string is a valid IP address
func ValidateIPFormat(ipStr string) bool {
	return net.ParseIP(ipStr) != nil
}

// ParseCIDROrIP parses a string as either CIDR notation or a single IP
// Returns the parsed network and nil error on success
func ParseCIDROrIP(s string) (*net.IPNet, error) {
	// Try parsing as CIDR first
	_, network, err := net.ParseCIDR(s)
	if err == nil {
		return network, nil
	}

	// Try parsing as single IP
	ip := net.ParseIP(s)
	if ip == nil {
		return nil, fmt.Errorf("invalid IP address or CIDR notation: %s", s)
	}

	// Convert single IP to CIDR
	if ip.To4() != nil {
		_, network, err = net.ParseCIDR(fmt.Sprintf("%s/32", ip.String()))
	} else {
		_, network, err = net.ParseCIDR(fmt.Sprintf("%s/128", ip.String()))
	}

	return network, err
}

// LogIPEvent logs IP-related events with consistent formatting
func LogIPEvent(event, ip, details string, verbose bool) {
	if verbose {
		log.Printf("[IP-%s] IP=%s, Details=%s", event, ip, details)
	}
}
