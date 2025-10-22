package slacksearch

import "fmt"

// SlackSearchErrorType represents classification for Slack search failures.
type SlackSearchErrorType int

const (
	ErrorTypeRateLimit SlackSearchErrorType = iota
	ErrorTypeAPIUnavailable
	ErrorTypeNoResults
	ErrorTypeInsufficientInfo
	ErrorTypeLLMTimeout
	ErrorTypeAuth
	ErrorTypePermission
	ErrorTypeContextOverflow
)

// SlackSearchError describes an error encountered during Slack search operations.
type SlackSearchError struct {
	Type      SlackSearchErrorType `json:"type"`
	Message   string               `json:"message"`
	Cause     error                `json:"-"`
	Retryable bool                 `json:"retryable"`
}

// Error implements the error interface.
func (e *SlackSearchError) Error() string {
	if e == nil {
		return ""
	}

	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}

	return e.Message
}

// Unwrap exposes the underlying cause for errors.Unwrap compatibility.
func (e *SlackSearchError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Cause
}
