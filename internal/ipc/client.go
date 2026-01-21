package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// ClientConfig holds configuration for the IPC client
type ClientConfig struct {
	SocketPath     string
	ConnectTimeout time.Duration
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
}

// Client represents an IPC client
type Client struct {
	config ClientConfig
}

// Connection represents an active connection to the IPC server
type Connection struct {
	conn         net.Conn
	reader       *bufio.Reader
	encoder      *json.Encoder
	readTimeout  time.Duration
	writeTimeout time.Duration
}

// NewClient creates a new IPC client
func NewClient(cfg ClientConfig) *Client {
	if cfg.SocketPath == "" {
		cfg.SocketPath = GetSocketPath()
	}
	if cfg.ConnectTimeout == 0 {
		cfg.ConnectTimeout = 5 * time.Second
	}
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 30 * time.Second
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 10 * time.Second
	}
	return &Client{config: cfg}
}

// Connect establishes a connection to the IPC server
func (c *Client) Connect(ctx context.Context) (*Connection, error) {
	// Check if socket exists
	if _, err := os.Stat(c.config.SocketPath); os.IsNotExist(err) {
		return nil, ErrNotRunning
	}

	// Set up dialer with timeout
	dialer := net.Dialer{Timeout: c.config.ConnectTimeout}
	conn, err := dialer.DialContext(ctx, "unix", c.config.SocketPath)
	if err != nil {
		// Check for connection refused
		if isConnectionRefused(err) {
			return nil, ErrStaleSocket
		}
		if os.IsTimeout(err) {
			return nil, ErrTimeout
		}
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	return &Connection{
		conn:         conn,
		reader:       bufio.NewReader(conn),
		encoder:      json.NewEncoder(conn),
		readTimeout:  c.config.ReadTimeout,
		writeTimeout: c.config.WriteTimeout,
	}, nil
}

func isConnectionRefused(err error) bool {
	if opErr, ok := err.(*net.OpError); ok {
		if sysErr, ok := opErr.Err.(*os.SyscallError); ok {
			return sysErr.Err.Error() == "connection refused"
		}
	}
	return false
}

// Close closes the connection
func (conn *Connection) Close() error {
	return conn.conn.Close()
}

// requestID is an atomic counter for generating unique request IDs
var requestID uint64

// Call performs a synchronous RPC call
func (conn *Connection) Call(ctx context.Context, method string, params, result interface{}) error {
	id := fmt.Sprintf("%d-%s", atomic.AddUint64(&requestID, 1), uuid.New().String()[:8])

	req, err := NewRequest(id, method, params)
	if err != nil {
		return err
	}

	// Set write deadline
	if err := conn.conn.SetWriteDeadline(time.Now().Add(conn.writeTimeout)); err != nil {
		return fmt.Errorf("failed to set write deadline: %w", err)
	}

	// Write request as newline-delimited JSON
	if err := conn.encoder.Encode(req); err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}

	// Set read deadline
	if err := conn.conn.SetReadDeadline(time.Now().Add(conn.readTimeout)); err != nil {
		return fmt.Errorf("failed to set read deadline: %w", err)
	}

	// Read response line
	line, err := conn.reader.ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if resp.Error != nil {
		return resp.Error
	}

	if result != nil && resp.Result != nil {
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("failed to unmarshal result: %w", err)
		}
	}

	return nil
}

// GetStatus is a convenience method to get the current status
func (c *Client) GetStatus(ctx context.Context) (*StatusResponse, error) {
	conn, err := c.Connect(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	var status StatusResponse
	if err := conn.Call(ctx, MethodStatusGet, nil, &status); err != nil {
		return nil, err
	}

	return &status, nil
}

// GetProgress is a convenience method to get the current progress
func (c *Client) GetProgress(ctx context.Context) (*ProgressResponse, error) {
	conn, err := c.Connect(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	var progress ProgressResponse
	if err := conn.Call(ctx, MethodProgressGet, nil, &progress); err != nil {
		return nil, err
	}

	return &progress, nil
}

// GetFullStatus gets both status and progress in a single connection
func (c *Client) GetFullStatus(ctx context.Context) (*FullStatusResponse, error) {
	conn, err := c.Connect(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	var status StatusResponse
	if err := conn.Call(ctx, MethodStatusGet, nil, &status); err != nil {
		return nil, err
	}

	result := &FullStatusResponse{Status: &status}

	// Get progress if running or waiting (waiting has last run's progress)
	if status.State == StateRunning || status.State == StateWaiting {
		var progress ProgressResponse
		if err := conn.Call(ctx, MethodProgressGet, nil, &progress); err == nil {
			result.Progress = &progress
		}
	}

	return result, nil
}

// RequestStop requests the vectorize process to stop
func (c *Client) RequestStop(ctx context.Context, force bool) (*StopResponse, error) {
	conn, err := c.Connect(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	params := &StopParams{Force: force}
	var resp StopResponse
	if err := conn.Call(ctx, MethodControlStop, params, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// IsRunning checks if a vectorize process is running
func (c *Client) IsRunning(ctx context.Context) bool {
	status, err := c.GetStatus(ctx)
	if err != nil {
		return false
	}
	return status.State == StateRunning
}
