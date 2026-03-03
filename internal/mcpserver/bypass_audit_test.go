package mcpserver

import (
	"bytes"
	"encoding/json"
	"log"
	"strings"
	"testing"
	"time"
)

func TestNewBypassAuditLogger(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
	}{
		{
			name:    "enabled logger",
			enabled: true,
		},
		{
			name:    "disabled logger",
			enabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := NewBypassAuditLogger(tt.enabled)
			if logger == nil {
				t.Fatal("NewBypassAuditLogger() returned nil")
			}
			if logger.enabled != tt.enabled {
				t.Errorf("NewBypassAuditLogger() enabled = %v, want %v", logger.enabled, tt.enabled)
			}
		})
	}
}

func TestBypassAuditLoggerImpl_LogBypassAccess(t *testing.T) {
	tests := []struct {
		name           string
		enabled        bool
		entry          BypassAuditEntry
		wantLog        bool
		wantSuccess    bool
		validateFields []string
	}{
		{
			name:    "enabled logger with basic entry",
			enabled: true,
			entry: BypassAuditEntry{
				IP:           "10.0.0.1",
				Method:       "GET",
				Path:         "/api/data",
				MatchedRange: "10.0.0.0/24",
			},
			wantLog:        true,
			wantSuccess:    true,
			validateFields: []string{"ip", "method", "path", "matched_range", "success", "timestamp"},
		},
		{
			name:    "enabled logger with full entry",
			enabled: true,
			entry: BypassAuditEntry{
				IP:           "192.168.1.100",
				Method:       "POST",
				Path:         "/api/users",
				MatchedRange: "192.168.1.0/24",
				UserAgent:    "TestClient/1.0",
				Headers: map[string]string{
					"X-Request-ID": "test-123",
				},
				Message: "Test bypass access",
			},
			wantLog:        true,
			wantSuccess:    true,
			validateFields: []string{"ip", "method", "path", "matched_range", "success", "timestamp", "user_agent", "headers", "message"},
		},
		{
			name:    "disabled logger",
			enabled: false,
			entry: BypassAuditEntry{
				IP:     "10.0.0.1",
				Method: "GET",
				Path:   "/api/data",
			},
			wantLog:        false,
			wantSuccess:    false,
			validateFields: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture log output
			var buf bytes.Buffer
			log.SetOutput(&buf)
			defer log.SetOutput(log.Writer()) // Restore original output

			logger := NewBypassAuditLogger(tt.enabled)
			err := logger.LogBypassAccess(tt.entry)

			if err != nil {
				t.Errorf("LogBypassAccess() error = %v", err)
			}

			logOutput := buf.String()

			if tt.wantLog {
				if !strings.Contains(logOutput, "[BYPASS-AUDIT-ACCESS]") {
					t.Error("Expected log output to contain [BYPASS-AUDIT-ACCESS]")
				}

				// Extract JSON from log output
				jsonStart := strings.Index(logOutput, "{")
				if jsonStart == -1 {
					t.Fatal("No JSON found in log output")
				}
				jsonStr := logOutput[jsonStart:]

				// Parse JSON
				var loggedEntry BypassAuditEntry
				if err := json.Unmarshal([]byte(jsonStr), &loggedEntry); err != nil {
					t.Fatalf("Failed to parse logged JSON: %v", err)
				}

				// Validate success field
				if tt.wantSuccess && !loggedEntry.Success {
					t.Error("Expected success=true in logged entry")
				}

				// Validate timestamp
				if loggedEntry.Timestamp.IsZero() {
					t.Error("Timestamp should not be zero")
				}

				// Validate required fields are present
				if tt.entry.IP != "" && loggedEntry.IP != tt.entry.IP {
					t.Errorf("IP = %q, want %q", loggedEntry.IP, tt.entry.IP)
				}
				if tt.entry.Method != "" && loggedEntry.Method != tt.entry.Method {
					t.Errorf("Method = %q, want %q", loggedEntry.Method, tt.entry.Method)
				}
				if tt.entry.Path != "" && loggedEntry.Path != tt.entry.Path {
					t.Errorf("Path = %q, want %q", loggedEntry.Path, tt.entry.Path)
				}
				if tt.entry.MatchedRange != "" && loggedEntry.MatchedRange != tt.entry.MatchedRange {
					t.Errorf("MatchedRange = %q, want %q", loggedEntry.MatchedRange, tt.entry.MatchedRange)
				}
				if tt.entry.UserAgent != "" && loggedEntry.UserAgent != tt.entry.UserAgent {
					t.Errorf("UserAgent = %q, want %q", loggedEntry.UserAgent, tt.entry.UserAgent)
				}
				if tt.entry.Message != "" && loggedEntry.Message != tt.entry.Message {
					t.Errorf("Message = %q, want %q", loggedEntry.Message, tt.entry.Message)
				}
			} else {
				if logOutput != "" {
					t.Errorf("Expected no log output for disabled logger, got: %s", logOutput)
				}
			}
		})
	}
}

func TestBypassAuditLoggerImpl_LogBypassAttempt(t *testing.T) {
	tests := []struct {
		name           string
		enabled        bool
		entry          BypassAuditEntry
		wantLog        bool
		validateFields []string
	}{
		{
			name:    "enabled logger with successful attempt",
			enabled: true,
			entry: BypassAuditEntry{
				IP:           "10.0.0.1",
				Method:       "GET",
				Path:         "/api/data",
				MatchedRange: "10.0.0.0/24",
				Success:      true,
			},
			wantLog:        true,
			validateFields: []string{"ip", "method", "path", "matched_range", "success", "timestamp"},
		},
		{
			name:    "enabled logger with failed attempt",
			enabled: true,
			entry: BypassAuditEntry{
				IP:      "192.168.1.100",
				Method:  "POST",
				Path:    "/api/users",
				Success: false,
				Message: "IP not in bypass range",
			},
			wantLog:        true,
			validateFields: []string{"ip", "method", "path", "success", "timestamp", "message"},
		},
		{
			name:    "disabled logger",
			enabled: false,
			entry: BypassAuditEntry{
				IP:     "10.0.0.1",
				Method: "GET",
				Path:   "/api/data",
			},
			wantLog:        false,
			validateFields: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture log output
			var buf bytes.Buffer
			log.SetOutput(&buf)
			defer log.SetOutput(log.Writer())

			logger := NewBypassAuditLogger(tt.enabled)
			err := logger.LogBypassAttempt(tt.entry)

			if err != nil {
				t.Errorf("LogBypassAttempt() error = %v", err)
			}

			logOutput := buf.String()

			if tt.wantLog {
				if !strings.Contains(logOutput, "[BYPASS-AUDIT-ATTEMPT]") {
					t.Error("Expected log output to contain [BYPASS-AUDIT-ATTEMPT]")
				}

				// Extract JSON from log output
				jsonStart := strings.Index(logOutput, "{")
				if jsonStart == -1 {
					t.Fatal("No JSON found in log output")
				}
				jsonStr := logOutput[jsonStart:]

				// Parse JSON
				var loggedEntry BypassAuditEntry
				if err := json.Unmarshal([]byte(jsonStr), &loggedEntry); err != nil {
					t.Fatalf("Failed to parse logged JSON: %v", err)
				}

				// Validate timestamp
				if loggedEntry.Timestamp.IsZero() {
					t.Error("Timestamp should not be zero")
				}

				// Validate success field maintains original value
				if loggedEntry.Success != tt.entry.Success {
					t.Errorf("Success = %v, want %v", loggedEntry.Success, tt.entry.Success)
				}

				// Validate required fields are present
				if tt.entry.IP != "" && loggedEntry.IP != tt.entry.IP {
					t.Errorf("IP = %q, want %q", loggedEntry.IP, tt.entry.IP)
				}
				if tt.entry.Method != "" && loggedEntry.Method != tt.entry.Method {
					t.Errorf("Method = %q, want %q", loggedEntry.Method, tt.entry.Method)
				}
				if tt.entry.Path != "" && loggedEntry.Path != tt.entry.Path {
					t.Errorf("Path = %q, want %q", loggedEntry.Path, tt.entry.Path)
				}
			} else {
				if logOutput != "" {
					t.Errorf("Expected no log output for disabled logger, got: %s", logOutput)
				}
			}
		})
	}
}

func TestCreateBypassAuditEntry(t *testing.T) {
	ip := "10.0.0.1"
	method := "GET"
	path := "/api/test"

	entry := CreateBypassAuditEntry(ip, method, path)

	if entry.IP != ip {
		t.Errorf("IP = %q, want %q", entry.IP, ip)
	}
	if entry.Method != method {
		t.Errorf("Method = %q, want %q", entry.Method, method)
	}
	if entry.Path != path {
		t.Errorf("Path = %q, want %q", entry.Path, path)
	}
	if entry.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}
}

func TestBypassAuditEntry_WithMatchedRange(t *testing.T) {
	entry := BypassAuditEntry{
		IP:     "10.0.0.1",
		Method: "GET",
		Path:   "/api/test",
	}

	matchedRange := "10.0.0.0/24"
	newEntry := entry.WithMatchedRange(matchedRange)

	if newEntry.MatchedRange != matchedRange {
		t.Errorf("MatchedRange = %q, want %q", newEntry.MatchedRange, matchedRange)
	}
	// Verify original fields are preserved
	if newEntry.IP != entry.IP {
		t.Error("IP was modified unexpectedly")
	}
}

func TestBypassAuditEntry_WithUserAgent(t *testing.T) {
	entry := BypassAuditEntry{
		IP:     "10.0.0.1",
		Method: "GET",
		Path:   "/api/test",
	}

	userAgent := "TestClient/1.0"
	newEntry := entry.WithUserAgent(userAgent)

	if newEntry.UserAgent != userAgent {
		t.Errorf("UserAgent = %q, want %q", newEntry.UserAgent, userAgent)
	}
	// Verify original fields are preserved
	if newEntry.IP != entry.IP {
		t.Error("IP was modified unexpectedly")
	}
}

func TestBypassAuditEntry_WithHeaders(t *testing.T) {
	entry := BypassAuditEntry{
		IP:     "10.0.0.1",
		Method: "GET",
		Path:   "/api/test",
	}

	headers := map[string]string{
		"X-Request-ID":  "test-123",
		"Content-Type":  "application/json",
		"Authorization": "Bearer secret-token", // Should be filtered
		"Cookie":        "session=abc",         // Should be filtered
		"X-Api-Key":     "secret-key",          // Should be filtered
		"X-Auth-Token":  "auth-token",          // Should be filtered
	}

	newEntry := entry.WithHeaders(headers)

	// Check that sensitive headers are filtered out
	if _, exists := newEntry.Headers["Authorization"]; exists {
		t.Error("Authorization header should be filtered out")
	}
	if _, exists := newEntry.Headers["Cookie"]; exists {
		t.Error("Cookie header should be filtered out")
	}
	if _, exists := newEntry.Headers["X-Api-Key"]; exists {
		t.Error("X-Api-Key header should be filtered out")
	}
	if _, exists := newEntry.Headers["X-Auth-Token"]; exists {
		t.Error("X-Auth-Token header should be filtered out")
	}

	// Check that non-sensitive headers are preserved
	if newEntry.Headers["X-Request-ID"] != "test-123" {
		t.Error("X-Request-ID header should be preserved")
	}
	if newEntry.Headers["Content-Type"] != "application/json" {
		t.Error("Content-Type header should be preserved")
	}
}

func TestBypassAuditEntry_WithMessage(t *testing.T) {
	entry := BypassAuditEntry{
		IP:     "10.0.0.1",
		Method: "GET",
		Path:   "/api/test",
	}

	message := "Custom audit message"
	newEntry := entry.WithMessage(message)

	if newEntry.Message != message {
		t.Errorf("Message = %q, want %q", newEntry.Message, message)
	}
	// Verify original fields are preserved
	if newEntry.IP != entry.IP {
		t.Error("IP was modified unexpectedly")
	}
}

func TestBypassAuditEntry_ChainedMethods(t *testing.T) {
	entry := CreateBypassAuditEntry("10.0.0.1", "GET", "/api/test").
		WithMatchedRange("10.0.0.0/24").
		WithUserAgent("TestClient/1.0").
		WithHeaders(map[string]string{"X-Request-ID": "test-123"}).
		WithMessage("Test message")

	if entry.IP != "10.0.0.1" {
		t.Errorf("IP = %q, want %q", entry.IP, "10.0.0.1")
	}
	if entry.MatchedRange != "10.0.0.0/24" {
		t.Errorf("MatchedRange = %q, want %q", entry.MatchedRange, "10.0.0.0/24")
	}
	if entry.UserAgent != "TestClient/1.0" {
		t.Errorf("UserAgent = %q, want %q", entry.UserAgent, "TestClient/1.0")
	}
	if entry.Headers["X-Request-ID"] != "test-123" {
		t.Error("Headers not set correctly")
	}
	if entry.Message != "Test message" {
		t.Errorf("Message = %q, want %q", entry.Message, "Test message")
	}
}

func TestBypassAuditLoggerImpl_JSONFormat(t *testing.T) {
	// Capture log output
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(log.Writer())

	logger := NewBypassAuditLogger(true)

	// Create a test entry with all fields
	entry := BypassAuditEntry{
		IP:           "10.0.0.1",
		Method:       "POST",
		Path:         "/api/users",
		MatchedRange: "10.0.0.0/24",
		UserAgent:    "TestClient/2.0",
		Headers: map[string]string{
			"X-Request-ID": "req-456",
			"Accept":       "application/json",
		},
		Message: "Test audit entry",
	}

	// Log the access
	if err := logger.LogBypassAccess(entry); err != nil {
		t.Fatalf("LogBypassAccess() error = %v", err)
	}

	logOutput := buf.String()

	// Extract JSON from log output
	jsonStart := strings.Index(logOutput, "{")
	if jsonStart == -1 {
		t.Fatal("No JSON found in log output")
	}
	jsonStr := logOutput[jsonStart:]

	// Validate JSON structure
	var jsonData map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &jsonData); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	// Check all required fields exist
	requiredFields := []string{"timestamp", "ip", "method", "path", "success"}
	for _, field := range requiredFields {
		if _, exists := jsonData[field]; !exists {
			t.Errorf("Required field %q missing in JSON", field)
		}
	}

	// Check optional fields
	optionalFields := []string{"matched_range", "user_agent", "headers", "message"}
	for _, field := range optionalFields {
		if _, exists := jsonData[field]; !exists {
			t.Logf("Optional field %q not present (this may be expected)", field)
		}
	}

	// Validate timestamp format
	timestampStr, ok := jsonData["timestamp"].(string)
	if !ok {
		t.Error("Timestamp should be a string")
	} else {
		if _, err := time.Parse(time.RFC3339, timestampStr); err != nil {
			t.Errorf("Timestamp not in RFC3339 format: %v", err)
		}
	}

	// Validate success is boolean
	if success, ok := jsonData["success"].(bool); !ok {
		t.Error("Success should be a boolean")
	} else if !success {
		t.Error("Success should be true for LogBypassAccess")
	}
}

func TestBypassAuditLoggerImpl_ErrorHandling(t *testing.T) {
	// Test with a logger that has invalid JSON marshalling
	// This is hard to trigger with the current implementation,
	// but we can at least verify the error path doesn't panic

	logger := NewBypassAuditLogger(true)

	// Create an entry with a very large message that shouldn't cause issues
	entry := BypassAuditEntry{
		IP:      "10.0.0.1",
		Method:  "GET",
		Path:    "/api/test",
		Message: strings.Repeat("x", 10000), // Large message
	}

	// Should not panic or error
	err := logger.LogBypassAccess(entry)
	if err != nil {
		t.Errorf("LogBypassAccess() with large message error = %v", err)
	}

	err = logger.LogBypassAttempt(entry)
	if err != nil {
		t.Errorf("LogBypassAttempt() with large message error = %v", err)
	}
}
