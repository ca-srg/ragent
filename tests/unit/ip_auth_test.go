package unit

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ca-srg/ragent/internal/mcpserver"
)

func TestNewIPAuthMiddleware(t *testing.T) {
	tests := []struct {
		name        string
		allowedIPs  []string
		expectError bool
	}{
		{
			name:        "valid IPv4 addresses",
			allowedIPs:  []string{"192.168.1.1", "10.0.0.1"},
			expectError: false,
		},
		{
			name:        "valid IPv6 addresses",
			allowedIPs:  []string{"::1", "2001:db8::1"},
			expectError: false,
		},
		{
			name:        "valid CIDR ranges",
			allowedIPs:  []string{"192.168.1.0/24", "10.0.0.0/8"},
			expectError: false,
		},
		{
			name:        "mixed IPv4 and IPv6",
			allowedIPs:  []string{"127.0.0.1", "::1", "192.168.0.0/16"},
			expectError: false,
		},
		{
			name:        "empty allowed IPs",
			allowedIPs:  []string{},
			expectError: true,
		},
		{
			name:        "invalid IP address",
			allowedIPs:  []string{"256.256.256.256"},
			expectError: true,
		},
		{
			name:        "invalid CIDR range",
			allowedIPs:  []string{"192.168.1.0/33"},
			expectError: true,
		},
		{
			name:        "malformed IP",
			allowedIPs:  []string{"not.an.ip"},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := mcpserver.NewIPAuthMiddleware(tt.allowedIPs, false)
			if (err != nil) != tt.expectError {
				t.Errorf("NewIPAuthMiddleware() error = %v, expectError %v", err, tt.expectError)
			}
		})
	}
}

func TestIPAuthMiddleware_IsIPAllowed(t *testing.T) {
	allowedIPs := []string{
		"127.0.0.1",      // localhost IPv4
		"::1",            // localhost IPv6
		"192.168.1.0/24", // private network range
		"10.0.0.1",       // specific private IP
		"2001:db8::/32",  // IPv6 range
	}

	middleware, err := mcpserver.NewIPAuthMiddleware(allowedIPs, false)
	if err != nil {
		t.Fatalf("Failed to create middleware: %v", err)
	}

	tests := []struct {
		name     string
		ip       string
		expected bool
	}{
		// Allowed IPs
		{"localhost IPv4", "127.0.0.1", true},
		{"localhost IPv6", "::1", true},
		{"private network IP in range", "192.168.1.100", true},
		{"private network IP at start", "192.168.1.0", true},
		{"private network IP at end", "192.168.1.255", true},
		{"specific allowed private IP", "10.0.0.1", true},
		{"IPv6 in range", "2001:db8::1", true},
		{"IPv6 in range expanded", "2001:db8:0000:0000:0000:0000:0000:0001", true},

		// Denied IPs
		{"private IP outside range", "192.168.2.1", false},
		{"different private IP", "10.0.0.2", false},
		{"public IPv4", "8.8.8.8", false},
		{"public IPv4 Google DNS", "8.8.4.4", false},
		{"public IPv6", "2001:4860:4860::8888", false},
		{"IPv6 outside range", "2001:db9::1", false},

		// Edge cases
		{"empty string", "", false},
		{"invalid IP", "not-an-ip", false},
		{"malformed IPv4", "999.999.999.999", false},
		{"malformed IPv6", "::gggg", false},
		{"partial IPv4", "192.168", false},
		{"IPv4 with port", "192.168.1.1:8080", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := middleware.IsIPAllowed(tt.ip)
			if result != tt.expected {
				t.Errorf("IsIPAllowed(%s) = %v, want %v", tt.ip, result, tt.expected)
			}
		})
	}
}

func TestIPAuthMiddleware_HTTPBehavior(t *testing.T) {
	allowedIPs := []string{"127.0.0.1", "192.168.1.0/24"}
	middleware, err := mcpserver.NewIPAuthMiddleware(allowedIPs, false)
	if err != nil {
		t.Fatalf("Failed to create middleware: %v", err)
	}

	// Create a test handler that should only be called for allowed IPs
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("success")); err != nil {
			t.Fatalf("failed to write response: %v", err)
		}
	})

	wrappedHandler := middleware.Middleware(testHandler)

	tests := []struct {
		name           string
		remoteAddr     string
		xForwardedFor  string
		xRealIP        string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "allowed IP via RemoteAddr",
			remoteAddr:     "127.0.0.1:12345",
			expectedStatus: http.StatusOK,
			expectedBody:   "success",
		},
		{
			name:           "allowed IP in range via RemoteAddr",
			remoteAddr:     "192.168.1.100:8080",
			expectedStatus: http.StatusOK,
			expectedBody:   "success",
		},
		{
			name:           "denied IP via RemoteAddr",
			remoteAddr:     "8.8.8.8:80",
			expectedStatus: http.StatusForbidden,
			expectedBody:   `{"error": {"code": -32603, "message": "Access denied: IP not authorized"}}`,
		},
		{
			name:           "allowed IP via X-Forwarded-For",
			remoteAddr:     "8.8.8.8:80",         // This would be denied
			xForwardedFor:  "127.0.0.1, 8.8.8.8", // But X-Forwarded-For has allowed IP
			expectedStatus: http.StatusOK,
			expectedBody:   "success",
		},
		{
			name:           "denied IP via X-Forwarded-For",
			remoteAddr:     "192.168.1.1:80", // This would be allowed
			xForwardedFor:  "8.8.8.8",        // But X-Forwarded-For has denied IP (takes precedence)
			expectedStatus: http.StatusForbidden,
			expectedBody:   `{"error": {"code": -32603, "message": "Access denied: IP not authorized"}}`,
		},
		{
			name:           "allowed IP via X-Real-IP",
			remoteAddr:     "8.8.8.8:80", // This would be denied
			xRealIP:        "127.0.0.1",  // But X-Real-IP has allowed IP
			expectedStatus: http.StatusOK,
			expectedBody:   "success",
		},
		{
			name:           "X-Forwarded-For takes precedence over X-Real-IP",
			remoteAddr:     "8.8.8.8:80",
			xForwardedFor:  "127.0.0.1", // Allowed
			xRealIP:        "8.8.4.4",   // Would be denied if checked
			expectedStatus: http.StatusOK,
			expectedBody:   "success",
		},
		{
			name:           "malformed RemoteAddr",
			remoteAddr:     "malformed-address",
			expectedStatus: http.StatusForbidden,
			expectedBody:   `{"error": {"code": -32603, "message": "Access denied: IP not authorized"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/", strings.NewReader("{}"))
			req.RemoteAddr = tt.remoteAddr
			if tt.xForwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.xForwardedFor)
			}
			if tt.xRealIP != "" {
				req.Header.Set("X-Real-IP", tt.xRealIP)
			}

			rr := httptest.NewRecorder()
			wrappedHandler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, rr.Code)
			}

			body := strings.TrimSpace(rr.Body.String())
			if body != tt.expectedBody {
				t.Errorf("Expected body %q, got %q", tt.expectedBody, body)
			}
		})
	}
}

func TestIPAuthMiddleware_SecurityEdgeCases(t *testing.T) {
	allowedIPs := []string{"127.0.0.1"}
	middleware, err := mcpserver.NewIPAuthMiddleware(allowedIPs, false)
	if err != nil {
		t.Fatalf("Failed to create middleware: %v", err)
	}

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrappedHandler := middleware.Middleware(testHandler)

	securityTests := []struct {
		name           string
		setupRequest   func(*http.Request)
		expectedStatus int
		description    string
	}{
		{
			name: "IP spoofing attempt via multiple X-Forwarded-For",
			setupRequest: func(req *http.Request) {
				req.RemoteAddr = "8.8.8.8:80"
				req.Header.Set("X-Forwarded-For", "127.0.0.1")
				req.Header.Add("X-Forwarded-For", "8.8.8.8") // Attempt to add second header
			},
			expectedStatus: http.StatusOK, // Should use first X-Forwarded-For
			description:    "Should use first X-Forwarded-For header value",
		},
		{
			name: "Injection attempt in X-Forwarded-For",
			setupRequest: func(req *http.Request) {
				req.RemoteAddr = "8.8.8.8:80"
				req.Header.Set("X-Forwarded-For", "127.0.0.1\r\nHost: evil.com")
			},
			expectedStatus: http.StatusOK, // Should extract just the IP part
			description:    "Should handle injection attempts in headers",
		},
		{
			name: "Empty X-Forwarded-For fallback",
			setupRequest: func(req *http.Request) {
				req.RemoteAddr = "127.0.0.1:80"
				req.Header.Set("X-Forwarded-For", "")
			},
			expectedStatus: http.StatusOK,
			description:    "Should fallback to RemoteAddr when X-Forwarded-For is empty",
		},
		{
			name: "Whitespace in headers",
			setupRequest: func(req *http.Request) {
				req.RemoteAddr = "8.8.8.8:80"
				req.Header.Set("X-Forwarded-For", "  127.0.0.1  ")
			},
			expectedStatus: http.StatusOK,
			description:    "Should handle whitespace in IP headers",
		},
		{
			name: "IPv6 in X-Forwarded-For",
			setupRequest: func(req *http.Request) {
				req.RemoteAddr = "8.8.8.8:80"
				req.Header.Set("X-Forwarded-For", "::1")
			},
			expectedStatus: http.StatusForbidden, // ::1 not in allowed list
			description:    "Should handle IPv6 addresses in X-Forwarded-For",
		},
	}

	for _, tt := range securityTests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/", strings.NewReader("{}"))
			tt.setupRequest(req)

			rr := httptest.NewRecorder()
			wrappedHandler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("%s: Expected status %d, got %d", tt.description, tt.expectedStatus, rr.Code)
			}
		})
	}
}

func TestIPAuthMiddleware_UpdateAllowedIPs(t *testing.T) {
	initialIPs := []string{"127.0.0.1"}
	middleware, err := mcpserver.NewIPAuthMiddleware(initialIPs, false)
	if err != nil {
		t.Fatalf("Failed to create middleware: %v", err)
	}

	// Test initial state
	if !middleware.IsIPAllowed("127.0.0.1") {
		t.Error("127.0.0.1 should be allowed initially")
	}
	if middleware.IsIPAllowed("192.168.1.1") {
		t.Error("192.168.1.1 should not be allowed initially")
	}

	// Update allowed IPs
	newIPs := []string{"192.168.1.1", "10.0.0.0/8"}
	err = middleware.UpdateAllowedIPs(newIPs)
	if err != nil {
		t.Fatalf("Failed to update allowed IPs: %v", err)
	}

	// Test updated state
	if middleware.IsIPAllowed("127.0.0.1") {
		t.Error("127.0.0.1 should not be allowed after update")
	}
	if !middleware.IsIPAllowed("192.168.1.1") {
		t.Error("192.168.1.1 should be allowed after update")
	}
	if !middleware.IsIPAllowed("10.0.0.100") {
		t.Error("10.0.0.100 should be allowed after update (in 10.0.0.0/8 range)")
	}

	// Test invalid update
	invalidIPs := []string{"invalid.ip"}
	err = middleware.UpdateAllowedIPs(invalidIPs)
	if err == nil {
		t.Error("UpdateAllowedIPs should fail with invalid IP")
	}

	// Ensure state didn't change after failed update
	if !middleware.IsIPAllowed("192.168.1.1") {
		t.Error("192.168.1.1 should still be allowed after failed update")
	}
}

func TestIPAuthMiddleware_GetAllowedIPs(t *testing.T) {
	allowedIPs := []string{"127.0.0.1", "192.168.1.0/24", "::1"}
	middleware, err := mcpserver.NewIPAuthMiddleware(allowedIPs, false)
	if err != nil {
		t.Fatalf("Failed to create middleware: %v", err)
	}

	retrievedIPs := middleware.GetAllowedIPs()

	if len(retrievedIPs) != len(allowedIPs) {
		t.Errorf("Expected %d allowed IPs, got %d", len(allowedIPs), len(retrievedIPs))
	}

	// Check that all original IPs are present (order might differ)
	for _, originalIP := range allowedIPs {
		found := false
		for _, retrievedIP := range retrievedIPs {
			if originalIP == retrievedIP {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected IP %s not found in retrieved IPs", originalIP)
		}
	}
}

func TestIPAuthMiddleware_ConcurrentAccess(t *testing.T) {
	allowedIPs := []string{"127.0.0.1", "192.168.1.0/24"}
	middleware, err := mcpserver.NewIPAuthMiddleware(allowedIPs, false)
	if err != nil {
		t.Fatalf("Failed to create middleware: %v", err)
	}

	// Test concurrent access to IsIPAllowed method
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				middleware.IsIPAllowed("127.0.0.1")
				middleware.IsIPAllowed("8.8.8.8")
				middleware.IsIPAllowed("192.168.1.100")
			}
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// Test should complete without data races or panics
}

// Benchmark tests
func BenchmarkIPAuthMiddleware_IsIPAllowed(b *testing.B) {
	allowedIPs := []string{"127.0.0.1", "192.168.1.0/24", "10.0.0.0/8", "::1"}
	middleware, err := mcpserver.NewIPAuthMiddleware(allowedIPs, false)
	if err != nil {
		b.Fatalf("Failed to create middleware: %v", err)
	}

	testIPs := []string{"127.0.0.1", "192.168.1.100", "10.0.0.50", "8.8.8.8"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ip := testIPs[i%len(testIPs)]
		middleware.IsIPAllowed(ip)
	}
}
