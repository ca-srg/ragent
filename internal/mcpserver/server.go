package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ca-srg/ragent/internal/types"
)

// MCPServer represents the MCP server implementation
type MCPServer struct {
	server           *http.Server
	toolRegistry     *ToolRegistry
	ipAuthMiddleware *IPAuthMiddleware
	authMiddleware   *UnifiedAuthMiddleware
	sseManager       *SSEManager
	logger           *log.Logger
	shutdownChan     chan struct{}
	wg               sync.WaitGroup
	mutex            sync.RWMutex
	isRunning        bool
	config           *MCPServerConfig
}

// MCPServerConfig contains server configuration
type MCPServerConfig struct {
	Host                   string
	Port                   int
	ReadTimeout            time.Duration
	WriteTimeout           time.Duration
	IdleTimeout            time.Duration
	MaxHeaderBytes         int
	EnableGracefulShutdown bool
	ShutdownTimeout        time.Duration
	EnableSSE              bool
	SSEConfig              *SSEManagerConfig
}

// DefaultMCPServerConfig returns default server configuration
func DefaultMCPServerConfig() *MCPServerConfig {
	return &MCPServerConfig{
		Host:                   "localhost",
		Port:                   8080,
		ReadTimeout:            30 * time.Second,
		WriteTimeout:           30 * time.Second,
		IdleTimeout:            120 * time.Second,
		MaxHeaderBytes:         1 << 20, // 1MB
		EnableGracefulShutdown: true,
		ShutdownTimeout:        30 * time.Second,
		EnableSSE:              true,
		SSEConfig:              DefaultSSEManagerConfig(),
	}
}

// NewMCPServer creates a new MCP server instance
func NewMCPServer(config *MCPServerConfig) *MCPServer {
	if config == nil {
		config = DefaultMCPServerConfig()
	}

	server := &MCPServer{
		toolRegistry: NewToolRegistry(),
		logger:       log.New(os.Stdout, "[MCPServer] ", log.LstdFlags),
		shutdownChan: make(chan struct{}),
		config:       config,
	}

	// Setup SSE manager if enabled
	if config.EnableSSE {
		if config.SSEConfig == nil {
			config.SSEConfig = DefaultSSEManagerConfig()
		}
		server.sseManager = NewSSEManager(config.SSEConfig, server.logger)
	}

	// Setup HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/", server.handleMCPRequest)
	mux.HandleFunc("/health", server.handleHealthCheck)

	// Add SSE endpoints if enabled
	if config.EnableSSE && server.sseManager != nil {
		mux.HandleFunc("/sse", server.sseManager.HandleSSE)
		mux.HandleFunc("/events", server.sseManager.HandleSSE) // Alternative endpoint
		mux.HandleFunc("/sse/info", server.handleSSEInfo)
	}

	server.server = &http.Server{
		Addr:           fmt.Sprintf("%s:%d", config.Host, config.Port),
		Handler:        mux,
		ReadTimeout:    config.ReadTimeout,
		WriteTimeout:   config.WriteTimeout,
		IdleTimeout:    config.IdleTimeout,
		MaxHeaderBytes: config.MaxHeaderBytes,
	}

	return server
}

// SetIPAuthMiddleware sets the IP authentication middleware
func (s *MCPServer) SetIPAuthMiddleware(middleware *IPAuthMiddleware) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.ipAuthMiddleware = middleware
}

// SetUnifiedAuthMiddleware sets the unified authentication middleware
func (s *MCPServer) SetUnifiedAuthMiddleware(middleware *UnifiedAuthMiddleware) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.authMiddleware = middleware
}

// GetToolRegistry returns the tool registry
func (s *MCPServer) GetToolRegistry() *ToolRegistry {
	return s.toolRegistry
}

// Start starts the MCP server
func (s *MCPServer) Start() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.isRunning {
		return fmt.Errorf("server is already running")
	}

	// Start SSE manager if enabled
	if s.config.EnableSSE && s.sseManager != nil {
		ctx := context.Background()
		s.sseManager.Start(ctx)
		s.logger.Printf("SSE manager started")
	}

	// Wrap handler with middleware if available
	// Prefer unified auth middleware if configured
	if s.authMiddleware != nil {
		s.server.Handler = s.authMiddleware.Middleware(s.server.Handler)
		s.logger.Printf("Unified authentication middleware enabled with method: %s", s.authMiddleware.GetAuthMethod())
	} else if s.ipAuthMiddleware != nil {
		s.server.Handler = s.ipAuthMiddleware.Middleware(s.server.Handler)
		s.logger.Printf("IP authentication middleware enabled")
	}

	s.logger.Printf("Starting MCP server on %s", s.server.Addr)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Printf("Server error: %v", err)
		}
	}()

	s.isRunning = true
	s.logger.Printf("MCP server started successfully")

	if s.config.EnableSSE {
		s.logger.Printf("SSE endpoints available at /sse and /events")
	}

	return nil
}

// Stop gracefully stops the MCP server
func (s *MCPServer) Stop() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if !s.isRunning {
		return fmt.Errorf("server is not running")
	}

	s.logger.Printf("Stopping MCP server...")

	if s.config.EnableGracefulShutdown {
		ctx, cancel := context.WithTimeout(context.Background(), s.config.ShutdownTimeout)
		defer cancel()

		if err := s.server.Shutdown(ctx); err != nil {
			s.logger.Printf("Graceful shutdown failed: %v", err)
			return s.server.Close()
		}
	} else {
		if err := s.server.Close(); err != nil {
			return err
		}
	}

	close(s.shutdownChan)
	s.wg.Wait()
	s.isRunning = false
	s.logger.Printf("MCP server stopped successfully")
	return nil
}

// IsRunning returns whether the server is currently running
func (s *MCPServer) IsRunning() bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.isRunning
}

// WaitForShutdown blocks until the server is shut down
func (s *MCPServer) WaitForShutdown() {
	<-s.shutdownChan
}

// handleMCPRequest handles MCP JSON-RPC requests
func (s *MCPServer) handleMCPRequest(w http.ResponseWriter, r *http.Request) {
	s.logger.Printf("Request: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)

	// Only allow POST requests for JSON-RPC
	if r.Method != http.MethodPost {
		s.writeErrorResponse(w, nil, types.MCPErrorMethodNotFound, "Only POST method is allowed", nil)
		return
	}

	// Check Content-Type
	contentType := r.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		s.writeErrorResponse(w, nil, types.MCPErrorInvalidRequest, "Content-Type must be application/json", nil)
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.logger.Printf("Failed to read request body: %v", err)
		s.writeErrorResponse(w, nil, types.MCPErrorParseError, "Failed to read request body", nil)
		return
	}
	defer func() {
		if err := r.Body.Close(); err != nil {
			s.logger.Printf("Failed to close request body: %v", err)
		}
	}()

	// Parse JSON-RPC request
	var request types.LegacyMCPToolRequest
	if err := json.Unmarshal(body, &request); err != nil {
		s.logger.Printf("Failed to parse JSON-RPC request: %v", err)
		s.writeErrorResponse(w, nil, types.MCPErrorParseError, "Invalid JSON", nil)
		return
	}

	// Validate JSON-RPC format
	if request.JSONRPC != "2.0" {
		s.writeErrorResponse(w, request.ID, types.MCPErrorInvalidRequest, "Invalid JSON-RPC version", nil)
		return
	}

	if request.Method == "" {
		s.writeErrorResponse(w, request.ID, types.MCPErrorInvalidRequest, "Method field is required", nil)
		return
	}

	// Route request based on method
	s.routeRequest(w, r.Context(), &request)
}

// routeRequest routes the request to the appropriate handler
func (s *MCPServer) routeRequest(w http.ResponseWriter, ctx context.Context, request *types.LegacyMCPToolRequest) {
	switch request.Method {
	case "tools/list":
		s.handleToolsList(w, ctx, request)
	case "tools/call":
		s.handleToolCall(w, ctx, request)
	default:
		s.writeErrorResponse(w, request.ID, types.MCPErrorMethodNotFound, fmt.Sprintf("Method '%s' not found", request.Method), nil)
	}
}

// handleToolsList handles the tools/list method
func (s *MCPServer) handleToolsList(w http.ResponseWriter, ctx context.Context, request *types.LegacyMCPToolRequest) {
	tools := s.toolRegistry.ListTools()

	result := types.MCPToolListResult{
		Tools: tools,
	}

	response := types.NewMCPToolResponse(request.ID, result)
	s.writeJSONResponse(w, response)

	s.logger.Printf("Listed %d tools for request ID: %v", len(tools), request.ID)
}

// handleToolCall handles the tools/call method
func (s *MCPServer) handleToolCall(w http.ResponseWriter, ctx context.Context, request *types.LegacyMCPToolRequest) {
	// Parse tool call parameters
	var params types.MCPToolCallParams
	if request.Params != nil {
		paramBytes, err := json.Marshal(request.Params)
		if err != nil {
			s.writeErrorResponse(w, request.ID, types.MCPErrorInvalidParams, "Failed to marshal parameters", err.Error())
			return
		}
		if err := json.Unmarshal(paramBytes, &params); err != nil {
			s.writeErrorResponse(w, request.ID, types.MCPErrorInvalidParams, "Invalid parameters format", err.Error())
			return
		}
	}

	if params.Name == "" {
		s.writeErrorResponse(w, request.ID, types.MCPErrorInvalidParams, "Tool name is required", nil)
		return
	}

	// Notify SSE clients about tool execution start
	if s.config.EnableSSE && s.sseManager != nil {
		s.sseManager.SendEvent(&SSEEvent{
			Event: "tool_execution_start",
			Data: map[string]interface{}{
				"tool":       params.Name,
				"request_id": request.ID,
				"params":     params.Arguments,
				"timestamp":  time.Now().UTC(),
			},
		})
	}

	// Execute the tool
	result, err := s.toolRegistry.ExecuteTool(ctx, params.Name, params.Arguments)

	// Notify SSE clients about tool execution result
	if s.config.EnableSSE && s.sseManager != nil {
		s.sseManager.NotifyToolExecution(params.Name, params.Arguments, result, err)
	}

	if err != nil {
		s.logger.Printf("Tool execution failed for '%s': %v", params.Name, err)
		s.writeErrorResponse(w, request.ID, types.MCPErrorInternalError, fmt.Sprintf("Tool execution failed: %v", err), nil)
		return
	}

	response := types.NewMCPToolResponse(request.ID, result)
	s.writeJSONResponse(w, response)

	s.logger.Printf("Executed tool '%s' for request ID: %v", params.Name, request.ID)
}

// handleHealthCheck handles health check requests
func (s *MCPServer) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	status := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
		"version":   "1.0.0",
		"tools":     s.toolRegistry.ToolCount(),
		"uptime":    time.Since(time.Now()), // This would need to be tracked properly
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(status); err != nil {
		s.logger.Printf("Failed to encode status response: %v", err)
	}
}

// writeJSONResponse writes a JSON response
func (s *MCPServer) writeJSONResponse(w http.ResponseWriter, response *types.MCPToolResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Printf("Failed to encode response: %v", err)
		// Write a basic error response if JSON encoding fails
		w.WriteHeader(http.StatusInternalServerError)
		if _, err := w.Write([]byte(`{"jsonrpc": "2.0", "error": {"code": -32603, "message": "Internal server error"}}`)); err != nil {
			s.logger.Printf("Failed to write error response: %v", err)
		}
	}
}

// writeErrorResponse writes an error response
func (s *MCPServer) writeErrorResponse(w http.ResponseWriter, id interface{}, code int, message string, data interface{}) {
	response := types.NewMCPErrorResponse(id, code, message, data)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // JSON-RPC errors are sent with HTTP 200

	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Printf("Failed to encode error response: %v", err)
		// Write a basic error response if JSON encoding fails
		w.WriteHeader(http.StatusInternalServerError)
		if _, err := w.Write([]byte(`{"jsonrpc": "2.0", "error": {"code": -32603, "message": "Internal server error"}}`)); err != nil {
			s.logger.Printf("Failed to write error response: %v", err)
		}
	}

	s.logger.Printf("Error response: code=%d, message=%s, id=%v", code, message, id)
}

// SetLogger sets a custom logger for the server
func (s *MCPServer) SetLogger(logger *log.Logger) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.logger = logger
}

// GetConfig returns the server configuration
func (s *MCPServer) GetConfig() *MCPServerConfig {
	return s.config
}

// GetServerInfo returns information about the running server
func (s *MCPServer) GetServerInfo() map[string]interface{} {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	info := map[string]interface{}{
		"address":    s.server.Addr,
		"running":    s.isRunning,
		"tools":      s.toolRegistry.GetRegisteredToolNames(),
		"tool_count": s.toolRegistry.ToolCount(),
		"config": map[string]interface{}{
			"read_timeout":      s.config.ReadTimeout.String(),
			"write_timeout":     s.config.WriteTimeout.String(),
			"idle_timeout":      s.config.IdleTimeout.String(),
			"max_header_bytes":  s.config.MaxHeaderBytes,
			"graceful_shutdown": s.config.EnableGracefulShutdown,
			"shutdown_timeout":  s.config.ShutdownTimeout.String(),
			"sse_enabled":       s.config.EnableSSE,
		},
	}

	// Add SSE information if enabled
	if s.config.EnableSSE && s.sseManager != nil {
		info["sse"] = map[string]interface{}{
			"client_count": s.sseManager.GetClientCount(),
			"endpoints":    []string{"/sse", "/events"},
		}
	}

	return info
}

// handleSSEInfo handles SSE information requests
func (s *MCPServer) handleSSEInfo(w http.ResponseWriter, r *http.Request) {
	if !s.config.EnableSSE || s.sseManager == nil {
		http.Error(w, "SSE is not enabled", http.StatusNotFound)
		return
	}

	info := map[string]interface{}{
		"enabled":      true,
		"client_count": s.sseManager.GetClientCount(),
		"clients":      s.sseManager.GetClientInfo(),
		"config": map[string]interface{}{
			"heartbeat_interval": s.config.SSEConfig.HeartbeatInterval.String(),
			"buffer_size":        s.config.SSEConfig.BufferSize,
			"max_clients":        s.config.SSEConfig.MaxClients,
			"history_size":       s.config.SSEConfig.HistorySize,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(info); err != nil {
		s.logger.Printf("Failed to encode SSE info response: %v", err)
	}
}
