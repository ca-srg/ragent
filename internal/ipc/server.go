package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// ServerConfig holds configuration for the IPC server
type ServerConfig struct {
	SocketPath string
	PIDFile    string
}

// Server represents the IPC server for vectorize process
type Server struct {
	config   ServerConfig
	listener net.Listener
	lockFile *os.File
	logger   *log.Logger

	handlers map[string]HandlerFunc

	statusMu sync.RWMutex
	status   *StatusResponse

	progressMu sync.RWMutex
	progress   *ProgressResponse

	stopChan chan struct{}
	wg       sync.WaitGroup
	running  bool
	runMu    sync.Mutex
}

// HandlerFunc is the type for RPC method handlers
type HandlerFunc func(ctx context.Context, params json.RawMessage) (interface{}, error)

// NewServer creates a new IPC server
func NewServer(cfg ServerConfig, logger *log.Logger) (*Server, error) {
	if cfg.SocketPath == "" {
		cfg.SocketPath = GetSocketPath()
	}
	if cfg.PIDFile == "" {
		cfg.PIDFile = GetPIDPath()
	}
	if logger == nil {
		logger = log.New(os.Stdout, "[ipc] ", log.LstdFlags)
	}

	// Create socket directory
	socketDir := filepath.Dir(cfg.SocketPath)
	if err := os.MkdirAll(socketDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create socket directory: %w", err)
	}

	// Try to acquire file lock on PID file
	lockFile, err := os.OpenFile(cfg.PIDFile, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open pid file: %w", err)
	}

	// Try exclusive lock (non-blocking)
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lockFile.Close()
		return nil, ErrAnotherInstanceRunning
	}

	// Clean up stale socket if exists
	if err := cleanupStaleSocket(cfg.SocketPath); err != nil {
		_ = lockFile.Close()
		return nil, err
	}

	// Write PID
	if err := lockFile.Truncate(0); err != nil {
		_ = lockFile.Close()
		return nil, fmt.Errorf("failed to truncate pid file: %w", err)
	}
	if _, err := lockFile.Seek(0, 0); err != nil {
		_ = lockFile.Close()
		return nil, fmt.Errorf("failed to seek pid file: %w", err)
	}
	if _, err := fmt.Fprintf(lockFile, "%d\n", os.Getpid()); err != nil {
		_ = lockFile.Close()
		return nil, fmt.Errorf("failed to write pid: %w", err)
	}

	s := &Server{
		config:   cfg,
		lockFile: lockFile,
		logger:   logger,
		handlers: make(map[string]HandlerFunc),
		status: &StatusResponse{
			State: StateIdle,
			PID:   os.Getpid(),
		},
		stopChan: make(chan struct{}),
	}

	s.registerDefaultHandlers()

	return s, nil
}

func cleanupStaleSocket(socketPath string) error {
	// Check if socket exists
	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		return nil
	}

	// Try to connect - if fails, socket is stale
	conn, err := net.DialTimeout("unix", socketPath, 100*time.Millisecond)
	if err != nil {
		// Stale socket, remove it
		return os.Remove(socketPath)
	}
	_ = conn.Close()

	// Socket is live, another process is running
	return ErrAnotherInstanceRunning
}

func (s *Server) registerDefaultHandlers() {
	s.handlers[MethodStatusGet] = func(_ context.Context, _ json.RawMessage) (interface{}, error) {
		s.statusMu.RLock()
		defer s.statusMu.RUnlock()
		return s.status, nil
	}

	s.handlers[MethodProgressGet] = func(_ context.Context, _ json.RawMessage) (interface{}, error) {
		s.progressMu.RLock()
		defer s.progressMu.RUnlock()
		return s.progress, nil
	}

	s.handlers[MethodControlStop] = func(_ context.Context, params json.RawMessage) (interface{}, error) {
		var p StopParams
		if params != nil {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, fmt.Errorf("invalid params: %w", err)
			}
		}

		select {
		case <-s.stopChan:
			return &StopResponse{Acknowledged: false, Message: "already stopping"}, nil
		default:
			close(s.stopChan)
			return &StopResponse{Acknowledged: true, Message: "shutdown initiated"}, nil
		}
	}
}

// RegisterHandler registers a custom RPC handler
func (s *Server) RegisterHandler(method string, handler HandlerFunc) {
	s.handlers[method] = handler
}

// Start starts the IPC server
func (s *Server) Start(ctx context.Context) error {
	s.runMu.Lock()
	if s.running {
		s.runMu.Unlock()
		return fmt.Errorf("server already running")
	}
	s.running = true
	s.runMu.Unlock()

	listener, err := net.Listen("unix", s.config.SocketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on socket: %w", err)
	}
	s.listener = listener

	// Set socket permissions (owner only)
	if err := os.Chmod(s.config.SocketPath, 0600); err != nil {
		_ = s.listener.Close()
		return fmt.Errorf("failed to set socket permissions: %w", err)
	}

	s.logger.Printf("IPC server started on %s", s.config.SocketPath)

	// Accept loop
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-s.stopChan:
					return
				case <-ctx.Done():
					return
				default:
					s.logger.Printf("accept error: %v", err)
					continue
				}
			}

			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				s.handleConnection(ctx, conn)
			}()
		}
	}()

	return nil
}

func (s *Server) handleConnection(ctx context.Context, conn net.Conn) {
	defer func() { _ = conn.Close() }()

	reader := bufio.NewReader(conn)
	encoder := json.NewEncoder(conn)

	for {
		// Read line (newline-delimited JSON)
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				s.logger.Printf("read error: %v", err)
			}
			return
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			resp := NewErrorResponse("", ErrCodeParse, "parse error: "+err.Error())
			if encErr := encoder.Encode(resp); encErr != nil {
				s.logger.Printf("encode error: %v", encErr)
			}
			continue
		}

		resp := s.handleRequest(ctx, &req)
		if err := encoder.Encode(resp); err != nil {
			s.logger.Printf("encode error: %v", err)
			return
		}
	}
}

func (s *Server) handleRequest(ctx context.Context, req *Request) *Response {
	handler, ok := s.handlers[req.Method]
	if !ok {
		return NewErrorResponse(req.ID, ErrCodeMethodNotFound, fmt.Sprintf("method not found: %s", req.Method))
	}

	result, err := handler(ctx, req.Params)
	if err != nil {
		var rpcErr *RPCError
		if ok := isRPCError(err, &rpcErr); ok {
			return &Response{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   rpcErr,
			}
		}
		return NewErrorResponse(req.ID, ErrCodeInternal, err.Error())
	}

	resp, err := NewResponse(req.ID, result)
	if err != nil {
		return NewErrorResponse(req.ID, ErrCodeInternal, "failed to create response")
	}

	return resp
}

func isRPCError(err error, target **RPCError) bool {
	if rpcErr, ok := err.(*RPCError); ok {
		*target = rpcErr
		return true
	}
	return false
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	s.runMu.Lock()
	if !s.running {
		s.runMu.Unlock()
		return nil
	}
	s.running = false
	s.runMu.Unlock()

	// Signal shutdown
	select {
	case <-s.stopChan:
	default:
		close(s.stopChan)
	}

	// Close listener
	if s.listener != nil {
		_ = s.listener.Close()
	}

	// Wait for connections with timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}

	// Cleanup
	if err := os.Remove(s.config.SocketPath); err != nil && !os.IsNotExist(err) {
		s.logger.Printf("failed to remove socket: %v", err)
	}
	if s.lockFile != nil {
		_ = s.lockFile.Close()
		if err := os.Remove(s.config.PIDFile); err != nil && !os.IsNotExist(err) {
			s.logger.Printf("failed to remove pid file: %v", err)
		}
	}

	s.logger.Printf("IPC server stopped")
	return nil
}

// StopChan returns the channel that is closed when stop is requested
func (s *Server) StopChan() <-chan struct{} {
	return s.stopChan
}

// UpdateStatus updates the current status
func (s *Server) UpdateStatus(status *StatusResponse) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	s.status = status
}

// UpdateProgress updates the current progress
func (s *Server) UpdateProgress(progress *ProgressResponse) {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	s.progress = progress
}

// SetState is a convenience method to update just the state
func (s *Server) SetState(state ProcessState) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	if s.status == nil {
		s.status = &StatusResponse{PID: os.Getpid()}
	}
	s.status.State = state
}

// SetStateWithTime sets the state with a start time
func (s *Server) SetStateWithTime(state ProcessState, startTime time.Time) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	if s.status == nil {
		s.status = &StatusResponse{PID: os.Getpid()}
	}
	s.status.State = state
	s.status.StartedAt = &startTime
}
