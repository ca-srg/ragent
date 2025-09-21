package types

import (
	"fmt"
	"time"
)

// DocumentMetadata represents metadata extracted from markdown files
type DocumentMetadata struct {
	Title        string                 `json:"title"`
	Category     string                 `json:"category"`
	Tags         []string               `json:"tags"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
	Author       string                 `json:"author"`
	Reference    string                 `json:"reference"`
	Source       string                 `json:"source"`
	FilePath     string                 `json:"file_path"`
	WordCount    int                    `json:"word_count"`
	CustomFields map[string]interface{} `json:"custom_fields"`
}

// FileInfo represents information about a markdown file to be processed
type FileInfo struct {
	Path       string           `json:"path"`
	Name       string           `json:"name"`
	Size       int64            `json:"size"`
	ModTime    time.Time        `json:"mod_time"`
	IsMarkdown bool             `json:"is_markdown"`
	Content    string           `json:"content"`
	Metadata   DocumentMetadata `json:"metadata"`
}

// VectorData represents the embedding vector data for a document
type VectorData struct {
	ID        string           `json:"id"`
	Embedding []float64        `json:"embedding"`
	Metadata  DocumentMetadata `json:"metadata"`
	Content   string           `json:"content"`
	CreatedAt time.Time        `json:"created_at"`
}

// ProcessingResult represents the result of processing markdown files
type ProcessingResult struct {
	ProcessedFiles int               `json:"processed_files"`
	SuccessCount   int               `json:"success_count"`
	FailureCount   int               `json:"failure_count"`
	Errors         []ProcessingError `json:"errors"`
	StartTime      time.Time         `json:"start_time"`
	EndTime        time.Time         `json:"end_time"`
	Duration       time.Duration     `json:"duration"`
	// OpenSearch specific statistics
	OpenSearchEnabled        bool          `json:"opensearch_enabled"`
	OpenSearchSuccessCount   int           `json:"opensearch_success_count"`
	OpenSearchFailureCount   int           `json:"opensearch_failure_count"`
	OpenSearchIndexedCount   int           `json:"opensearch_indexed_count"`
	OpenSearchSkippedCount   int           `json:"opensearch_skipped_count"`
	OpenSearchRetryCount     int           `json:"opensearch_retry_count"`
	OpenSearchProcessingTime time.Duration `json:"opensearch_processing_time"`
}

// ProcessingError represents an error that occurred during processing
type ProcessingError struct {
	Type       ErrorType `json:"type"`
	Message    string    `json:"message"`
	FilePath   string    `json:"file_path"`
	Timestamp  time.Time `json:"timestamp"`
	Retryable  bool      `json:"retryable"`
	RetryCount int       `json:"retry_count"`
}

// Error implements the error interface for ProcessingError
func (pe *ProcessingError) Error() string {
	return fmt.Sprintf("[%s] %s (file: %s)", pe.Type, pe.Message, pe.FilePath)
}

// IsRetryable returns whether this error type should be retried
func (pe *ProcessingError) IsRetryable() bool {
	return pe.Retryable && pe.RetryCount < 3 // Maximum 3 retries
}

// IncrementRetry increments the retry count
func (pe *ProcessingError) IncrementRetry() {
	pe.RetryCount++
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
	// OpenSearch specific error types
	ErrorTypeOpenSearchConnection ErrorType = "opensearch_connection"
	ErrorTypeOpenSearchMapping    ErrorType = "opensearch_mapping"
	ErrorTypeOpenSearchIndexing   ErrorType = "opensearch_indexing"
	ErrorTypeOpenSearchBulkIndex  ErrorType = "opensearch_bulk_index"
	ErrorTypeOpenSearchQuery      ErrorType = "opensearch_query"
	ErrorTypeOpenSearchIndex      ErrorType = "opensearch_index"
)

// Config represents the vectorizer configuration
type Config struct {
	// AWS S3 Vectors configuration
	AWSS3VectorBucket    string        `json:"aws_s3_vector_bucket" env:"AWS_S3_VECTOR_BUCKET,required=true"`
	AWSS3VectorIndex     string        `json:"aws_s3_vector_index" env:"AWS_S3_VECTOR_INDEX,required=true"`
	AWSS3Region          string        `json:"aws_s3_region" env:"AWS_S3_REGION,default=us-east-1"`
	ChatModel            string        `json:"chat_model" env:"CHAT_MODEL,default=anthropic.claude-3-5-sonnet-20240620-v1:0"`
	Concurrency          int           `json:"concurrency" env:"VECTORIZER_CONCURRENCY,default=10"`
	RetryAttempts        int           `json:"retry_attempts" env:"VECTORIZER_RETRY_ATTEMPTS,default=0"`
	RetryDelay           time.Duration `json:"retry_delay" env:"VECTORIZER_RETRY_DELAY,default=2s"`
	ExcludeCategoriesStr string        `json:"-" env:"EXCLUDE_CATEGORIES,default=個人メモ,日報"`
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
}

// QueryResult represents a single result from a vector query
type QueryResult struct {
	Key      string                 `json:"key"`
	Distance float64                `json:"distance"`
	Metadata map[string]interface{} `json:"metadata"`
	Content  string                 `json:"content,omitempty"`
}

// QueryVectorsResult represents the complete result from a vector query
type QueryVectorsResult struct {
	Results    []QueryResult `json:"results"`
	TotalCount int           `json:"total_count"`
	Query      string        `json:"query"`
	TopK       int           `json:"top_k"`
}
