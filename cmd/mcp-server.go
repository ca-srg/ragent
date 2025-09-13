package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/spf13/cobra"

	appcfg "github.com/ca-srg/ragent/internal/config"
	"github.com/ca-srg/ragent/internal/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/mcpserver"
	"github.com/ca-srg/ragent/internal/opensearch"
)

var (
	// Command line flags for MCP server
	mcpServerHost          string
	mcpServerPort          int
	mcpAllowedIPs          []string
	mcpEnableIPAuth        bool
	mcpEnableAccessLog     bool
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
	RunE: runMCPServer,
}

func init() {
	// Server configuration flags
	mcpServerCmd.Flags().StringVar(&mcpServerHost, "host", "localhost", "Server host address")
	mcpServerCmd.Flags().IntVar(&mcpServerPort, "port", 8080, "Server port")
	mcpServerCmd.Flags().StringSliceVar(&mcpAllowedIPs, "allowed-ips", []string{"127.0.0.1", "::1"}, "Comma-separated list of allowed IP addresses/ranges")
	mcpServerCmd.Flags().BoolVar(&mcpEnableIPAuth, "enable-ip-auth", true, "Enable IP-based authentication")
	mcpServerCmd.Flags().BoolVar(&mcpEnableAccessLog, "enable-access-log", true, "Enable HTTP access logging")

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
}

func runMCPServer(cmd *cobra.Command, args []string) error {
	// Load configuration
	cfg, err := appcfg.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Override configuration with command line flags if provided
	if cmd.Flags().Changed("host") {
		cfg.MCPServerHost = mcpServerHost
	}
	if cmd.Flags().Changed("port") {
		cfg.MCPServerPort = mcpServerPort
	}
	if cmd.Flags().Changed("allowed-ips") {
		cfg.MCPAllowedIPs = mcpAllowedIPs
	}
	if cmd.Flags().Changed("enable-ip-auth") {
		cfg.MCPIPAuthEnabled = mcpEnableIPAuth
	}
	if cmd.Flags().Changed("enable-access-log") {
		cfg.MCPServerEnableAccessLogging = mcpEnableAccessLog
	}
	if cmd.Flags().Changed("default-index") {
		cfg.OpenSearchIndex = mcpDefaultIndexName
	}
	if cmd.Flags().Changed("default-search-size") {
		cfg.MCPDefaultSearchSize = mcpDefaultSearchSize
	}
	if cmd.Flags().Changed("default-bm25-weight") {
		cfg.MCPDefaultBM25Weight = mcpDefaultBM25Weight
	}
	if cmd.Flags().Changed("default-vector-weight") {
		cfg.MCPDefaultVectorWeight = mcpDefaultVectorWeight
	}

	// Validate OpenSearch configuration (required for MCP server)
	if cfg.OpenSearchEndpoint == "" {
		return fmt.Errorf("OpenSearch is required for MCP server: set OPENSEARCH_ENDPOINT and related settings")
	}

	logger := log.New(os.Stdout, "[MCP Server] ", log.LstdFlags)

	// Create SDK-based server wrapper using RAGent configuration
	// The ServerWrapper will handle configuration conversion internally
	server, err := mcpserver.NewServerWrapper(cfg)
	if err != nil {
		return fmt.Errorf("failed to create server wrapper: %w", err)
	}
	server.SetLogger(logger)

	// Configure authentication
	// If auth-method is provided, prefer unified auth routing (supports ip/oidc/both/either)
	method := mcpAuthMethod
	if method == "" {
		method = "ip"
	}

	// Normalize method to lowercase
	switch method {
	case "ip", "oidc", "both", "either":
	default:
		return fmt.Errorf("invalid auth-method: %s (allowed: ip|oidc|both|either)", method)
	}

	if method == "ip" && cfg.MCPIPAuthEnabled {
		// Backward-compatible IP-only behavior
		ipAuthAdapter, err := mcpserver.NewIPAuthMiddlewareAdapter(cfg.MCPAllowedIPs, cfg.MCPIPAuthEnableLogging)
		if err != nil {
			return fmt.Errorf("failed to create IP authentication middleware: %w", err)
		}
		server.SetIPAuthMiddleware(ipAuthAdapter.GetIPAuthMiddleware())
		logger.Printf("IP authentication enabled for IPs: %v", cfg.MCPAllowedIPs)
	} else if method != "ip" { // oidc/both/either
		// Resolve OIDC values from env if not provided
		if oidcClientID == "" {
			oidcClientID = os.Getenv("OIDC_CLIENT_ID")
		}
		if oidcClientSecret == "" {
			oidcClientSecret = os.Getenv("OIDC_CLIENT_SECRET")
		}
		if !oidcSkipDiscovery {
			if oidcIssuer == "" {
				oidcIssuer = os.Getenv("OIDC_ISSUER")
			}
		}
		if oidcAuthURL == "" {
			oidcAuthURL = os.Getenv("OIDC_AUTH_URL")
		}
		if oidcTokenURL == "" {
			oidcTokenURL = os.Getenv("OIDC_TOKEN_URL")
		}
		if oidcUserInfoURL == "" {
			oidcUserInfoURL = os.Getenv("OIDC_USERINFO_URL")
		}
		if oidcJWKSURL == "" {
			oidcJWKSURL = os.Getenv("OIDC_JWKS_URL")
		}

		// Validate minimal OIDC requirements
		useCustomEndpoints := oidcSkipDiscovery || (oidcAuthURL != "" && oidcTokenURL != "")
		if useCustomEndpoints {
			if oidcAuthURL == "" || oidcTokenURL == "" {
				return fmt.Errorf("authorization URL and token URL are required when using custom endpoints")
			}
			if oidcClientID == "" {
				return fmt.Errorf("client ID is required for %s authentication", method)
			}
		} else {
			if oidcIssuer == "" || oidcClientID == "" {
				return fmt.Errorf("OIDC issuer and client ID are required for %s authentication", method)
			}
		}

		// Build unified auth config
		var authMethod mcpserver.AuthMethod
		switch method {
		case "oidc":
			authMethod = mcpserver.AuthMethodOIDC
		case "both":
			authMethod = mcpserver.AuthMethodBoth
		case "either":
			authMethod = mcpserver.AuthMethodEither
		}

		unifiedCfg := &mcpserver.UnifiedAuthConfig{
			AuthMethod:    authMethod,
			EnableLogging: mcpAuthEnableLogging,
		}

		// IP part (for both/either)
		if authMethod == mcpserver.AuthMethodBoth || authMethod == mcpserver.AuthMethodEither {
			unifiedCfg.IPConfig = &mcpserver.IPAuthConfig{
				AllowedIPs:    cfg.MCPAllowedIPs,
				EnableLogging: cfg.MCPIPAuthEnableLogging,
			}
		}

		// OIDC part
		unifiedCfg.OIDCConfig = &mcpserver.OIDCConfig{
			Issuer:       oidcIssuer,
			ClientID:     oidcClientID,
			ClientSecret: oidcClientSecret,
			Scopes:       oidcScopes,
			// OAuth2 callback will use MCP_SERVER_PORT
			CallbackPort:     cfg.MCPServerPort,
			EnableLogging:    mcpAuthEnableLogging,
			AuthorizationURL: oidcAuthURL,
			TokenURL:         oidcTokenURL,
			UserInfoURL:      oidcUserInfoURL,
			JWKSURL:          oidcJWKSURL,
			SkipDiscovery:    oidcSkipDiscovery,
		}

		unified, err := mcpserver.NewUnifiedAuthMiddleware(unifiedCfg)
		if err != nil {
			return fmt.Errorf("failed to create unified auth middleware: %w", err)
		}
		server.SetUnifiedAuthMiddleware(unified)

		logger.Printf("Unified auth enabled (method=%s)", method)
		if method == "oidc" || method == "both" || method == "either" {
			if useCustomEndpoints {
				logger.Printf("OIDC custom endpoints: auth=%s token=%s", oidcAuthURL, oidcTokenURL)
				if oidcUserInfoURL != "" {
					logger.Printf("OIDC userinfo=%s", oidcUserInfoURL)
				}
			} else {
				logger.Printf("OIDC issuer: %s", oidcIssuer)
			}
			logger.Printf("OAuth2 callback port: %d", cfg.MCPServerPort)
		}
	} else {
		logger.Printf("WARNING: No authentication middleware enabled (auth-method=%s)", method)
	}

	// Initialize OpenSearch client
	osConfig, err := opensearch.NewConfigFromTypes(cfg)
	if err != nil {
		return fmt.Errorf("failed to create OpenSearch config: %w", err)
	}

	if err := osConfig.Validate(); err != nil {
		return fmt.Errorf("OpenSearch config validation failed: %w", err)
	}

	osClient, err := opensearch.NewClient(osConfig)
	if err != nil {
		return fmt.Errorf("failed to create OpenSearch client: %w", err)
	}

	// Test OpenSearch connection
	ctx := context.Background()
	if err := osClient.HealthCheck(ctx); err != nil {
		return fmt.Errorf("OpenSearch health check failed: %w", err)
	}
	logger.Printf("OpenSearch connection established: %s", cfg.OpenSearchEndpoint)

	// Load AWS configuration
	awsConfig, err := config.LoadDefaultConfig(ctx, config.WithRegion(cfg.AWSS3Region))
	if err != nil {
		return fmt.Errorf("failed to load AWS configuration: %w", err)
	}

	// Initialize Bedrock embedding client
	embeddingClient := bedrock.NewBedrockClient(awsConfig, "amazon.titan-embed-text-v2:0")

	// Create hybrid search tool configuration
	hybridSearchConfig := &mcpserver.HybridSearchConfig{
		DefaultIndexName:      cfg.OpenSearchIndex,
		DefaultSize:           cfg.MCPDefaultSearchSize,
		DefaultBM25Weight:     cfg.MCPDefaultBM25Weight,
		DefaultVectorWeight:   cfg.MCPDefaultVectorWeight,
		DefaultFusionMethod:   "weighted_sum",
		DefaultUseJapaneseNLP: cfg.MCPDefaultUseJapaneseNLP,
		DefaultTimeoutSeconds: cfg.MCPDefaultTimeoutSeconds,
	}

	// Create hybrid search tool handler for SDK integration
	hybridSearchHandler := mcpserver.NewHybridSearchHandler(osClient, embeddingClient, hybridSearchConfig)

	// Create function wrapper to match mcp.ToolHandler signature
	toolHandlerFunc := hybridSearchHandler.HandleSDKToolCall

	// Determine tool name
	toolName := cfg.MCPHybridSearchToolName
	if cfg.MCPToolPrefix != "" {
		toolName = cfg.MCPToolPrefix + toolName
	}

	// Register tool with SDK server through ServerWrapper
	err = server.RegisterTool(toolName, toolHandlerFunc)
	if err != nil {
		return fmt.Errorf("failed to register hybrid search tool: %w", err)
	}

	logger.Printf("Registered tool '%s' with SDK server", toolName)

	// Setup graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Printf("Received shutdown signal, stopping server...")
		cancel()

		// Give the server a moment to finish current requests
		time.Sleep(1 * time.Second)

		if err := server.Stop(); err != nil {
			logger.Printf("Error during server shutdown: %v", err)
		}
	}()

	// Start the SDK-based server
	logger.Printf("Starting MCP server (SDK-based) on %s:%d", cfg.MCPServerHost, cfg.MCPServerPort)
	logger.Printf("Server address: %s", server.GetServerAddress())
	logger.Printf("Available tools: %s", toolName)

	if err := server.Start(); err != nil {
		return fmt.Errorf("failed to start MCP server: %w", err)
	}

	// Wait for shutdown signal
	<-ctx.Done()

	logger.Printf("MCP server (SDK-based) stopped successfully")
	return nil
}
