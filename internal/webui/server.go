package webui

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"sync"
	"time"

	domain "github.com/ca-srg/ragent/internal/pkg/domain"
	"github.com/ca-srg/ragent/internal/pkg/ipc"
)

// ServerConfig holds the web UI server configuration
type ServerConfig struct {
	Host            string
	Port            int
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
	Directory       string // Source directory for vectorization
	BasePath        string // URL prefix when embedded (e.g., "/dashboard"); empty for standalone
}

// DefaultServerConfig returns the default server configuration
func DefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		Host:            "localhost",
		Port:            8081,
		ReadTimeout:     30 * time.Second,
		WriteTimeout:    30 * time.Second,
		IdleTimeout:     120 * time.Second,
		ShutdownTimeout: 30 * time.Second,
		Directory:       "./source",
	}
}

// Server represents the web UI server
type Server struct {
	config       *ServerConfig
	httpServer   *http.Server
	templates    *TemplateManager
	state        *VectorizeState
	sseManager   *SSEManager
	vectorizer   domain.Vectorizer
	fileScanner  domain.FileScanner
	ipcClient    *ipc.Client
	logger       *log.Logger
	cancelFunc   context.CancelFunc
	shutdownOnce sync.Once
}

// NewServer creates a new web UI server
func NewServer(serverConfig *ServerConfig, deps *Dependencies, logger *log.Logger) (*Server, error) {
	if serverConfig == nil {
		serverConfig = DefaultServerConfig()
	}
	if deps == nil {
		return nil, fmt.Errorf("dependencies are required")
	}
	if deps.FileScanner == nil {
		return nil, fmt.Errorf("file scanner dependency is required")
	}
	if deps.Vectorizer == nil {
		return nil, fmt.Errorf("vectorizer dependency is required")
	}
	if logger == nil {
		logger = log.New(log.Writer(), "[webui] ", log.LstdFlags)
	}

	// Create SSE manager
	sseManager := NewSSEManager(&SSEConfig{
		HeartbeatInterval: 30 * time.Second,
		BufferSize:        100,
		MaxClients:        100,
	}, logger)

	// Create state manager
	state := NewVectorizeState(sseManager)

	// Create template manager
	templates, err := NewTemplateManagerWithBasePath(serverConfig.BasePath)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize templates: %w", err)
	}

	// Create IPC client for external process status
	ipcClient := ipc.NewClient(ipc.ClientConfig{})

	s := &Server{
		config:      serverConfig,
		templates:   templates,
		state:       state,
		sseManager:  sseManager,
		vectorizer:  deps.Vectorizer,
		fileScanner: deps.FileScanner,
		ipcClient:   ipcClient,
		logger:      logger,
	}

	return s, nil
}

// Initialize prepares the server for handling requests (SSE)
// without starting an HTTP listener. Call this before Handler() when embedding.
func (s *Server) Initialize(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	s.cancelFunc = cancel

	s.sseManager.Start(ctx)
	return nil
}

// Handler returns the HTTP handler for embedding in another server.
// Call Initialize() before this method.
func (s *Server) Handler() http.Handler {
	mux := s.setupRoutes()
	return mux
}

// Cleanup stops the SSE manager. Call on shutdown when embedded.
func (s *Server) Cleanup() {
	s.sseManager.Stop()
	if s.cancelFunc != nil {
		s.cancelFunc()
	}
}

// Run starts the server and blocks until context is cancelled
func (s *Server) Run(ctx context.Context) error {
	if err := s.Initialize(ctx); err != nil {
		return err
	}
	defer s.Cleanup()

	handler := s.Handler()
	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", s.config.Host, s.config.Port),
		Handler:      handler,
		ReadTimeout:  s.config.ReadTimeout,
		WriteTimeout: s.config.WriteTimeout,
		IdleTimeout:  s.config.IdleTimeout,
	}

	errChan := make(chan error, 1)
	go func() {
		s.logger.Printf("Starting Web UI server at http://%s:%d", s.config.Host, s.config.Port)
		if err := s.httpServer.ListenAndServe(); err != http.ErrServerClosed {
			errChan <- err
		}
		close(errChan)
	}()

	select {
	case <-ctx.Done():
		return s.shutdown()
	case err := <-errChan:
		return err
	}
}

// shutdown performs graceful shutdown
func (s *Server) shutdown() error {
	var shutdownErr error
	s.shutdownOnce.Do(func() {
		s.logger.Println("Shutting down server...")

		// Shutdown HTTP server
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.config.ShutdownTimeout)
		defer cancel()

		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			shutdownErr = fmt.Errorf("server shutdown error: %w", err)
		}
	})
	return shutdownErr
}

// setupRoutes configures HTTP routes
func (s *Server) setupRoutes() *http.ServeMux {
	mux := http.NewServeMux()

	// Static files
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		s.logger.Printf("Warning: failed to setup static files: %v", err)
	} else {
		mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	}

	// Pages
	mux.HandleFunc("/", s.handleDashboard)
	mux.HandleFunc("/files", s.handleFilesPage)
	mux.HandleFunc("/history", s.handleHistoryPage)

	// API endpoints
	mux.HandleFunc("/api/status", s.handleAPIStatus)
	mux.HandleFunc("/api/upload", s.handleUpload)
	mux.HandleFunc("/api/files", s.handleAPIFiles)
	mux.HandleFunc("/api/history", s.handleAPIHistory)
	mux.HandleFunc("/api/errors", s.handleAPIErrors)

	// SSE endpoints
	mux.HandleFunc("/sse/events", s.handleSSEEvents)

	// HTMX partials
	mux.HandleFunc("/partials/progress", s.handlePartialProgress)
	mux.HandleFunc("/partials/file-list", s.handlePartialFileList)
	mux.HandleFunc("/partials/error-list", s.handlePartialErrorList)

	return mux
}

// GetState returns the current state
func (s *Server) GetState() *VectorizeState {
	return s.state
}

// GetExternalProcessStatus returns the status of an external vectorize process via IPC
func (s *Server) GetExternalProcessStatus(ctx context.Context) (*ipc.FullStatusResponse, error) {
	if s.ipcClient == nil {
		return nil, nil
	}

	fullStatus, err := s.ipcClient.GetFullStatus(ctx)
	if err != nil {
		// Not running or other error - return nil (no external process)
		if err == ipc.ErrNotRunning || err == ipc.ErrStaleSocket {
			return nil, nil
		}
		return nil, err
	}

	return fullStatus, nil
}

// IsExternalProcessRunning checks if an external vectorize process is running
func (s *Server) IsExternalProcessRunning(ctx context.Context) bool {
	if s.ipcClient == nil {
		return false
	}
	return s.ipcClient.IsRunning(ctx)
}
