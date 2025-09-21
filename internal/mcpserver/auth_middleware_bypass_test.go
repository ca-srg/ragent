package mcpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testSuccessHandler is a simple handler for testing that always returns OK
func testSuccessHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func TestUnifiedAuthMiddleware_BypassIP(t *testing.T) {
	tests := []struct {
		name           string
		config         *UnifiedAuthConfig
		clientIP       string
		xForwardedFor  string
		expectAllowed  bool
		expectBypass   bool
	}{
		{
			name: "bypass IP allowed - direct connection",
			config: &UnifiedAuthConfig{
				AuthMethod: AuthMethodIP,
				IPConfig: &IPAuthConfig{
					AllowedIPs:    []string{"192.168.1.0/24"},
					EnableLogging: false,
				},
				BypassConfig: &BypassIPConfig{
					BypassIPRanges: []string{"10.0.0.0/24"},
					VerboseLogging: false,
					AuditLogging:   true,
				},
			},
			clientIP:      "10.0.0.100",
			expectAllowed: true,
			expectBypass:  true,
		},
		{
			name: "bypass IP not in range - requires normal auth",
			config: &UnifiedAuthConfig{
				AuthMethod: AuthMethodIP,
				IPConfig: &IPAuthConfig{
					AllowedIPs:    []string{"192.168.1.0/24"},
					EnableLogging: false,
				},
				BypassConfig: &BypassIPConfig{
					BypassIPRanges: []string{"10.0.0.0/24"},
					VerboseLogging: false,
					AuditLogging:   true,
				},
			},
			clientIP:      "192.168.1.100",
			expectAllowed: true,
			expectBypass:  false,
		},
		{
			name: "bypass IP via proxy - with trusted proxy",
			config: &UnifiedAuthConfig{
				AuthMethod: AuthMethodIP,
				IPConfig: &IPAuthConfig{
					AllowedIPs:    []string{"192.168.1.0/24"},
					EnableLogging: false,
				},
				BypassConfig: &BypassIPConfig{
					BypassIPRanges: []string{"10.0.0.0/24"},
					TrustedProxies: []string{"127.0.0.1"},
					VerboseLogging: false,
					AuditLogging:   true,
				},
			},
			clientIP:      "127.0.0.1",
			xForwardedFor: "10.0.0.50",
			expectAllowed: true,
			expectBypass:  true,
		},
		{
			name: "bypass IP via proxy - untrusted proxy",
			config: &UnifiedAuthConfig{
				AuthMethod: AuthMethodIP,
				IPConfig: &IPAuthConfig{
					AllowedIPs:    []string{"10.0.0.0/24"}, // Allow the forwarded IP range
					EnableLogging: false,
				},
				BypassConfig: &BypassIPConfig{
					BypassIPRanges: []string{"10.0.0.0/24"},
					TrustedProxies: []string{"192.168.1.1"}, // 127.0.0.1 is not trusted
					VerboseLogging: false,
					AuditLogging:   true,
				},
			},
			clientIP:      "127.0.0.1",
			xForwardedFor: "10.0.0.50",
			expectAllowed: true, // IPAuth will still trust X-Forwarded-For
			expectBypass:  false, // Bypass won't trust untrusted proxy
		},
		{
			name: "no bypass configured - normal auth",
			config: &UnifiedAuthConfig{
				AuthMethod: AuthMethodIP,
				IPConfig: &IPAuthConfig{
					AllowedIPs:    []string{"192.168.1.0/24"},
					EnableLogging: false,
				},
			},
			clientIP:      "192.168.1.100",
			expectAllowed: true,
			expectBypass:  false,
		},
		{
			name: "multiple bypass ranges",
			config: &UnifiedAuthConfig{
				AuthMethod: AuthMethodIP,
				IPConfig: &IPAuthConfig{
					AllowedIPs:    []string{"8.8.8.8/32"},
					EnableLogging: false,
				},
				BypassConfig: &BypassIPConfig{
					BypassIPRanges: []string{"10.0.0.0/24", "172.16.0.0/16", "192.168.0.0/16"},
					VerboseLogging: false,
					AuditLogging:   true,
				},
			},
			clientIP:      "172.16.10.5",
			expectAllowed: true,
			expectBypass:  true,
		},
		{
			name: "IPv6 bypass",
			config: &UnifiedAuthConfig{
				AuthMethod: AuthMethodIP,
				IPConfig: &IPAuthConfig{
					AllowedIPs:    []string{"2001:db8::/32"},
					EnableLogging: false,
				},
				BypassConfig: &BypassIPConfig{
					BypassIPRanges: []string{"2001:db8::/64"},
					VerboseLogging: false,
					AuditLogging:   true,
				},
			},
			clientIP:      "[2001:db8::1]",
			expectAllowed: true,
			expectBypass:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create middleware
			middleware, err := NewUnifiedAuthMiddleware(tt.config)
			if err != nil {
				t.Fatalf("Failed to create middleware: %v", err)
			}

			// Create test handler
			handler := http.HandlerFunc(testSuccessHandler)
			wrappedHandler := middleware.Middleware(handler)

			// Create test request
			req := httptest.NewRequest("GET", "/api/test", nil)

			// Set client IP
			if strings.Contains(tt.clientIP, "[") {
				// IPv6 address
				req.RemoteAddr = tt.clientIP + ":12345"
			} else {
				req.RemoteAddr = tt.clientIP + ":12345"
			}

			// Set X-Forwarded-For header if specified
			if tt.xForwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.xForwardedFor)
			}

			// Create response recorder
			rr := httptest.NewRecorder()

			// Execute request
			wrappedHandler.ServeHTTP(rr, req)

			// Check response
			if tt.expectAllowed {
				if rr.Code != http.StatusOK {
					t.Errorf("Expected status 200, got %d", rr.Code)
				}
			} else {
				if rr.Code == http.StatusOK {
					t.Errorf("Expected authentication failure, but got status 200")
				}
			}
		})
	}
}

func TestUnifiedAuthMiddleware_BypassConfigValidation(t *testing.T) {
	tests := []struct {
		name      string
		config    *UnifiedAuthConfig
		wantError bool
		errorMsg  string
	}{
		{
			name: "bypass with either auth method - should fail",
			config: &UnifiedAuthConfig{
				AuthMethod: AuthMethodEither,
				IPConfig: &IPAuthConfig{
					AllowedIPs: []string{"192.168.1.0/24"},
				},
				OIDCConfig: &OIDCConfig{
					Issuer:       "https://example.com",
					ClientID:     "test-client",
					ClientSecret: "test-secret",
				},
				BypassConfig: &BypassIPConfig{
					BypassIPRanges: []string{"10.0.0.0/24"},
				},
			},
			wantError: true,
			errorMsg:  "bypass authentication cannot be used with 'either' auth method",
		},
		{
			name: "bypass with invalid CIDR",
			config: &UnifiedAuthConfig{
				AuthMethod: AuthMethodIP,
				IPConfig: &IPAuthConfig{
					AllowedIPs: []string{"192.168.1.0/24"},
				},
				BypassConfig: &BypassIPConfig{
					BypassIPRanges: []string{"10.0.0.0/33"},
				},
			},
			wantError: true,
			errorMsg:  "failed to add bypass IP range",
		},
		{
			name: "valid bypass configuration",
			config: &UnifiedAuthConfig{
				AuthMethod: AuthMethodIP,
				IPConfig: &IPAuthConfig{
					AllowedIPs: []string{"192.168.1.0/24"},
				},
				BypassConfig: &BypassIPConfig{
					BypassIPRanges: []string{"10.0.0.0/24"},
				},
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewUnifiedAuthMiddleware(tt.config)
			if (err != nil) != tt.wantError {
				t.Errorf("NewUnifiedAuthMiddleware() error = %v, wantError %v", err, tt.wantError)
			}
			if err != nil && tt.errorMsg != "" {
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Error message should contain %q, got %q", tt.errorMsg, err.Error())
				}
			}
		})
	}
}

func TestUnifiedAuthMiddleware_BypassWithNormalAuth(t *testing.T) {
	// Test that non-bypass IPs still require normal authentication
	config := &UnifiedAuthConfig{
		AuthMethod: AuthMethodIP,
		IPConfig: &IPAuthConfig{
			AllowedIPs:    []string{"192.168.1.0/24"},
			EnableLogging: false,
		},
		BypassConfig: &BypassIPConfig{
			BypassIPRanges: []string{"10.0.0.0/24"},
			VerboseLogging: false,
			AuditLogging:   false,
		},
	}

	middleware, err := NewUnifiedAuthMiddleware(config)
	if err != nil {
		t.Fatalf("Failed to create middleware: %v", err)
	}

	handler := http.HandlerFunc(testSuccessHandler)
	wrappedHandler := middleware.Middleware(handler)

	// Test cases
	tests := []struct {
		name          string
		clientIP      string
		expectStatus  int
	}{
		{
			name:         "bypass IP - allowed without auth",
			clientIP:     "10.0.0.100",
			expectStatus: http.StatusOK,
		},
		{
			name:         "allowed IP - normal auth works",
			clientIP:     "192.168.1.100",
			expectStatus: http.StatusOK,
		},
		{
			name:         "non-allowed IP - denied",
			clientIP:     "8.8.8.8",
			expectStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/test", nil)
			req.RemoteAddr = tt.clientIP + ":12345"
			rr := httptest.NewRecorder()

			wrappedHandler.ServeHTTP(rr, req)

			if rr.Code != tt.expectStatus {
				t.Errorf("Expected status %d, got %d", tt.expectStatus, rr.Code)
			}
		})
	}
}

func TestUnifiedAuthMiddleware_ProxyChainHandling(t *testing.T) {
	config := &UnifiedAuthConfig{
		AuthMethod: AuthMethodIP,
		IPConfig: &IPAuthConfig{
			AllowedIPs:    []string{"192.168.1.0/24"},
			EnableLogging: false,
		},
		BypassConfig: &BypassIPConfig{
			BypassIPRanges: []string{"10.0.0.0/24"},
			TrustedProxies: []string{"127.0.0.1", "192.168.1.1"},
			VerboseLogging: false,
			AuditLogging:   false,
		},
	}

	middleware, err := NewUnifiedAuthMiddleware(config)
	if err != nil {
		t.Fatalf("Failed to create middleware: %v", err)
	}

	handler := http.HandlerFunc(testSuccessHandler)
	wrappedHandler := middleware.Middleware(handler)

	tests := []struct {
		name          string
		clientIP      string
		xForwardedFor string
		expectStatus  int
	}{
		{
			name:          "trusted proxy with bypass IP",
			clientIP:      "127.0.0.1",
			xForwardedFor: "10.0.0.50",
			expectStatus:  http.StatusOK,
		},
		{
			name:          "trusted proxy with non-bypass IP",
			clientIP:      "127.0.0.1",
			xForwardedFor: "8.8.8.8",
			expectStatus:  http.StatusForbidden,
		},
		{
			name:          "untrusted proxy with bypass IP in header",
			clientIP:      "8.8.8.8",
			xForwardedFor: "10.0.0.50",
			expectStatus:  http.StatusForbidden, // Should not trust X-Forwarded-For
		},
		{
			name:          "proxy chain with bypass IP",
			clientIP:      "127.0.0.1",
			xForwardedFor: "10.0.0.50", // Single IP - proxy chain not fully supported
			expectStatus:  http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/test", nil)
			req.RemoteAddr = tt.clientIP + ":12345"
			if tt.xForwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.xForwardedFor)
			}
			rr := httptest.NewRecorder()

			wrappedHandler.ServeHTTP(rr, req)

			if rr.Code != tt.expectStatus {
				t.Errorf("Expected status %d, got %d", tt.expectStatus, rr.Code)
			}
		})
	}
}

func TestUnifiedAuthMiddleware_NoBypassConfig(t *testing.T) {
	// Test that middleware works correctly without bypass configuration
	config := &UnifiedAuthConfig{
		AuthMethod: AuthMethodIP,
		IPConfig: &IPAuthConfig{
			AllowedIPs:    []string{"192.168.1.0/24"},
			EnableLogging: false,
		},
		// No BypassConfig
	}

	middleware, err := NewUnifiedAuthMiddleware(config)
	if err != nil {
		t.Fatalf("Failed to create middleware: %v", err)
	}

	handler := http.HandlerFunc(testSuccessHandler)
	wrappedHandler := middleware.Middleware(handler)

	tests := []struct {
		name         string
		clientIP     string
		expectStatus int
	}{
		{
			name:         "allowed IP",
			clientIP:     "192.168.1.100",
			expectStatus: http.StatusOK,
		},
		{
			name:         "non-allowed IP",
			clientIP:     "10.0.0.100",
			expectStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/test", nil)
			req.RemoteAddr = tt.clientIP + ":12345"
			rr := httptest.NewRecorder()

			wrappedHandler.ServeHTTP(rr, req)

			if rr.Code != tt.expectStatus {
				t.Errorf("Expected status %d, got %d", tt.expectStatus, rr.Code)
			}
		})
	}
}

func BenchmarkUnifiedAuthMiddleware_BypassCheck(b *testing.B) {
	config := &UnifiedAuthConfig{
		AuthMethod: AuthMethodIP,
		IPConfig: &IPAuthConfig{
			AllowedIPs:    []string{"192.168.1.0/24"},
			EnableLogging: false,
		},
		BypassConfig: &BypassIPConfig{
			BypassIPRanges: []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"},
			VerboseLogging: false,
			AuditLogging:   false,
		},
	}

	middleware, _ := NewUnifiedAuthMiddleware(config)
	handler := http.HandlerFunc(testSuccessHandler)
	wrappedHandler := middleware.Middleware(handler)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "10.0.0.100:12345"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rr := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(rr, req)
	}
}