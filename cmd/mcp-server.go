package cmd

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"

	"github.com/ca-srg/ragent/internal/ingestion"
	"github.com/ca-srg/ragent/internal/mcpserver"
	"github.com/ca-srg/ragent/internal/webui"
)

var (
	// Command line flags for MCP server
	mcpServerHost          string
	mcpServerPort          int
	mcpAllowedIPs          []string
	mcpEnableIPAuth        bool
	mcpDefaultIndexName    string
	mcpDefaultSearchSize   int
	mcpDefaultBM25Weight   float64
	mcpDefaultVectorWeight float64

	// Unified authentication flags (OIDC/IP)
	mcpAuthMethod        string
	mcpAuthEnableLogging bool
	oidcIssuer           string
	oidcClientID         string
	oidcClientSecret     string
	oidcScopes           []string
	oidcAuthURL          string
	oidcTokenURL         string
	oidcUserInfoURL      string
	oidcJWKSURL          string
	oidcSkipDiscovery    bool

	// Bypass IP authentication flags
	mcpBypassIPRanges     []string
	mcpBypassVerboseLog   bool
	mcpBypassAuditLog     bool
	mcpTrustedProxies     []string
	mcpOnlySlack          bool
	mcpExportEval         bool
	mcpExportEvalPath     string
	mcpDashboardDirectory string
)

var mcpServerCmd = &cobra.Command{
	Use:   "mcp-server",
	Short: "Start MCP (Model Context Protocol) server for hybrid search",
	Long: `
Start an MCP server that exposes RAGent's hybrid search capabilities as tools
that can be used by MCP-compatible clients like Claude Desktop, IDEs, and other applications.

The server provides a "hybrid_search" tool that combines BM25 and vector search 
using OpenSearch and Amazon Bedrock for high-quality document retrieval.

Configuration is loaded from environment variables (see README for details).

Examples:
  ragent mcp-server                                    # Start server with default settings
  ragent mcp-server --port 9000                       # Use custom port
  ragent mcp-server --host 0.0.0.0 --disable-ip-auth # Allow all IPs (not recommended)
  ragent mcp-server --allowed-ips "192.168.1.0/24"   # Allow specific IP range
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := mcpserver.MCPServerOptions{
			AuthMethod:        mcpAuthMethod,
			AuthEnableLogging: mcpAuthEnableLogging,
			OIDCIssuer:        oidcIssuer,
			OIDCClientID:      oidcClientID,
			OIDCClientSecret:  oidcClientSecret,
			OIDCScopes:        oidcScopes,
			OIDCAuthURL:       oidcAuthURL,
			OIDCTokenURL:      oidcTokenURL,
			OIDCUserInfoURL:   oidcUserInfoURL,
			OIDCJWKSURL:       oidcJWKSURL,
			OIDCSkipDiscovery: oidcSkipDiscovery,
			BypassIPRanges:    mcpBypassIPRanges,
			BypassVerboseLog:  mcpBypassVerboseLog,
			BypassAuditLog:    mcpBypassAuditLog,
			TrustedProxies:    mcpTrustedProxies,
			OnlySlack:         mcpOnlySlack,
			ExportEval:        mcpExportEval,
			ExportEvalPath:    mcpExportEvalPath,
		}

		dashboardDir := mcpDashboardDirectory
		if dashboardDir == "" {
			dashboardDir = "./source"
		}

		fs, vec, err := ingestion.BuildDashboardDependencies()
		if err != nil {
			return fmt.Errorf("failed to build dashboard dependencies: %w", err)
		}

		handler, cleanup, err := webui.SetupDashboard(
			&webui.ServerConfig{Directory: dashboardDir, BasePath: "/dashboard"},
			&webui.Dependencies{FileScanner: fs, Vectorizer: vec},
			log.New(os.Stdout, "[dashboard] ", log.LstdFlags),
		)
		if err != nil {
			return fmt.Errorf("failed to setup dashboard: %w", err)
		}
		opts.DashboardHandler = handler
		opts.DashboardCleanup = cleanup
		opts.DashboardBasePath = "/dashboard"

		return mcpserver.RunMCPServer(context.Background(), cmd, opts)
	},
}

func init() {
	// Server configuration flags
	mcpServerCmd.Flags().StringVar(&mcpServerHost, "host", "localhost", "Server host address")
	mcpServerCmd.Flags().IntVar(&mcpServerPort, "port", 8080, "Server port")
	mcpServerCmd.Flags().StringSliceVar(&mcpAllowedIPs, "allowed-ips", []string{"127.0.0.1", "::1"}, "Comma-separated list of allowed IP addresses/ranges")
	mcpServerCmd.Flags().BoolVar(&mcpEnableIPAuth, "enable-ip-auth", true, "Enable IP-based authentication")

	// Authentication (unified) flags
	mcpServerCmd.Flags().StringVar(&mcpAuthMethod, "auth-method", "ip", "Authentication method: ip, oidc, both, either")
	mcpServerCmd.Flags().BoolVar(&mcpAuthEnableLogging, "auth-enable-logging", true, "Enable detailed auth logging")

	// OIDC flags (used when auth-method is oidc/both/either)
	mcpServerCmd.Flags().StringVar(&oidcIssuer, "oidc-issuer", "", "OIDC issuer URL (e.g., https://accounts.google.com)")
	mcpServerCmd.Flags().StringVar(&oidcClientID, "oidc-client-id", "", "OIDC client ID")
	mcpServerCmd.Flags().StringVar(&oidcClientSecret, "oidc-client-secret", "", "OIDC client secret")
	mcpServerCmd.Flags().StringSliceVar(&oidcScopes, "oidc-scopes", []string{"openid", "profile", "email"}, "OIDC scopes")
	mcpServerCmd.Flags().StringVar(&oidcAuthURL, "oidc-auth-url", "", "Custom authorization endpoint URL")
	mcpServerCmd.Flags().StringVar(&oidcTokenURL, "oidc-token-url", "", "Custom token endpoint URL")
	mcpServerCmd.Flags().StringVar(&oidcUserInfoURL, "oidc-userinfo-url", "", "Custom userinfo endpoint URL")
	mcpServerCmd.Flags().StringVar(&oidcJWKSURL, "oidc-jwks-url", "", "Custom JWKS endpoint URL")
	mcpServerCmd.Flags().BoolVar(&oidcSkipDiscovery, "oidc-skip-discovery", false, "Skip OIDC discovery and use only custom endpoints")

	// Search configuration flags
	mcpServerCmd.Flags().StringVar(&mcpDefaultIndexName, "default-index", "ragent-docs", "Default OpenSearch index name")
	mcpServerCmd.Flags().IntVar(&mcpDefaultSearchSize, "default-search-size", 10, "Default number of search results")
	mcpServerCmd.Flags().Float64Var(&mcpDefaultBM25Weight, "default-bm25-weight", 0.5, "Default BM25 weight for hybrid search")
	mcpServerCmd.Flags().Float64Var(&mcpDefaultVectorWeight, "default-vector-weight", 0.5, "Default vector weight for hybrid search")

	// Bypass IP authentication flags
	mcpServerCmd.Flags().StringSliceVar(&mcpBypassIPRanges, "bypass-ip-range", []string{}, "Comma-separated list of IP ranges to bypass authentication (CIDR format)")
	mcpServerCmd.Flags().BoolVar(&mcpBypassVerboseLog, "bypass-verbose-log", false, "Enable verbose logging for bypass authentication")
	mcpServerCmd.Flags().BoolVar(&mcpBypassAuditLog, "bypass-audit-log", true, "Enable audit logging for bypass authentication")
	mcpServerCmd.Flags().StringSliceVar(&mcpTrustedProxies, "trusted-proxies", []string{}, "Comma-separated list of trusted proxy IPs for X-Forwarded-For processing")
	mcpServerCmd.Flags().BoolVar(&mcpOnlySlack, "only-slack", false, "Run in Slack-only mode (skip OpenSearch, provide only slack_search tool)")
	mcpServerCmd.Flags().BoolVar(&mcpExportEval, "export-eval", false, "Enable evaluation data export")
	mcpServerCmd.Flags().StringVar(&mcpExportEvalPath, "export-eval-path", "./evaluation/exports/", "Output directory for JSONL evaluation data")

	mcpServerCmd.Flags().StringVar(&mcpDashboardDirectory, "dashboard-directory", "./source", "Source directory for dashboard vectorization")
}
