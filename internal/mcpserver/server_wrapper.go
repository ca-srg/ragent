package mcpserver

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ca-srg/ragent/internal/types"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ServerWrapper wraps the MCP SDK server with RAGent-specific functionality
// Maintains API compatibility with existing MCPServer while using official SDK
type ServerWrapper struct {
	// Core SDK server instance
	sdkServer  *mcp.Server
	httpServer *http.Server

	// Configuration management
	configAdapter *ConfigAdapter
	sdkConfig     *SDKServerConfig
	ragentConfig  *types.Config

	// RAGent-specific components (maintain existing integrations)
	toolRegistry          *ToolRegistry
	ipAuthMiddleware      *IPAuthMiddleware
	unifiedAuthMiddleware *UnifiedAuthMiddleware
	sseManager            *SSEManager

	// Server lifecycle management
	logger       *log.Logger
	shutdownChan chan struct{}
	wg           sync.WaitGroup
	mutex        sync.RWMutex
	isRunning    bool

	// Context management
	ctx        context.Context
	cancelFunc context.CancelFunc
}

// NewServerWrapper creates a new SDK-based server wrapper with RAGent extensions
func NewServerWrapper(config *types.Config) (*ServerWrapper, error) {
	if config == nil {
		return nil, fmt.Errorf("configuration cannot be nil")
	}

	// Create configuration adapter
	configAdapter := NewConfigAdapter(config)
	sdkConfig, err := configAdapter.ToSDKConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to convert configuration: %w", err)
	}

	// Initialize logger
	logger := log.New(os.Stdout, "[ServerWrapper] ", log.LstdFlags)

	wrapper := &ServerWrapper{
		configAdapter: configAdapter,
		sdkConfig:     sdkConfig,
		ragentConfig:  config,
		toolRegistry:  NewToolRegistry(),
		logger:        logger,
		shutdownChan:  make(chan struct{}),
	}

	// Create context for server lifecycle
	wrapper.ctx, wrapper.cancelFunc = context.WithCancel(context.Background())

	// Initialize SDK server
	if err := wrapper.initializeSDKServer(); err != nil {
		return nil, fmt.Errorf("failed to initialize SDK server: %w", err)
	}

	// Setup SSE manager if enabled
	if wrapper.sdkConfig.SSEEnabled {
		wrapper.initializeSSEManager()
	}

	logger.Printf("ServerWrapper initialized successfully")
	return wrapper, nil
}

// initializeSDKServer creates and configures the SDK server instance
func (sw *ServerWrapper) initializeSDKServer() error {
	// Create SDK server implementation info
	impl := &mcp.Implementation{
		Name:    "ragent-mcp-server",
		Version: "1.0.0",
	}

	// Create SDK server with implementation
	sw.sdkServer = mcp.NewServer(impl, nil)

	sw.logger.Printf("SDK server initialized with implementation: %+v", impl)
	return nil
}

// initializeSSEManager sets up the SSE manager with configuration
func (sw *ServerWrapper) initializeSSEManager() {
	sseConfig := &SSEManagerConfig{
		HeartbeatInterval: sw.sdkConfig.SSEHeartbeatInterval,
		BufferSize:        sw.sdkConfig.SSEBufferSize,
		MaxClients:        sw.sdkConfig.SSEMaxClients,
		HistorySize:       sw.sdkConfig.SSEHistorySize,
	}

	sw.sseManager = NewSSEManager(sseConfig, sw.logger)
	sw.logger.Printf("SSE manager initialized")
}

// SetIPAuthMiddleware sets the IP authentication middleware
// Maintains API compatibility with existing MCPServer
func (sw *ServerWrapper) SetIPAuthMiddleware(middleware *IPAuthMiddleware) {
	sw.mutex.Lock()
	defer sw.mutex.Unlock()

	sw.ipAuthMiddleware = middleware
	sw.logger.Printf("IP authentication middleware set")
}

// SetUnifiedAuthMiddleware sets the unified authentication middleware (IP/OIDC/both/either)
// If set, this takes precedence over IP-only middleware when starting the HTTP server.
func (sw *ServerWrapper) SetUnifiedAuthMiddleware(middleware *UnifiedAuthMiddleware) {
	sw.mutex.Lock()
	defer sw.mutex.Unlock()

	sw.unifiedAuthMiddleware = middleware
	if middleware != nil {
		sw.logger.Printf("Unified authentication middleware set (method: %s)", middleware.GetAuthMethod())
	} else {
		sw.logger.Printf("Unified authentication middleware cleared")
	}
}

// GetToolRegistry returns the tool registry
// Maintains API compatibility with existing MCPServer
func (sw *ServerWrapper) GetToolRegistry() *ToolRegistry {
	return sw.toolRegistry
}

// RegisterTool registers a tool with the SDK server
func (sw *ServerWrapper) RegisterTool(name string, handler mcp.ToolHandler) error {
	if sw.sdkServer == nil {
		return fmt.Errorf("SDK server not initialized")
	}

	// Create basic input schema for SDK compatibility
	basicSchema := &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"query": {
				Type:        "string",
				Description: "Input query or parameters",
			},
		},
		Required: []string{"query"},
	}

	// Create tool definition for SDK
	tool := &mcp.Tool{
		Name:        name,
		Description: "Tool for RAGent",
		InputSchema: basicSchema,
	}

	// Register tool with SDK server using AddTool
	sw.sdkServer.AddTool(tool, handler)

	sw.logger.Printf("Tool %s registered successfully", name)
	return nil
}

// Start starts the server with lifecycle management
// Maintains API compatibility with existing MCPServer.Start()
func (sw *ServerWrapper) Start() error {
	sw.mutex.Lock()
	defer sw.mutex.Unlock()

	if sw.isRunning {
		return fmt.Errorf("server is already running")
	}

	if sw.sdkServer == nil {
		return fmt.Errorf("SDK server not initialized")
	}

	// Start SSE manager if enabled
	if sw.sdkConfig.SSEEnabled && sw.sseManager != nil {
		sw.sseManager.Start(sw.ctx)
		sw.logger.Printf("SSE manager started")
	}

	serverAddr := sw.configAdapter.GetServerAddress()
	sw.logger.Printf("Starting MCP server (SDK-based) on %s", serverAddr)

	// Create HTTP server with SDK handler
	mux := http.NewServeMux()

	// Create handlers that return our server instance
	baseGetServer := func(r *http.Request) *mcp.Server { return sw.sdkServer }
	// Back-compat root handler (streamable)
	mcpHandler := mcp.NewStreamableHTTPHandler(baseGetServer, nil)
	mux.Handle("/", mcpHandler)

	// Dual transport handler on /mcp to support both http and sse transports
	dual := NewDualTransportHandler(baseGetServer)
	mux.Handle("/mcp", dual)

	// Add health check endpoint
	mux.HandleFunc("/health", sw.handleHealthCheck)

	// Register auth-related routes (e.g., OAuth2 callback)
	sw.registerAuthRoutes(mux)

	// Build handler chain with authentication
	var handler http.Handler = mux
	if sw.unifiedAuthMiddleware != nil {
		handler = sw.unifiedAuthMiddleware.Middleware(handler)
		sw.logger.Printf("Unified authentication middleware enabled")
	} else if sw.ipAuthMiddleware != nil {
		handler = sw.ipAuthMiddleware.Middleware(handler)
		sw.logger.Printf("IP authentication middleware enabled")
	}

	// Always-on access logging
	handler = sw.loggingMiddleware(handler)

	// Create HTTP server
	server := &http.Server{
		Addr:           serverAddr,
		Handler:        handler,
		ReadTimeout:    sw.sdkConfig.ReadTimeout,
		WriteTimeout:   sw.sdkConfig.WriteTimeout,
		IdleTimeout:    sw.sdkConfig.IdleTimeout,
		MaxHeaderBytes: sw.sdkConfig.MaxHeaderBytes,
	}

	// Store server reference for shutdown
	sw.httpServer = server

	// Start the HTTP server in a goroutine
	sw.wg.Add(1)
	go func() {
		defer sw.wg.Done()
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			sw.logger.Printf("HTTP server error: %v", err)
		}
	}()

	// Give the server a moment to start
	time.Sleep(100 * time.Millisecond)

	sw.isRunning = true
	sw.logger.Printf("MCP server (SDK-based) started successfully")

	if sw.sdkConfig.SSEEnabled {
		sw.logger.Printf("SSE endpoints available via SDK server")
	}

	return nil
}

// Stop stops the server with graceful shutdown
// Maintains API compatibility with existing MCPServer.Stop()
func (sw *ServerWrapper) Stop() error {
	sw.mutex.Lock()
	defer sw.mutex.Unlock()

	if !sw.isRunning {
		return fmt.Errorf("server is not running")
	}

	sw.logger.Printf("Stopping MCP server (SDK-based)...")

	// Stop HTTP server
	if sw.httpServer != nil {
		if sw.sdkConfig.GracefulShutdown {
			// Create shutdown context with timeout
			shutdownCtx, cancel := context.WithTimeout(context.Background(), sw.sdkConfig.ShutdownTimeout)
			defer cancel()

			if err := sw.httpServer.Shutdown(shutdownCtx); err != nil {
				sw.logger.Printf("Graceful shutdown failed: %v, forcing immediate shutdown", err)
				if err := sw.httpServer.Close(); err != nil {
					sw.logger.Printf("Failed to close HTTP server: %v", err)
				}
			}
		} else {
			// Immediate shutdown
			if err := sw.httpServer.Close(); err != nil {
				sw.logger.Printf("Failed to close HTTP server: %v", err)
			}
		}
	}

	// Cancel context for SDK server and other components
	sw.cancelFunc()

	// Stop SSE manager if running
	if sw.sseManager != nil {
		// SSE manager will stop when context is cancelled
		sw.logger.Printf("SSE manager stopping")
	}

	// Signal shutdown and wait for goroutines
	close(sw.shutdownChan)
	sw.wg.Wait()

	sw.isRunning = false
	sw.logger.Printf("MCP server (SDK-based) stopped successfully")
	return nil
}

// IsRunning returns whether the server is currently running
// Maintains API compatibility with existing MCPServer
func (sw *ServerWrapper) IsRunning() bool {
	sw.mutex.RLock()
	defer sw.mutex.RUnlock()
	return sw.isRunning
}

// WaitForShutdown waits for the server to be shut down
// Maintains API compatibility with existing MCPServer
func (sw *ServerWrapper) WaitForShutdown() {
	<-sw.shutdownChan
}

// GetConfig returns the current server configuration
// Maintains API compatibility with existing MCPServer
func (sw *ServerWrapper) GetConfig() *SDKServerConfig {
	return sw.sdkConfig
}

// GetRAGentConfig returns the original RAGent configuration
func (sw *ServerWrapper) GetRAGentConfig() *types.Config {
	return sw.ragentConfig
}

// GetSDKServer returns the underlying SDK server instance
// This is specific to the wrapper and not in the original MCPServer
func (sw *ServerWrapper) GetSDKServer() *mcp.Server {
	return sw.sdkServer
}

// SetLogger sets a custom logger
// Maintains API compatibility with existing MCPServer
func (sw *ServerWrapper) SetLogger(logger *log.Logger) {
	sw.mutex.Lock()
	defer sw.mutex.Unlock()

	if logger != nil {
		sw.logger = logger
	}
}

// GetServerInfo returns information about the server
// Maintains API compatibility with existing MCPServer
func (sw *ServerWrapper) GetServerInfo() map[string]interface{} {
	sw.mutex.RLock()
	defer sw.mutex.RUnlock()

	return map[string]interface{}{
		"server_type":       "SDK-based",
		"sdk_version":       "v0.4.0", // This should be dynamically determined
		"host":              sw.sdkConfig.Host,
		"port":              sw.sdkConfig.Port,
		"is_running":        sw.isRunning,
		"sse_enabled":       sw.sdkConfig.SSEEnabled,
		"ip_auth_enabled":   sw.sdkConfig.IPAuthEnabled,
		"graceful_shutdown": sw.sdkConfig.GracefulShutdown,
		"tools_registered":  len(sw.toolRegistry.tools), // Assuming tools field exists
		"configuration": map[string]interface{}{
			"read_timeout":     sw.sdkConfig.ReadTimeout,
			"write_timeout":    sw.sdkConfig.WriteTimeout,
			"idle_timeout":     sw.sdkConfig.IdleTimeout,
			"max_header_bytes": sw.sdkConfig.MaxHeaderBytes,
		},
	}
}

// GetServerAddress returns the full server address
func (sw *ServerWrapper) GetServerAddress() string {
	return sw.configAdapter.GetServerAddress()
}

// IsSecureTransport returns whether the server uses secure transport
func (sw *ServerWrapper) IsSecureTransport() bool {
	return sw.configAdapter.IsSecureTransport()
}

// handleHealthCheck handles health check requests
func (sw *ServerWrapper) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	status := map[string]interface{}{
		"status":      "healthy",
		"server_type": "SDK-based",
		"sdk_version": "v0.4.0",
		"running":     sw.isRunning,
		"address":     sw.GetServerAddress(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Simple JSON encoding for health check
	response := fmt.Sprintf(`{"status":"%v","server_type":"%v","sdk_version":"%v","running":%v,"address":"%v"}`,
		status["status"], status["server_type"], status["sdk_version"], status["running"], status["address"])
	if _, err := w.Write([]byte(response)); err != nil {
		sw.logger.Printf("Failed to write response: %v", err)
	}
}

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
	size   int64
}

func newLoggingResponseWriter(w http.ResponseWriter) *loggingResponseWriter {
	return &loggingResponseWriter{ResponseWriter: w, status: http.StatusOK}
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.status = code
	lrw.ResponseWriter.WriteHeader(code)
}

func (lrw *loggingResponseWriter) Write(b []byte) (int, error) {
	n, err := lrw.ResponseWriter.Write(b)
	lrw.size += int64(n)
	return n, err
}

func (sw *ServerWrapper) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lrw := newLoggingResponseWriter(w)
		next.ServeHTTP(lrw, r)

		forwarded := strings.Join(r.Header.Values("X-Forwarded-For"), ",")
		clientIP := sw.extractClientIP(r)
		sw.logger.Printf(
			"Request: %s %s status=%d bytes=%d duration=%s remote=%s client_ip=%s forwarded=%s user_agent=%q",
			r.Method,
			r.URL.Path,
			lrw.status,
			lrw.size,
			time.Since(start),
			r.RemoteAddr,
			clientIP,
			forwarded,
			r.Header.Get("User-Agent"),
		)
	})
}

func (sw *ServerWrapper) extractClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			candidate := strings.TrimSpace(parts[0])
			if candidate != "" {
				return candidate
			}
		}
	}

	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// registerAuthRoutes registers authentication-related HTTP routes on the server mux
func (sw *ServerWrapper) registerAuthRoutes(mux *http.ServeMux) {
	// Register OIDC callback handler if OIDC is configured via unified middleware
	if sw.unifiedAuthMiddleware != nil {
		if oidc := sw.unifiedAuthMiddleware.GetOIDCAuthMiddleware(); oidc != nil {
			mux.HandleFunc("/callback", oidc.HandleCallback)
			sw.logger.Printf("Registered OIDC callback route: /callback")
			mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
				// Generate request-bound auth URL and redirect
				authURL := oidc.GetAuthURLForRequest(r)
				http.Redirect(w, r, authURL, http.StatusFound)
			})
			sw.logger.Printf("Registered OIDC login route: /login")
		}
	}
}
