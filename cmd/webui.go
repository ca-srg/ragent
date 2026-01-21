package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/ca-srg/ragent/internal/webui"
)

var (
	webuiHost      string
	webuiPort      int
	webuiDirectory string
)

var webuiCmd = &cobra.Command{
	Use:   "webui",
	Short: "Start the Web UI server for monitoring and controlling vectorization",
	Long: `
The webui command starts a local web server that provides:
- Real-time vectorization progress monitoring
- File browser for vectorization targets
- Processing history and error details
- Follow mode scheduling controls

The web UI uses HTMX for live updates without requiring a JavaScript framework.

Example:
  ragent webui                          # Start with defaults (localhost:8081)
  ragent webui --port 8080              # Use custom port
  ragent webui --directory ./docs       # Specify source directory
`,
	RunE: runWebUI,
}

func init() {
	webuiCmd.Flags().StringVar(&webuiHost, "host", "localhost",
		"Host to bind the web server")
	webuiCmd.Flags().IntVarP(&webuiPort, "port", "p", 8081,
		"Port to bind the web server")
	webuiCmd.Flags().StringVarP(&webuiDirectory, "directory", "d", "./source",
		"Directory containing source files to process")
}

func runWebUI(cmd *cobra.Command, args []string) error {
	logger := log.New(os.Stdout, "[webui] ", log.LstdFlags)

	// Create server config
	serverConfig := &webui.ServerConfig{
		Host:            webuiHost,
		Port:            webuiPort,
		ReadTimeout:     30 * time.Second,
		WriteTimeout:    30 * time.Second,
		IdleTimeout:     120 * time.Second,
		ShutdownTimeout: 30 * time.Second,
		Directory:       webuiDirectory,
	}

	// Create server
	server, err := webui.NewServer(serverConfig, logger)
	if err != nil {
		return fmt.Errorf("failed to create webui server: %w", err)
	}

	// Create context with signal handling
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		logger.Printf("Received signal: %v", sig)
		cancel()
	}()

	// Run server
	return server.Run(ctx)
}
