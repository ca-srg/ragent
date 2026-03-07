package config

import (
	"time"
)

// Config represents the vectorizer configuration
type Config struct {
	// AWS S3 Vectors configuration
	AWSS3VectorBucket    string        `json:"aws_s3_vector_bucket" env:"AWS_S3_VECTOR_BUCKET"`
	AWSS3VectorIndex     string        `json:"aws_s3_vector_index" env:"AWS_S3_VECTOR_INDEX"`
	S3VectorRegion       string        `json:"s3_vector_region" env:"S3_VECTOR_REGION,default=us-east-1"`
	VectorDBBackend      string        `json:"vector_db_backend" env:"VECTOR_DB_BACKEND,default=s3"`
	SqliteVecDBPath      string        `json:"sqlite_vec_db_path" env:"SQLITE_VEC_DB_PATH,default=~/.ragent/vectors.db"`
	S3SourceRegion       string        `json:"s3_source_region" env:"S3_SOURCE_REGION,default=us-east-1"`
	ChatModel            string        `json:"chat_model" env:"CHAT_MODEL,default=global.anthropic.claude-sonnet-4-6"`
	Concurrency          int           `json:"concurrency" env:"VECTORIZER_CONCURRENCY,default=10"`
	RetryAttempts        int           `json:"retry_attempts" env:"VECTORIZER_RETRY_ATTEMPTS,default=10"`
	RetryDelay           time.Duration `json:"retry_delay" env:"VECTORIZER_RETRY_DELAY,default=10s"`
	ExcludeCategoriesStr string        `json:"-" env:"EXCLUDE_CATEGORIES,default=日報"`
	ExcludeCategories    []string      `json:"exclude_categories"`
	// OpenSearch configuration
	OpenSearchEndpoint          string        `json:"opensearch_endpoint" env:"OPENSEARCH_ENDPOINT,required=true"`
	OpenSearchIndex             string        `json:"opensearch_index" env:"OPENSEARCH_INDEX,required=true"`
	OpenSearchRegion            string        `json:"opensearch_region" env:"OPENSEARCH_REGION,default=us-east-1"`
	OpenSearchInsecureSkipTLS   bool          `json:"opensearch_insecure_skip_tls" env:"OPENSEARCH_INSECURE_SKIP_TLS,default=false"`
	OpenSearchRateLimit         float64       `json:"opensearch_rate_limit" env:"OPENSEARCH_RATE_LIMIT,default=10.0"`
	OpenSearchRateBurst         int           `json:"opensearch_rate_burst" env:"OPENSEARCH_RATE_BURST,default=20"`
	OpenSearchConnectionTimeout time.Duration `json:"opensearch_connection_timeout" env:"OPENSEARCH_CONNECTION_TIMEOUT,default=30s"`
	OpenSearchRequestTimeout    time.Duration `json:"opensearch_request_timeout" env:"OPENSEARCH_REQUEST_TIMEOUT,default=60s"`
	OpenSearchMaxRetries        int           `json:"opensearch_max_retries" env:"OPENSEARCH_MAX_RETRIES,default=3"`
	OpenSearchRetryDelay        time.Duration `json:"opensearch_retry_delay" env:"OPENSEARCH_RETRY_DELAY,default=1s"`
	OpenSearchMaxConnections    int           `json:"opensearch_max_connections" env:"OPENSEARCH_MAX_CONNECTIONS,default=100"`
	OpenSearchMaxIdleConns      int           `json:"opensearch_max_idle_conns" env:"OPENSEARCH_MAX_IDLE_CONNS,default=10"`
	OpenSearchIdleConnTimeout   time.Duration `json:"opensearch_idle_conn_timeout" env:"OPENSEARCH_IDLE_CONN_TIMEOUT,default=90s"`

	// MCP Server configuration
	MCPServerEnabled          bool          `json:"mcp_server_enabled" env:"MCP_SERVER_ENABLED,default=false"`
	MCPServerHost             string        `json:"mcp_server_host" env:"MCP_SERVER_HOST,default=localhost"`
	MCPServerPort             int           `json:"mcp_server_port" env:"MCP_SERVER_PORT,default=8080"`
	MCPServerReadTimeout      time.Duration `json:"mcp_server_read_timeout" env:"MCP_SERVER_READ_TIMEOUT,default=30s"`
	MCPServerWriteTimeout     time.Duration `json:"mcp_server_write_timeout" env:"MCP_SERVER_WRITE_TIMEOUT,default=30s"`
	MCPServerIdleTimeout      time.Duration `json:"mcp_server_idle_timeout" env:"MCP_SERVER_IDLE_TIMEOUT,default=120s"`
	MCPServerMaxHeaderBytes   int           `json:"mcp_server_max_header_bytes" env:"MCP_SERVER_MAX_HEADER_BYTES,default=1048576"` // 1MB
	MCPServerGracefulShutdown bool          `json:"mcp_server_graceful_shutdown" env:"MCP_SERVER_GRACEFUL_SHUTDOWN,default=true"`
	MCPServerShutdownTimeout  time.Duration `json:"mcp_server_shutdown_timeout" env:"MCP_SERVER_SHUTDOWN_TIMEOUT,default=30s"`

	// MCP IP Authentication configuration
	MCPIPAuthEnabled       bool     `json:"mcp_ip_auth_enabled" env:"MCP_IP_AUTH_ENABLED,default=true"`
	MCPAllowedIPsStr       string   `json:"-" env:"MCP_ALLOWED_IPS,default=127.0.0.1,::1"`
	MCPAllowedIPs          []string `json:"mcp_allowed_ips"`
	MCPIPAuthEnableLogging bool     `json:"mcp_ip_auth_enable_logging" env:"MCP_IP_AUTH_ENABLE_LOGGING,default=true"`

	// MCP IP Bypass configuration
	MCPBypassIPRangesStr string   `json:"-" env:"MCP_BYPASS_IP_RANGE"`
	MCPBypassIPRanges    []string `json:"mcp_bypass_ip_ranges"`
	MCPBypassVerboseLog  bool     `json:"mcp_bypass_verbose_log" env:"MCP_BYPASS_VERBOSE_LOG,default=false"`
	MCPBypassAuditLog    bool     `json:"mcp_bypass_audit_log" env:"MCP_BYPASS_AUDIT_LOG,default=true"`
	MCPTrustedProxiesStr string   `json:"-" env:"MCP_TRUSTED_PROXIES"`
	MCPTrustedProxies    []string `json:"mcp_trusted_proxies"`

	// MCP Tool configuration
	MCPToolPrefix            string  `json:"mcp_tool_prefix" env:"MCP_TOOL_PREFIX,default="`
	MCPHybridSearchToolName  string  `json:"mcp_hybrid_search_tool_name" env:"MCP_TOOL_NAME_HYBRID_SEARCH,default=hybrid_search"`
	MCPDefaultSearchSize     int     `json:"mcp_default_search_size" env:"MCP_DEFAULT_SEARCH_SIZE,default=10"`
	MCPDefaultBM25Weight     float64 `json:"mcp_default_bm25_weight" env:"MCP_DEFAULT_BM25_WEIGHT,default=0.5"`
	MCPDefaultVectorWeight   float64 `json:"mcp_default_vector_weight" env:"MCP_DEFAULT_VECTOR_WEIGHT,default=0.5"`
	MCPDefaultUseJapaneseNLP bool    `json:"mcp_default_use_japanese_nlp" env:"MCP_DEFAULT_USE_JAPANESE_NLP,default=true"`
	MCPDefaultTimeoutSeconds int     `json:"mcp_default_timeout_seconds" env:"MCP_DEFAULT_TIMEOUT_SECONDS,default=30"`

	// MCP SSE (Server-Sent Events) configuration
	MCPSSEEnabled           bool          `json:"mcp_sse_enabled" env:"MCP_SSE_ENABLED,default=true"`
	MCPSSEHeartbeatInterval time.Duration `json:"mcp_sse_heartbeat_interval" env:"MCP_SSE_HEARTBEAT_INTERVAL,default=30s"`
	MCPSSEBufferSize        int           `json:"mcp_sse_buffer_size" env:"MCP_SSE_BUFFER_SIZE,default=100"`
	MCPSSEMaxClients        int           `json:"mcp_sse_max_clients" env:"MCP_SSE_MAX_CLIENTS,default=1000"`
	MCPSSEHistorySize       int           `json:"mcp_sse_history_size" env:"MCP_SSE_HISTORY_SIZE,default=50"`

	// Slack Search configuration
	SlackSearchEnabled              bool   `json:"slack_search_enabled" env:"SLACK_SEARCH_ENABLED,default=false"`
	SlackUserToken                  string `json:"slack_user_token" env:"SLACK_USER_TOKEN"`
	SlackSearchMaxResults           int    `json:"slack_search_max_results" env:"SLACK_SEARCH_MAX_RESULTS,default=20"`
	SlackSearchMaxRetries           int    `json:"slack_search_max_retries" env:"SLACK_SEARCH_MAX_RETRIES,default=3"`
	SlackSearchContextWindowMinutes int    `json:"slack_search_context_window_minutes" env:"SLACK_SEARCH_CONTEXT_WINDOW_MINUTES,default=30"`
	SlackSearchMaxIterations        int    `json:"slack_search_max_iterations" env:"SLACK_SEARCH_MAX_ITERATIONS,default=3"`
	SlackSearchMaxContextMessages   int    `json:"slack_search_max_context_messages" env:"SLACK_SEARCH_MAX_CONTEXT_MESSAGES,default=100"`
	SlackSearchTimeoutSeconds       int    `json:"slack_search_timeout_seconds" env:"SLACK_SEARCH_TIMEOUT_SECONDS,default=60"`
	SlackSearchLLMTimeoutSeconds    int    `json:"slack_search_llm_timeout_seconds" env:"SLACK_SEARCH_LLM_TIMEOUT_SECONDS,default=60"`

	// Observability (OpenTelemetry) configuration
	OTelEnabled              bool    `json:"otel_enabled" env:"OTEL_ENABLED,default=false"`
	OTelServiceName          string  `json:"otel_service_name" env:"OTEL_SERVICE_NAME,default=ragent"`
	OTelExporterOTLPEndpoint string  `json:"otel_exporter_otlp_endpoint" env:"OTEL_EXPORTER_OTLP_ENDPOINT"`
	OTelExporterOTLPProtocol string  `json:"otel_exporter_otlp_protocol" env:"OTEL_EXPORTER_OTLP_PROTOCOL,default=http/protobuf"`
	OTelResourceAttributes   string  `json:"otel_resource_attributes" env:"OTEL_RESOURCE_ATTRIBUTES"`
	OTelTracesSampler        string  `json:"otel_traces_sampler" env:"OTEL_TRACES_SAMPLER,default=always_on"`
	OTelTracesSamplerArg     float64 `json:"otel_traces_sampler_arg" env:"OTEL_TRACES_SAMPLER_ARG,default=1.0"`

	// GitHub configuration
	GitHubToken string `json:"github_token" env:"GITHUB_TOKEN"`

	// OCR configuration
	OCRProvider  string        `json:"ocr_provider" env:"OCR_PROVIDER"`
	OCRModel     string        `json:"ocr_model" env:"OCR_MODEL,default=global.anthropic.claude-sonnet-4-6"`
	OCRTimeout   time.Duration `json:"ocr_timeout" env:"OCR_TIMEOUT,default=600s"`
	OCRMaxTokens int           `json:"ocr_max_tokens" env:"OCR_MAX_TOKENS,default=200000"`
	OCRConcurrency int           `json:"ocr_concurrency" env:"OCR_CONCURRENCY,default=5"`

	// Gemini API configuration (for OCR_PROVIDER=gemini)
	GeminiAPIKey string `json:"gemini_api_key" env:"GEMINI_API_KEY"`
}

// ErrorType represents the type of error that occurred
type ErrorType string

const (
	ErrorTypeFileRead       ErrorType = "file_read"
	ErrorTypeMetadata       ErrorType = "metadata_extraction"
	ErrorTypeEmbedding      ErrorType = "embedding_generation"
	ErrorTypeS3Upload       ErrorType = "s3_upload"
	ErrorTypeNetworkTimeout ErrorType = "network_timeout"
	ErrorTypeTimeout        ErrorType = "timeout"
	ErrorTypeRateLimit      ErrorType = "rate_limit"
	ErrorTypeValidation     ErrorType = "validation"
	ErrorTypeAuthentication ErrorType = "authentication"
	ErrorTypeUnknown        ErrorType = "unknown"
	ErrorTypeOCR            ErrorType = "ocr"
	// OpenSearch specific error types
	ErrorTypeOpenSearchConnection ErrorType = "opensearch_connection"
	ErrorTypeOpenSearchMapping    ErrorType = "opensearch_mapping"
	ErrorTypeOpenSearchIndexing   ErrorType = "opensearch_indexing"
	ErrorTypeOpenSearchBulkIndex  ErrorType = "opensearch_bulk_index"
	ErrorTypeOpenSearchQuery      ErrorType = "opensearch_query"
	ErrorTypeOpenSearchIndex      ErrorType = "opensearch_index"
)
