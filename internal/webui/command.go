package webui

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// WebUIOptions holds options for the webui command.
type WebUIOptions struct {
	Host      string
	Port      int
	Directory string
}

// RunWebUI initializes and starts the Web UI server.
func RunWebUI(ctx context.Context, opts WebUIOptions) error {
	logger := log.New(os.Stdout, "[webui] ", log.LstdFlags)
	serverConfig := &ServerConfig{
		Host:            opts.Host,
		Port:            opts.Port,
		ReadTimeout:     30 * time.Second,
		WriteTimeout:    30 * time.Second,
		IdleTimeout:     120 * time.Second,
		ShutdownTimeout: 30 * time.Second,
		Directory:       opts.Directory,
	}
	server, err := NewServer(serverConfig, logger)
	if err != nil {
		return fmt.Errorf("failed to create webui server: %w", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		logger.Printf("Received signal: %v", sig)
		cancel()
	}()
	return server.Run(ctx)
}
