package mcpserver

import (
	"encoding/json"
	"log"
	"time"
)

// BypassAuditLogger defines the interface for audit logging of bypass authentication
type BypassAuditLogger interface {
	LogBypassAccess(entry BypassAuditEntry) error
	LogBypassAttempt(entry BypassAuditEntry) error
}

// BypassAuditEntry represents an audit log entry for bypass authentication
type BypassAuditEntry struct {
	Timestamp    time.Time `json:"timestamp"`
	IP           string    `json:"ip"`
	Method       string    `json:"method"`
	Path         string    `json:"path"`
	MatchedRange string    `json:"matched_range,omitempty"`
	Success      bool      `json:"success"`
	UserAgent    string    `json:"user_agent,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	Message      string    `json:"message,omitempty"`
}

// BypassAuditLoggerImpl implements the BypassAuditLogger interface
type BypassAuditLoggerImpl struct {
	enabled bool
}

// NewBypassAuditLogger creates a new BypassAuditLogger instance
func NewBypassAuditLogger(enabled bool) *BypassAuditLoggerImpl {
	return &BypassAuditLoggerImpl{
		enabled: enabled,
	}
}

// LogBypassAccess logs successful bypass authentication access
func (l *BypassAuditLoggerImpl) LogBypassAccess(entry BypassAuditEntry) error {
	if !l.enabled {
		return nil
	}

	entry.Timestamp = time.Now()
	entry.Success = true

	jsonData, err := json.Marshal(entry)
	if err != nil {
		log.Printf("[BYPASS-AUDIT] ERROR: Failed to marshal audit entry for IP '%s' on %s %s: %v", entry.IP, entry.Method, entry.Path, err)
		return err
	}

	log.Printf("[BYPASS-AUDIT-ACCESS] Bypassed authentication for IP '%s' accessing %s %s: %s", entry.IP, entry.Method, entry.Path, string(jsonData))
	return nil
}

// LogBypassAttempt logs bypass authentication attempts (both successful and failed)
func (l *BypassAuditLoggerImpl) LogBypassAttempt(entry BypassAuditEntry) error {
	if !l.enabled {
		return nil
	}

	entry.Timestamp = time.Now()

	jsonData, err := json.Marshal(entry)
	if err != nil {
		log.Printf("[BYPASS-AUDIT] ERROR: Failed to marshal audit attempt entry for IP '%s' on %s %s: %v", entry.IP, entry.Method, entry.Path, err)
		return err
	}

	log.Printf("[BYPASS-AUDIT-ATTEMPT] Bypass attempt (success=%v) for IP '%s' accessing %s %s: %s", entry.Success, entry.IP, entry.Method, entry.Path, string(jsonData))
	return nil
}

// CreateBypassAuditEntry is a helper function to create a basic audit entry
func CreateBypassAuditEntry(ip, method, path string) BypassAuditEntry {
	return BypassAuditEntry{
		Timestamp: time.Now(),
		IP:        ip,
		Method:    method,
		Path:      path,
	}
}

// WithMatchedRange adds matched range information to the audit entry
func (e BypassAuditEntry) WithMatchedRange(matchedRange string) BypassAuditEntry {
	e.MatchedRange = matchedRange
	return e
}

// WithUserAgent adds user agent information to the audit entry
func (e BypassAuditEntry) WithUserAgent(userAgent string) BypassAuditEntry {
	e.UserAgent = userAgent
	return e
}

// WithHeaders adds headers to the audit entry (excluding sensitive information)
func (e BypassAuditEntry) WithHeaders(headers map[string]string) BypassAuditEntry {
	// Filter out sensitive headers
	filteredHeaders := make(map[string]string)
	sensitiveHeaders := map[string]bool{
		"Authorization":     true,
		"Cookie":           true,
		"X-Api-Key":        true,
		"X-Auth-Token":     true,
	}

	for key, value := range headers {
		if !sensitiveHeaders[key] {
			filteredHeaders[key] = value
		}
	}

	e.Headers = filteredHeaders
	return e
}

// WithMessage adds a custom message to the audit entry
func (e BypassAuditEntry) WithMessage(message string) BypassAuditEntry {
	e.Message = message
	return e
}