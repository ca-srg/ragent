package ipc

import (
	"encoding/json"
	"errors"
	"fmt"
)

// JSON-RPC 2.0 error codes
const (
	ErrCodeParse          = -32700 // Invalid JSON
	ErrCodeInvalidRequest = -32600 // Invalid Request object
	ErrCodeMethodNotFound = -32601 // Method not found
	ErrCodeInvalidParams  = -32602 // Invalid method parameters
	ErrCodeInternal       = -32603 // Internal error

	// Application-specific error codes
	ErrCodeNotRunning = -1001 // Vectorize process not running
	ErrCodeBusy       = -1002 // Process is busy
)

// Request represents a JSON-RPC 2.0 request
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents a JSON-RPC 2.0 response
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC 2.0 error object
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Error implements the error interface
func (e *RPCError) Error() string {
	return fmt.Sprintf("RPC error %d: %s", e.Code, e.Message)
}

// NewRequest creates a new JSON-RPC 2.0 request
func NewRequest(id, method string, params interface{}) (*Request, error) {
	req := &Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
	}

	if params != nil {
		paramsJSON, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal params: %w", err)
		}
		req.Params = paramsJSON
	}

	return req, nil
}

// NewResponse creates a successful JSON-RPC 2.0 response
func NewResponse(id string, result interface{}) (*Response, error) {
	resp := &Response{
		JSONRPC: "2.0",
		ID:      id,
	}

	if result != nil {
		resultJSON, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal result: %w", err)
		}
		resp.Result = resultJSON
	}

	return resp, nil
}

// NewErrorResponse creates an error JSON-RPC 2.0 response
func NewErrorResponse(id string, code int, message string) *Response {
	return &Response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
		},
	}
}

// Sentinel errors for IPC operations
var (
	ErrNotRunning             = errors.New("vectorize process is not running")
	ErrConnectionRefused      = errors.New("connection refused")
	ErrStaleSocket            = errors.New("socket exists but process is dead")
	ErrTimeout                = errors.New("connection timeout")
	ErrAnotherInstanceRunning = errors.New("another vectorize instance is already running")
)
