package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/slack-go/slack"
	"github.com/spf13/pflag"

	appcfg "github.com/ca-srg/ragent/internal/pkg/config"
	"github.com/ca-srg/ragent/internal/pkg/embedding/bedrock"
	"github.com/ca-srg/ragent/internal/pkg/metrics"
	"github.com/ca-srg/ragent/internal/pkg/observability"
	"github.com/ca-srg/ragent/internal/pkg/opensearch"
	"github.com/ca-srg/ragent/internal/pkg/slacksearch"
)

// FlagChecker is a minimal interface so RunMCPServer can call cmd.Flags().Changed()
// without depending on cobra directly.
type FlagChecker interface {
	Flags() *pflag.FlagSet
}

// MCPServerOptions holds all the command-line flag values for the mcp-server command.
type MCPServerOptions struct {
	AuthMethod        string
	AuthEnableLogging bool
	OIDCIssuer        string
	OIDCClientID      string
	OIDCClientSecret  string
	OIDCScopes        []string
	OIDCAuthURL       string
	OIDCTokenURL      string
	OIDCUserInfoURL   string
	OIDCJWKSURL       string
	OIDCSkipDiscovery bool
	BypassIPRanges    []string
	BypassVerboseLog  bool
	BypassAuditLog    bool
	TrustedProxies    []string
	OnlySlack         bool
}

// RunMCPServer is the entry point for the mcp-server command.
// cmd is passed so it can use cmd.Flags().Changed(...) to detect explicit flag overrides.
func RunMCPServer(ctx context.Context, cmd FlagChecker, opts MCPServerOptions) error {
	// Note: MCP invocations are now recorded per tool call in HandleSDKToolCall()
	// metrics.RecordInvocation(metrics.ModeMCP) - removed to avoid counting server startup

	// Load configuration
	cfg, err := appcfg.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	logger := log.New(os.Stdout, "[MCP Server] ", log.LstdFlags)

	// Override configuration with command line flags if provided
	if cmd.Flags().Changed("host") {
		cfg.MCPServerHost = cmd.Flags().Lookup("host").Value.String()
	}
	if cmd.Flags().Changed("port") {
		if p, err := cmd.Flags().GetInt("port"); err == nil {
			cfg.MCPServerPort = p
		}
	}
	if cmd.Flags().Changed("allowed-ips") {
		if ips, err := cmd.Flags().GetStringSlice("allowed-ips"); err == nil {
			cfg.MCPAllowedIPs = ips
		}
	}
	if cmd.Flags().Changed("enable-ip-auth") {
		if v, err := cmd.Flags().GetBool("enable-ip-auth"); err == nil {
			cfg.MCPIPAuthEnabled = v
		}
	}
	if cmd.Flags().Changed("default-index") {
		cfg.OpenSearchIndex = cmd.Flags().Lookup("default-index").Value.String()
	}
	if cmd.Flags().Changed("default-search-size") {
		if v, err := cmd.Flags().GetInt("default-search-size"); err == nil {
			cfg.MCPDefaultSearchSize = v
		}
	}
	if cmd.Flags().Changed("default-bm25-weight") {
		if v, err := cmd.Flags().GetFloat64("default-bm25-weight"); err == nil {
			cfg.MCPDefaultBM25Weight = v
		}
	}
	if cmd.Flags().Changed("default-vector-weight") {
		if v, err := cmd.Flags().GetFloat64("default-vector-weight"); err == nil {
			cfg.MCPDefaultVectorWeight = v
		}
	}
	// Override bypass configuration with command line flags if provided
	if cmd.Flags().Changed("bypass-ip-range") {
		cfg.MCPBypassIPRanges = opts.BypassIPRanges
	}
	if cmd.Flags().Changed("bypass-verbose-log") {
		cfg.MCPBypassVerboseLog = opts.BypassVerboseLog
	}
	if cmd.Flags().Changed("bypass-audit-log") {
		cfg.MCPBypassAuditLog = opts.BypassAuditLog
	}
	if cmd.Flags().Changed("trusted-proxies") {
		cfg.MCPTrustedProxies = opts.TrustedProxies
	}

	// Validate OpenSearch configuration (required for MCP server unless --only-slack is used)
	if !opts.OnlySlack && cfg.OpenSearchEndpoint == "" {
		return fmt.Errorf("OpenSearch is required for MCP server: set OPENSEARCH_ENDPOINT and related settings (use --only-slack to skip)")
	}

	// In --only-slack mode, force enable Slack search
	if opts.OnlySlack {
		cfg.SlackSearchEnabled = true
		logger.Printf("Running in Slack-only mode (OpenSearch disabled)")
	}

	shutdown, obsErr := observability.Init(cfg)
	if obsErr != nil {
		logger.Printf("observability initialization error: %v", obsErr)
	}
	if err := metrics.InitOTelMetrics(); err != nil {
		logger.Printf("metrics OTel initialization error: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdown(shutdownCtx); err != nil {
			logger.Printf("observability shutdown error: %v", err)
		}
	}()

	// Create SDK-based server wrapper using RAGent configuration
	// The ServerWrapper will handle configuration conversion internally
	server, err := NewServerWrapper(cfg)
	if err != nil {
		return fmt.Errorf("failed to create server wrapper: %w", err)
	}
	server.SetLogger(logger)

	// Configure authentication
	// If auth-method is provided, prefer unified auth routing (supports ip/oidc/both/either)
	method := opts.AuthMethod
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
		ipAuthAdapter, err := NewIPAuthMiddlewareAdapter(cfg.MCPAllowedIPs, cfg.MCPIPAuthEnableLogging)
		if err != nil {
			return fmt.Errorf("failed to create IP authentication middleware: %w", err)
		}
		server.SetIPAuthMiddleware(ipAuthAdapter.GetIPAuthMiddleware())
		logger.Printf("IP authentication enabled for IPs: %v", cfg.MCPAllowedIPs)
	} else if method != "ip" { // oidc/both/either
		// Resolve OIDC values from env if not provided
		oidcClientID := opts.OIDCClientID
		oidcClientSecret := opts.OIDCClientSecret
		oidcIssuer := opts.OIDCIssuer
		oidcAuthURL := opts.OIDCAuthURL
		oidcTokenURL := opts.OIDCTokenURL
		oidcUserInfoURL := opts.OIDCUserInfoURL
		oidcJWKSURL := opts.OIDCJWKSURL
		oidcSkipDiscovery := opts.OIDCSkipDiscovery
		oidcScopes := opts.OIDCScopes

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
		var authMethod AuthMethod
		switch method {
		case "oidc":
			authMethod = AuthMethodOIDC
		case "both":
			authMethod = AuthMethodBoth
		case "either":
			authMethod = AuthMethodEither
		}

		unifiedCfg := &UnifiedAuthConfig{
			AuthMethod:    authMethod,
			EnableLogging: opts.AuthEnableLogging,
		}

		// Add bypass configuration if provided
		if len(cfg.MCPBypassIPRanges) > 0 {
			// Validate all CIDR formats before proceeding
			for _, cidr := range cfg.MCPBypassIPRanges {
				if _, _, err := net.ParseCIDR(cidr); err != nil {
					// Try parsing as single IP (will be converted to CIDR internally)
					if ip := net.ParseIP(cidr); ip == nil {
						logger.Printf("Warning: Invalid CIDR format for bypass IP range: %s", cidr)
					}
				}
			}

			unifiedCfg.BypassConfig = &BypassIPConfig{
				BypassIPRanges: cfg.MCPBypassIPRanges,
				VerboseLogging: cfg.MCPBypassVerboseLog,
				AuditLogging:   cfg.MCPBypassAuditLog,
				TrustedProxies: cfg.MCPTrustedProxies,
			}
			logger.Printf("Bypass IP authentication configured with %d ranges", len(cfg.MCPBypassIPRanges))
		}

		// IP part (for both/either)
		if authMethod == AuthMethodBoth || authMethod == AuthMethodEither {
			unifiedCfg.IPConfig = &IPAuthConfig{
				AllowedIPs:    cfg.MCPAllowedIPs,
				EnableLogging: cfg.MCPIPAuthEnableLogging,
			}
		}

		// OIDC part
		unifiedCfg.OIDCConfig = &OIDCConfig{
			Issuer:       oidcIssuer,
			ClientID:     oidcClientID,
			ClientSecret: oidcClientSecret,
			Scopes:       oidcScopes,
			// OAuth2 callback will use MCP_SERVER_PORT
			CallbackPort:     cfg.MCPServerPort,
			EnableLogging:    opts.AuthEnableLogging,
			AuthorizationURL: oidcAuthURL,
			TokenURL:         oidcTokenURL,
			UserInfoURL:      oidcUserInfoURL,
			JWKSURL:          oidcJWKSURL,
			SkipDiscovery:    oidcSkipDiscovery,
		}

		unified, err := NewUnifiedAuthMiddleware(unifiedCfg)
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

	bgCtx := context.Background()

	// Load AWS configuration
	awsConfig, err := bedrock.BuildBedrockAWSConfig(bgCtx, cfg.BedrockRegion, cfg.BedrockBearerToken)
	if err != nil {
		return fmt.Errorf("failed to load Bedrock AWS configuration: %w", err)
	}

	// Initialize Bedrock client for Slack search (needed for LLM in Slack search)
	slackBedrockClient := bedrock.NewBedrockClient(awsConfig, cfg.ChatModel)

	// Initialize Slack search service (required in --only-slack mode, optional otherwise)
	var slackService *slacksearch.SlackSearchService
	if cfg.SlackSearchEnabled || opts.OnlySlack {
		slackCfg, slackErr := appcfg.LoadSlack()
		if slackErr != nil {
			if opts.OnlySlack {
				return fmt.Errorf("slack configuration required in --only-slack mode: %w", slackErr)
			}
			logger.Printf("Slack configuration not available: %v", slackErr)
		} else if strings.TrimSpace(slackCfg.BotToken) == "" {
			if opts.OnlySlack {
				return fmt.Errorf("SLACK_BOT_TOKEN is required in --only-slack mode")
			}
			logger.Printf("Slack search disabled: SLACK_BOT_TOKEN is not configured")
		} else if strings.TrimSpace(cfg.SlackUserToken) == "" {
			if opts.OnlySlack {
				return fmt.Errorf("SLACK_USER_TOKEN is required in --only-slack mode")
			}
			logger.Printf("Slack search disabled: SLACK_USER_TOKEN is not configured")
		} else {
			slackClient := slack.New(slackCfg.BotToken)
			service, serr := slacksearch.NewSlackSearchService(cfg, slackClient, slackBedrockClient, logger)
			if serr != nil {
				if opts.OnlySlack {
					return fmt.Errorf("slack search initialization failed in --only-slack mode: %w", serr)
				}
				logger.Printf("Slack search initialization failed: %v", serr)
			} else if err := service.Initialize(bgCtx); err != nil {
				if opts.OnlySlack {
					return fmt.Errorf("slack search dependencies not ready in --only-slack mode: %w", err)
				}
				logger.Printf("Slack search dependencies not ready: %v", err)
			} else {
				slackService = service
				logger.Printf("Slack search support enabled for MCP server")
			}
		}
	}

	var registeredTools []string

	// In --only-slack mode, register only slack_search tool
	if opts.OnlySlack {
		// Create Slack search tool configuration
		slackSearchConfig := &SlackSearchConfig{
			DefaultMaxResults:     cfg.MCPDefaultSearchSize,
			DefaultTimeoutSeconds: cfg.MCPDefaultTimeoutSeconds,
		}

		// Create Slack search handler
		slackSearchHandler := NewSlackSearchHandler(slackService, slackSearchConfig)
		slackToolHandlerFunc := slackSearchHandler.HandleSDKToolCall

		// Determine tool name
		slackToolName := "slack_search"
		if cfg.MCPToolPrefix != "" {
			slackToolName = cfg.MCPToolPrefix + slackToolName
		}

		// Build enriched tool definition
		baseSlackDef := slackSearchHandler.GetSDKToolDefinition()
		detailedSlackTool := BuildSlackSearchToolDefinition(baseSlackDef, slackToolName, slackSearchConfig)

		// Register Slack search tool
		if err := server.RegisterCustomTool(detailedSlackTool, slackToolHandlerFunc); err != nil {
			return fmt.Errorf("failed to register slack_search tool: %w", err)
		}

		documentedParams := 0
		if detailedSlackTool != nil && detailedSlackTool.InputSchema != nil && detailedSlackTool.InputSchema.Properties != nil {
			documentedParams = len(detailedSlackTool.InputSchema.Properties)
		}

		logger.Printf(
			"Registered tool '%s' with SDK server (documented parameters=%d)",
			slackToolName,
			documentedParams,
		)
		registeredTools = append(registeredTools, slackToolName)
	} else {
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
		if err := osClient.HealthCheck(bgCtx); err != nil {
			return fmt.Errorf("OpenSearch health check failed: %w", err)
		}
		logger.Printf("OpenSearch connection established: %s", cfg.OpenSearchEndpoint)

		// Initialize Bedrock embedding client
		embeddingClient := bedrock.NewBedrockClient(awsConfig, "amazon.titan-embed-text-v2:0")

		// Create hybrid search tool configuration
		hybridSearchConfig := &HybridSearchConfig{
			DefaultIndexName:      cfg.OpenSearchIndex,
			DefaultSize:           cfg.MCPDefaultSearchSize,
			DefaultBM25Weight:     cfg.MCPDefaultBM25Weight,
			DefaultVectorWeight:   cfg.MCPDefaultVectorWeight,
			DefaultFusionMethod:   "weighted_sum",
			DefaultUseJapaneseNLP: cfg.MCPDefaultUseJapaneseNLP,
			DefaultTimeoutSeconds: cfg.MCPDefaultTimeoutSeconds,
		}

		// Create hybrid search tool handler for SDK integration
		hybridSearchHandler := NewHybridSearchHandler(osClient, embeddingClient, hybridSearchConfig, slackService)

		// Create function wrapper to match mcp.ToolHandler signature
		toolHandlerFunc := hybridSearchHandler.HandleSDKToolCall

		// Determine tool name
		toolName := cfg.MCPHybridSearchToolName
		if cfg.MCPToolPrefix != "" {
			toolName = cfg.MCPToolPrefix + toolName
		}

		// Build enriched tool definition for MCP clients
		baseDefinition := hybridSearchHandler.GetSDKToolDefinition()
		detailedTool := BuildHybridSearchToolDefinition(baseDefinition, toolName, hybridSearchConfig)

		// Register tool with SDK server through ServerWrapper using the enriched definition
		if err := server.RegisterCustomTool(detailedTool, toolHandlerFunc); err != nil {
			return fmt.Errorf("failed to register hybrid search tool: %w", err)
		}

		documentedParams := 0
		if detailedTool != nil && detailedTool.InputSchema != nil && detailedTool.InputSchema.Properties != nil {
			documentedParams = len(detailedTool.InputSchema.Properties)
		}

		logger.Printf(
			"Registered tool '%s' with SDK server (documented parameters=%d)",
			toolName,
			documentedParams,
		)
		registeredTools = append(registeredTools, toolName)
	}

	// Setup graceful shutdown
	runCtx, cancel := context.WithCancel(context.Background())
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
	logger.Printf("Available tools: %s", strings.Join(registeredTools, ", "))

	if err := server.Start(); err != nil {
		return fmt.Errorf("failed to start MCP server: %w", err)
	}

	// Wait for shutdown signal
	<-runCtx.Done()

	logger.Printf("MCP server (SDK-based) stopped successfully")
	return nil
}

// BuildHybridSearchToolDefinition builds enriched tool definition for MCP clients.
// Renamed from buildHybridSearchToolDefinition (now exported).
func BuildHybridSearchToolDefinition(base *mcp.Tool, toolName string, defaults *HybridSearchConfig) *mcp.Tool {
	if defaults == nil {
		defaults = &HybridSearchConfig{
			DefaultIndexName:      "ragent-docs",
			DefaultSize:           10,
			DefaultBM25Weight:     0.5,
			DefaultVectorWeight:   0.5,
			DefaultFusionMethod:   "weighted_sum",
			DefaultUseJapaneseNLP: true,
			DefaultTimeoutSeconds: 30,
		}
	}

	var toolCopy mcp.Tool
	if base != nil {
		toolCopy = *base
	}
	toolCopy.Name = toolName

	toolCopy.Description = fmt.Sprintf(
		"ハイブリッド検索ツール。RAGent の OpenSearch (BM25) と Titan ベクトル検索を組み合わせ、最大 %d 件の候補を融合スコアで返します。日本語・英語いずれの自然文クエリにも対応し、手順書・設計資料・ナレッジノートを横断的に調べる用途を想定しています。必要に応じて `enable_slack_search` を true にすることで社内 Slack の会話も同時に検索できます。レスポンスは JSON テキストで、各ドキュメントのタイトル/抜粋/スコア/パス/メタデータ (任意) を含みます。\n\nEnglish: Run hybrid retrieval across the Markdown knowledge base by blending BM25 and Titan embeddings on Amazon OpenSearch. Returns up to %d ranked documents with fused scores plus optional metadata. Set `enable_slack_search` to true to enrich the response with Slack conversations.",
		defaults.DefaultSize,
		defaults.DefaultSize,
	)

	var schema *jsonschema.Schema
	if base != nil && base.InputSchema != nil {
		schema = base.InputSchema.CloneSchemas()
	} else {
		schema = &jsonschema.Schema{}
	}

	schema.Type = "object"
	schema.Title = "Hybrid Search Parameters / ハイブリッド検索パラメータ"
	schema.Description = "ハイブリッド検索ツールに渡すことができるパラメータ一覧です。最低限 `query` を指定し、必要に応じて件数・フィルタ・重み付けを調整してください。"
	schema.Required = []string{"query"}

	if schema.Properties == nil {
		schema.Properties = make(map[string]*jsonschema.Schema)
	}

	ensureProperty := func(key, typeName string) *jsonschema.Schema {
		prop, ok := schema.Properties[key]
		if ok && prop != nil {
			prop = prop.CloneSchemas()
		} else {
			prop = &jsonschema.Schema{}
		}
		if typeName != "" {
			prop.Type = typeName
			prop.Types = nil
		}
		schema.Properties[key] = prop
		return prop
	}

	toRaw := func(v any) json.RawMessage {
		data, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		return data
	}

	queryProp := ensureProperty("query", "string")
	queryProp.Title = "Query / クエリ"
	queryProp.Description = "検索対象の質問やキーワードを自然文で入力します。短いキーワードよりも、欲しい情報や前提条件を含めた文章の方が精度が上がります。"
	minQueryLen := 1
	queryProp.MinLength = &minQueryLen
	queryProp.Examples = []any{
		"S3 Vector インデックスをローテーションする手順",
		"What does the runbook recommend for recovering failing hybrid search nodes?",
	}

	topKProp := ensureProperty("top_k", "integer")
	topKProp.Title = "Top K Results"
	topKProp.Description = fmt.Sprintf("返却件数を指定します。1〜100 の範囲で設定でき、デフォルトは %d 件です。", defaults.DefaultSize)
	minTopK := float64(1)
	maxTopK := float64(100)
	topKProp.Minimum = &minTopK
	topKProp.Maximum = &maxTopK
	topKProp.Default = toRaw(defaults.DefaultSize)
	topKProp.Examples = []any{5, defaults.DefaultSize, 20}

	filtersProp := ensureProperty("filters", "object")
	filtersProp.Title = "Metadata Filters"
	filtersProp.Description = "ドキュメントのメタデータに対する完全一致フィルタです。キーには `category`、`tags`、`path` などのフィールド名を指定し、値には期待する文字列を設定します。ログイン不要な公開ノートに絞りたい場合などに利用します。"
	filtersProp.AdditionalProperties = &jsonschema.Schema{
		Type:        "string",
		Description: "フィルタする値。完全一致で比較されます。ワイルドカードは未サポートです。",
	}
	filtersProp.Examples = []any{
		map[string]any{"category": "Runbook"},
		map[string]any{"scope": "Production", "tags": "oncall"},
	}

	searchModeProp := ensureProperty("search_mode", "string")
	searchModeProp.Title = "Search Mode"
	searchModeProp.Description = "実行する検索モードを選択します。`hybrid` は BM25 とベクトル検索の融合、`bm25` はキーワード優先、`vector` は意味検索優先です。"
	searchModeProp.Enum = []any{"hybrid", "bm25", "vector"}
	searchModeProp.Default = toRaw("hybrid")
	searchModeProp.Examples = []any{"hybrid", "bm25"}

	bm25WeightProp := ensureProperty("bm25_weight", "number")
	bm25WeightProp.Title = "BM25 Weight"
	bm25WeightProp.Description = "BM25 (キーワード一致) スコアの比重を 0〜1 の範囲で調整します。値を高くするとキーワード一致を優先します。"
	minWeight := float64(0)
	maxWeight := float64(1)
	bm25WeightProp.Minimum = &minWeight
	bm25WeightProp.Maximum = &maxWeight
	bm25WeightProp.Default = toRaw(defaults.DefaultBM25Weight)
	bm25WeightProp.Examples = []any{0.3, defaults.DefaultBM25Weight, 0.7}

	vectorWeightProp := ensureProperty("vector_weight", "number")
	vectorWeightProp.Title = "Vector Weight"
	vectorWeightProp.Description = "ベクトル (意味) スコアの比重を 0〜1 の範囲で調整します。BM25 weight との合計が 1 に近いバランスになるようにしてください。"
	vectorWeightProp.Minimum = &minWeight
	vectorWeightProp.Maximum = &maxWeight
	vectorWeightProp.Default = toRaw(defaults.DefaultVectorWeight)
	vectorWeightProp.Examples = []any{0.5, defaults.DefaultVectorWeight, 0.8}

	minScoreProp := ensureProperty("min_score", "number")
	minScoreProp.Title = "Minimum Score"
	minScoreProp.Description = "この値よりスコアが低い結果を除外します。ノイズの多いクエリで精度を絞り込みたいときに利用します。"
	minScoreProp.Minimum = &minWeight
	minScoreProp.Default = toRaw(0.0)
	minScoreProp.Examples = []any{0.0, 0.35}

	includeMetadataProp := ensureProperty("include_metadata", "boolean")
	includeMetadataProp.Title = "Include Metadata"
	includeMetadataProp.Description = "`true` にするとレスポンスに `metadata` フィールドが追加され、各ドキュメントの生メタデータを確認できます。LLM へのフォローアップ生成時に便利です。"
	includeMetadataProp.Default = toRaw(false)
	includeMetadataProp.Examples = []any{true}

	fusionMethodProp := ensureProperty("fusion_method", "string")
	fusionMethodProp.Title = "Fusion Method"
	fusionMethodProp.Description = "BM25 とベクトル結果の統合方法です。現在は `weighted_sum` のみが実装されており、その他の値を指定した場合も加重和で処理されます。"
	fusionMethodProp.Enum = []any{"weighted_sum", "rrf"}
	fusionMethodProp.Default = toRaw(defaults.DefaultFusionMethod)

	nlpProp := ensureProperty("use_japanese_nlp", "boolean")
	nlpProp.Title = "Use Japanese NLP"
	nlpProp.Description = "日本語の形態素解析を有効にするかどうか。現在はサーバー設定に従って動作し、明示的に変更するオプションはプレビュー扱いです。"
	nlpProp.Default = toRaw(defaults.DefaultUseJapaneseNLP)

	slackToggleProp := ensureProperty("enable_slack_search", "boolean")
	slackToggleProp.Title = "Enable Slack Search"
	slackToggleProp.Description = "Slack のワークスペース会話を同時に検索する場合は true を指定します。サーバー側で Slack の資格情報が設定されている必要があります。"
	slackToggleProp.Default = toRaw(false)

	schema.Properties["query"] = queryProp
	schema.Properties["top_k"] = topKProp
	schema.Properties["filters"] = filtersProp
	schema.Properties["search_mode"] = searchModeProp
	schema.Properties["bm25_weight"] = bm25WeightProp
	schema.Properties["vector_weight"] = vectorWeightProp
	schema.Properties["min_score"] = minScoreProp
	schema.Properties["include_metadata"] = includeMetadataProp
	schema.Properties["fusion_method"] = fusionMethodProp
	schema.Properties["use_japanese_nlp"] = nlpProp
	schema.Properties["enable_slack_search"] = slackToggleProp

	schema.Examples = []any{
		map[string]any{
			"query":            "障害復旧のフローをまとめた runbook を探したい",
			"top_k":            8,
			"filters":          map[string]any{"category": "Runbook"},
			"bm25_weight":      defaults.DefaultBM25Weight,
			"vector_weight":    defaults.DefaultVectorWeight,
			"include_metadata": true,
		},
		map[string]any{
			"query":               "Explain the monitoring setup for the embedding pipeline",
			"search_mode":         "vector",
			"min_score":           0.25,
			"filters":             map[string]any{"tags": "observability"},
			"enable_slack_search": true,
		},
	}

	toolCopy.InputSchema = schema
	return &toolCopy
}
