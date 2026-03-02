package cmd

import (
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
	RunE: func(cmd *cobra.Command, args []string) error { return webui.RunWebUI(cmd.Context(), webui.WebUIOptions{Host: webuiHost, Port: webuiPort, Directory: webuiDirectory}) },
}

func init() {
	webuiCmd.Flags().StringVar(&webuiHost, "host", "localhost",
		"Host to bind the web server")
	webuiCmd.Flags().IntVarP(&webuiPort, "port", "p", 8081,
		"Port to bind the web server")
	webuiCmd.Flags().StringVarP(&webuiDirectory, "directory", "d", "./source",
		"Directory containing source files to process")
}

