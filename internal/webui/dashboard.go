package webui

import (
	"context"
	"fmt"
	"log"
	"net/http"
)

// SetupDashboard creates and initialises a dashboard, returning a ready-to-use
// HTTP handler and a cleanup function.
func SetupDashboard(
	cfg *ServerConfig,
	deps *Dependencies,
	logger *log.Logger,
) (handler http.Handler, cleanup func(), err error) {
	srv, err := NewServer(cfg, deps, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create dashboard server: %w", err)
	}
	if err := srv.Initialize(context.Background()); err != nil {
		return nil, nil, fmt.Errorf("failed to initialize dashboard: %w", err)
	}
	return srv.Handler(), srv.Cleanup, nil
}
