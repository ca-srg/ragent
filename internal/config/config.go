package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/ca-srg/ragent/internal/types"
	env "github.com/netflix/go-env"
)

// Type alias for Config
type Config = types.Config

// Load loads configuration from environment variables
func Load() (*Config, error) {
	var config Config

	_, err := env.UnmarshalFromEnviron(&config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse environment variables: %w", err)
	}

	// Parse ExcludeCategories from pipe-separated string
	if config.ExcludeCategoriesStr != "" {
		config.ExcludeCategories = strings.Split(config.ExcludeCategoriesStr, "|")
	}

	// Parse MCPAllowedIPs from comma-separated string
	if config.MCPAllowedIPsStr != "" {
		ips := strings.Split(config.MCPAllowedIPsStr, ",")
		config.MCPAllowedIPs = make([]string, 0, len(ips))
		for _, ip := range ips {
			if trimmed := strings.TrimSpace(ip); trimmed != "" {
				config.MCPAllowedIPs = append(config.MCPAllowedIPs, trimmed)
			}
		}
	}

	// Parse MCPBypassIPRanges from comma-separated string
	if config.MCPBypassIPRangesStr != "" {
		ranges := strings.Split(config.MCPBypassIPRangesStr, ",")
		config.MCPBypassIPRanges = make([]string, 0, len(ranges))
		for _, r := range ranges {
			if trimmed := strings.TrimSpace(r); trimmed != "" {
				config.MCPBypassIPRanges = append(config.MCPBypassIPRanges, trimmed)
			}
		}
	}

	// Parse MCPTrustedProxies from comma-separated string
	if config.MCPTrustedProxiesStr != "" {
		proxies := strings.Split(config.MCPTrustedProxiesStr, ",")
		config.MCPTrustedProxies = make([]string, 0, len(proxies))
		for _, p := range proxies {
			if trimmed := strings.TrimSpace(p); trimmed != "" {
				config.MCPTrustedProxies = append(config.MCPTrustedProxies, trimmed)
			}
		}
	}

	if err := validateConfig(&config); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return &config, nil
}

// validateConfig validates configuration values and adjusts them to safe ranges
func validateConfig(config *Config) error {
	// Validate concurrency limits
	if config.Concurrency < 1 {
		config.Concurrency = 1
	}
	if config.Concurrency > 20 {
		config.Concurrency = 20
	}

	// Validate retry attempts
	if config.RetryAttempts < 0 {
		config.RetryAttempts = 0
	}
	if config.RetryAttempts > 10 {
		config.RetryAttempts = 10
	}

	// Validate OpenSearch configuration if endpoint is provided
	if config.OpenSearchEndpoint != "" {
		if err := validateOpenSearchConfig(config); err != nil {
			return fmt.Errorf("OpenSearch configuration validation failed: %w", err)
		}
	}

	// Validate MCP server configuration if enabled
	if config.MCPServerEnabled {
		if err := validateMCPConfig(config); err != nil {
			return fmt.Errorf("MCP server configuration validation failed: %w", err)
		}

		// Validate SDK compatibility for MCP server configuration (Requirement 3.3)
		if err := validateSDKCompatibility(config); err != nil {
			return fmt.Errorf("MCP SDK compatibility validation failed: %w", err)
		}
	}

	// Validate Slack search configuration (always validate to catch early misconfiguration)
	if err := validateSlackSearchConfig(config); err != nil {
		return fmt.Errorf("slack search configuration validation failed: %w", err)
	}

	// Validate OpenTelemetry configuration if enabled
	if config.OTelEnabled {
		if err := validateOTelConfig(config); err != nil {
			return fmt.Errorf("OpenTelemetry configuration validation failed: %w", err)
		}
	}

	return nil
}

// validateSlackSearchConfig validates configuration for the Slack search feature.
func validateSlackSearchConfig(config *Config) error {
	if !config.SlackSearchEnabled {
		return nil
	}

	if strings.TrimSpace(config.SlackUserToken) == "" {
		return fmt.Errorf("SLACK_USER_TOKEN is required when Slack search is enabled")
	}

	if config.SlackSearchMaxResults <= 0 || config.SlackSearchMaxResults > 100 {
		return fmt.Errorf("SLACK_SEARCH_MAX_RESULTS must be between 1 and 100 (got %d)", config.SlackSearchMaxResults)
	}

	if config.SlackSearchMaxRetries < 0 || config.SlackSearchMaxRetries > 10 {
		return fmt.Errorf("SLACK_SEARCH_MAX_RETRIES must be between 0 and 10 (got %d)", config.SlackSearchMaxRetries)
	}

	if config.SlackSearchContextWindowMinutes <= 0 || config.SlackSearchContextWindowMinutes > 720 {
		return fmt.Errorf("SLACK_SEARCH_CONTEXT_WINDOW_MINUTES must be between 1 and 720 (got %d)", config.SlackSearchContextWindowMinutes)
	}

	if config.SlackSearchMaxIterations <= 0 || config.SlackSearchMaxIterations > 10 {
		return fmt.Errorf("SLACK_SEARCH_MAX_ITERATIONS must be between 1 and 10 (got %d)", config.SlackSearchMaxIterations)
	}

	if config.SlackSearchMaxContextMessages <= 0 || config.SlackSearchMaxContextMessages > 500 {
		return fmt.Errorf("SLACK_SEARCH_MAX_CONTEXT_MESSAGES must be between 1 and 500 (got %d)", config.SlackSearchMaxContextMessages)
	}

	if config.SlackSearchTimeoutSeconds <= 0 || config.SlackSearchTimeoutSeconds > 60 {
		return fmt.Errorf("SLACK_SEARCH_TIMEOUT_SECONDS must be between 1 and 60 (got %d)", config.SlackSearchTimeoutSeconds)
	}

	return nil
}

// validateOpenSearchConfig validates OpenSearch-specific configuration
func validateOpenSearchConfig(config *Config) error {
	// Validate OpenSearch endpoint URL format
	if config.OpenSearchEndpoint == "" {
		return fmt.Errorf("OPENSEARCH_ENDPOINT is required when OpenSearch is enabled")
	}

	// Parse and validate URL format
	parsedURL, err := url.Parse(config.OpenSearchEndpoint)
	if err != nil {
		return fmt.Errorf("invalid OPENSEARCH_ENDPOINT URL format: %w", err)
	}

	// Check for required URL components
	if parsedURL.Scheme == "" {
		return fmt.Errorf("OPENSEARCH_ENDPOINT must include scheme (http:// or https://)")
	}

	if !strings.HasPrefix(parsedURL.Scheme, "http") {
		return fmt.Errorf("OPENSEARCH_ENDPOINT scheme must be http or https")
	}

	if parsedURL.Host == "" {
		return fmt.Errorf("OPENSEARCH_ENDPOINT must include a valid host")
	}

	// Validate OpenSearch region
	if config.OpenSearchRegion == "" {
		return fmt.Errorf("OPENSEARCH_REGION is required when OpenSearch is enabled")
	}

	// Validate rate limiting configuration
	if config.OpenSearchRateLimit <= 0 {
		return fmt.Errorf("OPENSEARCH_RATE_LIMIT must be greater than 0")
	}
	if config.OpenSearchRateLimit > 1000 {
		return fmt.Errorf("OPENSEARCH_RATE_LIMIT cannot exceed 1000 requests/second")
	}

	if config.OpenSearchRateBurst <= 0 {
		return fmt.Errorf("OPENSEARCH_RATE_BURST must be greater than 0")
	}
	if config.OpenSearchRateBurst > int(config.OpenSearchRateLimit*10) {
		return fmt.Errorf("OPENSEARCH_RATE_BURST should not exceed 10x the rate limit")
	}

	// Validate timeout values
	if config.OpenSearchConnectionTimeout <= 0 {
		return fmt.Errorf("OPENSEARCH_CONNECTION_TIMEOUT must be greater than 0")
	}
	if config.OpenSearchRequestTimeout <= 0 {
		return fmt.Errorf("OPENSEARCH_REQUEST_TIMEOUT must be greater than 0")
	}

	// Validate retry configuration
	if config.OpenSearchMaxRetries < 0 {
		return fmt.Errorf("OPENSEARCH_MAX_RETRIES cannot be negative")
	}
	if config.OpenSearchMaxRetries > 10 {
		return fmt.Errorf("OPENSEARCH_MAX_RETRIES cannot exceed 10")
	}

	if config.OpenSearchRetryDelay <= 0 {
		return fmt.Errorf("OPENSEARCH_RETRY_DELAY must be greater than 0")
	}

	// Validate connection pool settings
	if config.OpenSearchMaxConnections <= 0 {
		return fmt.Errorf("OPENSEARCH_MAX_CONNECTIONS must be greater than 0")
	}
	if config.OpenSearchMaxConnections > 100 {
		return fmt.Errorf("OPENSEARCH_MAX_CONNECTIONS cannot exceed 100")
	}

	if config.OpenSearchMaxIdleConns <= 0 {
		return fmt.Errorf("OPENSEARCH_MAX_IDLE_CONNS must be greater than 0")
	}
	if config.OpenSearchMaxIdleConns > config.OpenSearchMaxConnections {
		return fmt.Errorf("OPENSEARCH_MAX_IDLE_CONNS cannot exceed OPENSEARCH_MAX_CONNECTIONS")
	}

	if config.OpenSearchIdleConnTimeout <= 0 {
		return fmt.Errorf("OPENSEARCH_IDLE_CONN_TIMEOUT must be greater than 0")
	}

	return nil
}

// validateOTelConfig validates OpenTelemetry-specific configuration.
func validateOTelConfig(config *Config) error {
	if strings.TrimSpace(config.OTelServiceName) == "" {
		return fmt.Errorf("OTEL_SERVICE_NAME cannot be empty when OTEL_ENABLED is true")
	}

	if strings.TrimSpace(config.OTelExporterOTLPEndpoint) == "" {
		return fmt.Errorf("OTEL_EXPORTER_OTLP_ENDPOINT cannot be empty when OTEL_ENABLED is true")
	}

	switch config.OTelExporterOTLPProtocol {
	case "http/protobuf":
		if !strings.HasPrefix(config.OTelExporterOTLPEndpoint, "http://") &&
			!strings.HasPrefix(config.OTelExporterOTLPEndpoint, "https://") {
			return fmt.Errorf("OTEL_EXPORTER_OTLP_ENDPOINT must include http or https scheme when using http/protobuf protocol")
		}

		parsedURL, err := url.Parse(config.OTelExporterOTLPEndpoint)
		if err != nil {
			return fmt.Errorf("invalid OTEL_EXPORTER_OTLP_ENDPOINT: %w", err)
		}
		if parsedURL.Host == "" {
			return fmt.Errorf("OTEL_EXPORTER_OTLP_ENDPOINT must include a host when using http/protobuf protocol")
		}
	case "grpc":
		// gRPC transport allows host:port without scheme, ensure minimal validation.
		if strings.Contains(config.OTelExporterOTLPEndpoint, "://") {
			parsedURL, err := url.Parse(config.OTelExporterOTLPEndpoint)
			if err != nil {
				return fmt.Errorf("invalid OTEL_EXPORTER_OTLP_ENDPOINT for grpc protocol: %w", err)
			}
			if parsedURL.Host == "" {
				return fmt.Errorf("OTEL_EXPORTER_OTLP_ENDPOINT must include a host when scheme is provided for grpc protocol")
			}
		} else if !strings.Contains(config.OTelExporterOTLPEndpoint, ":") {
			return fmt.Errorf("OTEL_EXPORTER_OTLP_ENDPOINT should include host:port when using grpc protocol")
		}
	default:
		return fmt.Errorf("OTEL_EXPORTER_OTLP_PROTOCOL must be either http/protobuf or grpc")
	}

	if config.OTelTracesSamplerArg < 0 {
		return fmt.Errorf("OTEL_TRACES_SAMPLER_ARG must be non-negative")
	}

	if strings.EqualFold(config.OTelTracesSampler, "traceidratio") {
		if config.OTelTracesSamplerArg <= 0 || config.OTelTracesSamplerArg > 1 {
			return fmt.Errorf("OTEL_TRACES_SAMPLER_ARG must be between 0 and 1 when OTEL_TRACES_SAMPLER is traceidratio")
		}
	}

	return nil
}

// validateMCPConfig validates MCP server-specific configuration
func validateMCPConfig(config *Config) error {
	// Validate MCP server port
	if config.MCPServerPort < 1 || config.MCPServerPort > 65535 {
		return fmt.Errorf("MCP_SERVER_PORT must be between 1 and 65535")
	}

	// Validate MCP server host
	if config.MCPServerHost == "" {
		return fmt.Errorf("MCP_SERVER_HOST cannot be empty")
	}

	// Validate timeout values
	if config.MCPServerReadTimeout <= 0 {
		return fmt.Errorf("MCP_SERVER_READ_TIMEOUT must be greater than 0")
	}
	if config.MCPServerWriteTimeout <= 0 {
		return fmt.Errorf("MCP_SERVER_WRITE_TIMEOUT must be greater than 0")
	}
	if config.MCPServerIdleTimeout <= 0 {
		return fmt.Errorf("MCP_SERVER_IDLE_TIMEOUT must be greater than 0")
	}
	if config.MCPServerShutdownTimeout <= 0 {
		return fmt.Errorf("MCP_SERVER_SHUTDOWN_TIMEOUT must be greater than 0")
	}

	// Validate max header bytes
	if config.MCPServerMaxHeaderBytes <= 0 {
		return fmt.Errorf("MCP_SERVER_MAX_HEADER_BYTES must be greater than 0")
	}
	if config.MCPServerMaxHeaderBytes > 10<<20 { // 10MB limit
		return fmt.Errorf("MCP_SERVER_MAX_HEADER_BYTES cannot exceed 10MB")
	}

	// Validate IP authentication configuration
	if config.MCPIPAuthEnabled && len(config.MCPAllowedIPs) == 0 {
		return fmt.Errorf("MCP_ALLOWED_IPS cannot be empty when IP authentication is enabled")
	}

	// MCP will use OpenSearch index - validate it exists
	if config.OpenSearchIndex == "" {
		return fmt.Errorf("OPENSEARCH_INDEX cannot be empty when MCP server is enabled")
	}

	// Validate search size limits
	if config.MCPDefaultSearchSize < 1 {
		config.MCPDefaultSearchSize = 1
	}
	if config.MCPDefaultSearchSize > 100 {
		config.MCPDefaultSearchSize = 100
	}

	// Validate weight values (should be between 0 and 1)
	if config.MCPDefaultBM25Weight < 0 || config.MCPDefaultBM25Weight > 1 {
		return fmt.Errorf("MCP_DEFAULT_BM25_WEIGHT must be between 0.0 and 1.0")
	}
	if config.MCPDefaultVectorWeight < 0 || config.MCPDefaultVectorWeight > 1 {
		return fmt.Errorf("MCP_DEFAULT_VECTOR_WEIGHT must be between 0.0 and 1.0")
	}

	// Validate timeout seconds
	if config.MCPDefaultTimeoutSeconds <= 0 {
		return fmt.Errorf("MCP_DEFAULT_TIMEOUT_SECONDS must be greater than 0")
	}
	if config.MCPDefaultTimeoutSeconds > 300 { // 5 minutes max
		return fmt.Errorf("MCP_DEFAULT_TIMEOUT_SECONDS cannot exceed 300 seconds")
	}

	return nil
}

// validateSDKCompatibility validates that the configuration is compatible with MCP SDK v0.4.0 requirements
// This function ensures SDK-specific configuration constraints are met (Requirement 3.3)
func validateSDKCompatibility(config *Config) error {
	if config == nil {
		return fmt.Errorf("configuration is nil")
	}

	// Validate SDK server configuration requirements
	if err := validateSDKServerConfig(config); err != nil {
		return fmt.Errorf("SDK server configuration validation failed: %w", err)
	}

	// Validate SDK authentication configuration requirements
	if err := validateSDKAuthConfig(config); err != nil {
		return fmt.Errorf("SDK authentication configuration validation failed: %w", err)
	}

	// Validate SDK tool configuration requirements
	if err := validateSDKToolConfig(config); err != nil {
		return fmt.Errorf("SDK tool configuration validation failed: %w", err)
	}

	// Validate SDK SSE configuration requirements (if SSE is enabled)
	if config.MCPSSEEnabled {
		if err := validateSDKSSEConfig(config); err != nil {
			return fmt.Errorf("SDK SSE configuration validation failed: %w", err)
		}
	}

	return nil
}

// validateSDKServerConfig validates server configuration for SDK v0.4.0 compatibility
func validateSDKServerConfig(config *Config) error {
	// SDK requires specific host validation
	if config.MCPServerHost == "" {
		return fmt.Errorf("MCP server host cannot be empty for SDK compatibility")
	}

	// Validate that host is a valid hostname or IP address
	if net.ParseIP(config.MCPServerHost) == nil {
		// If not a valid IP, validate as hostname
		if config.MCPServerHost == "localhost" || isValidHostname(config.MCPServerHost) {
			// Valid hostname
		} else {
			return fmt.Errorf("MCP server host must be a valid IP address or hostname for SDK compatibility: %s", config.MCPServerHost)
		}
	}

	// SDK requires port to be in valid range
	if config.MCPServerPort < 1 || config.MCPServerPort > 65535 {
		return fmt.Errorf("MCP server port must be between 1 and 65535 for SDK compatibility, got: %d", config.MCPServerPort)
	}

	// SDK requires positive timeout values with reasonable limits
	timeoutChecks := []struct {
		name     string
		value    time.Duration
		minValue time.Duration
		maxValue time.Duration
	}{
		{"read timeout", config.MCPServerReadTimeout, time.Second, 5 * time.Minute},
		{"write timeout", config.MCPServerWriteTimeout, time.Second, 5 * time.Minute},
		{"idle timeout", config.MCPServerIdleTimeout, time.Second, 30 * time.Minute},
		{"shutdown timeout", config.MCPServerShutdownTimeout, time.Second, 2 * time.Minute},
	}

	for _, check := range timeoutChecks {
		if check.value <= 0 {
			return fmt.Errorf("MCP server %s must be positive for SDK compatibility, got: %v", check.name, check.value)
		}
		if check.value < check.minValue {
			return fmt.Errorf("MCP server %s is too small for SDK compatibility, minimum: %v, got: %v", check.name, check.minValue, check.value)
		}
		if check.value > check.maxValue {
			return fmt.Errorf("MCP server %s is too large for SDK compatibility, maximum: %v, got: %v", check.name, check.maxValue, check.value)
		}
	}

	// SDK requires reasonable max header bytes limits
	if config.MCPServerMaxHeaderBytes <= 0 {
		return fmt.Errorf("MCP server max header bytes must be positive for SDK compatibility, got: %d", config.MCPServerMaxHeaderBytes)
	}

	const maxHeaderBytesLimit = 10 << 20 // 10MB
	if config.MCPServerMaxHeaderBytes > maxHeaderBytesLimit {
		return fmt.Errorf("MCP server max header bytes exceeds SDK compatibility limit of 10MB, got: %d", config.MCPServerMaxHeaderBytes)
	}

	return nil
}

// validateSDKAuthConfig validates authentication configuration for SDK compatibility
func validateSDKAuthConfig(config *Config) error {
	// If IP authentication is enabled, validate the IP list for SDK compatibility
	if config.MCPIPAuthEnabled {
		if len(config.MCPAllowedIPs) == 0 {
			return fmt.Errorf("MCP allowed IPs cannot be empty when IP authentication is enabled for SDK compatibility")
		}

		// Validate each IP address format for SDK compatibility
		for i, ip := range config.MCPAllowedIPs {
			if strings.TrimSpace(ip) == "" {
				return fmt.Errorf("MCP allowed IP at index %d cannot be empty for SDK compatibility", i)
			}

			// Parse IP to ensure it's valid
			if parsedIP := net.ParseIP(strings.TrimSpace(ip)); parsedIP == nil {
				return fmt.Errorf("invalid IP address in MCP allowed IPs for SDK compatibility at index %d: %s", i, ip)
			}
		}

		// SDK recommends reasonable limits on allowed IPs
		const maxAllowedIPs = 100
		if len(config.MCPAllowedIPs) > maxAllowedIPs {
			return fmt.Errorf("too many allowed IPs for SDK compatibility, maximum: %d, got: %d", maxAllowedIPs, len(config.MCPAllowedIPs))
		}
	}

	return nil
}

// validateSDKToolConfig validates tool configuration for SDK compatibility
func validateSDKToolConfig(config *Config) error {
	// SDK requires non-empty index name - use OpenSearch index
	if config.OpenSearchIndex == "" {
		return fmt.Errorf("OpenSearch index name cannot be empty for SDK compatibility")
	}

	// Validate index name format for SDK compatibility
	if !isValidIndexName(config.OpenSearchIndex) {
		return fmt.Errorf("OpenSearch index name contains invalid characters for SDK compatibility: %s", config.OpenSearchIndex)
	}

	// SDK requires search size within reasonable bounds
	if config.MCPDefaultSearchSize < 1 {
		return fmt.Errorf("MCP default search size must be at least 1 for SDK compatibility, got: %d", config.MCPDefaultSearchSize)
	}

	const maxSearchSize = 1000
	if config.MCPDefaultSearchSize > maxSearchSize {
		return fmt.Errorf("MCP default search size exceeds SDK compatibility limit of %d, got: %d", maxSearchSize, config.MCPDefaultSearchSize)
	}

	// SDK requires weights to be valid probabilities
	if config.MCPDefaultBM25Weight < 0 || config.MCPDefaultBM25Weight > 1 {
		return fmt.Errorf("MCP default BM25 weight must be between 0.0 and 1.0 for SDK compatibility, got: %f", config.MCPDefaultBM25Weight)
	}

	if config.MCPDefaultVectorWeight < 0 || config.MCPDefaultVectorWeight > 1 {
		return fmt.Errorf("MCP default vector weight must be between 0.0 and 1.0 for SDK compatibility, got: %f", config.MCPDefaultVectorWeight)
	}

	// Validate that weights are reasonable for hybrid search
	totalWeight := config.MCPDefaultBM25Weight + config.MCPDefaultVectorWeight
	if totalWeight <= 0 {
		return fmt.Errorf("combined BM25 and vector weights must be greater than 0 for SDK compatibility, got total: %f", totalWeight)
	}

	// SDK requires reasonable timeout limits
	if config.MCPDefaultTimeoutSeconds <= 0 {
		return fmt.Errorf("MCP default timeout seconds must be positive for SDK compatibility, got: %d", config.MCPDefaultTimeoutSeconds)
	}

	const maxTimeoutSeconds = 600 // 10 minutes
	if config.MCPDefaultTimeoutSeconds > maxTimeoutSeconds {
		return fmt.Errorf("MCP default timeout seconds exceeds SDK compatibility limit of %d seconds, got: %d", maxTimeoutSeconds, config.MCPDefaultTimeoutSeconds)
	}

	// Validate hybrid search tool name for SDK compatibility
	if config.MCPHybridSearchToolName == "" {
		return fmt.Errorf("MCP hybrid search tool name cannot be empty for SDK compatibility")
	}

	if !isValidToolName(config.MCPHybridSearchToolName) {
		return fmt.Errorf("MCP hybrid search tool name contains invalid characters for SDK compatibility: %s", config.MCPHybridSearchToolName)
	}

	return nil
}

// validateSDKSSEConfig validates SSE configuration for SDK compatibility
func validateSDKSSEConfig(config *Config) error {
	// SDK requires positive heartbeat interval
	if config.MCPSSEHeartbeatInterval <= 0 {
		return fmt.Errorf("MCP SSE heartbeat interval must be positive when SSE is enabled for SDK compatibility, got: %v", config.MCPSSEHeartbeatInterval)
	}

	// Validate reasonable heartbeat interval range
	const minHeartbeat = time.Second
	const maxHeartbeat = 10 * time.Minute
	if config.MCPSSEHeartbeatInterval < minHeartbeat {
		return fmt.Errorf("MCP SSE heartbeat interval is too small for SDK compatibility, minimum: %v, got: %v", minHeartbeat, config.MCPSSEHeartbeatInterval)
	}
	if config.MCPSSEHeartbeatInterval > maxHeartbeat {
		return fmt.Errorf("MCP SSE heartbeat interval is too large for SDK compatibility, maximum: %v, got: %v", maxHeartbeat, config.MCPSSEHeartbeatInterval)
	}

	// SDK requires positive buffer size with reasonable limits
	if config.MCPSSEBufferSize <= 0 {
		return fmt.Errorf("MCP SSE buffer size must be positive when SSE is enabled for SDK compatibility, got: %d", config.MCPSSEBufferSize)
	}

	const maxBufferSize = 10000
	if config.MCPSSEBufferSize > maxBufferSize {
		return fmt.Errorf("MCP SSE buffer size exceeds SDK compatibility limit of %d, got: %d", maxBufferSize, config.MCPSSEBufferSize)
	}

	// SDK requires positive max clients with reasonable limits
	if config.MCPSSEMaxClients <= 0 {
		return fmt.Errorf("MCP SSE max clients must be positive when SSE is enabled for SDK compatibility, got: %d", config.MCPSSEMaxClients)
	}

	const maxSSEClients = 1000
	if config.MCPSSEMaxClients > maxSSEClients {
		return fmt.Errorf("MCP SSE max clients exceeds SDK compatibility limit of %d, got: %d", maxSSEClients, config.MCPSSEMaxClients)
	}

	// SDK allows non-negative history size
	if config.MCPSSEHistorySize < 0 {
		return fmt.Errorf("MCP SSE history size cannot be negative for SDK compatibility, got: %d", config.MCPSSEHistorySize)
	}

	const maxHistorySize = 10000
	if config.MCPSSEHistorySize > maxHistorySize {
		return fmt.Errorf("MCP SSE history size exceeds SDK compatibility limit of %d, got: %d", maxHistorySize, config.MCPSSEHistorySize)
	}

	return nil
}

// isValidHostname checks if a string is a valid hostname
func isValidHostname(hostname string) bool {
	if len(hostname) == 0 || len(hostname) > 253 {
		return false
	}

	// Check for invalid characters
	for _, char := range hostname {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') &&
			(char < '0' || char > '9') && char != '-' && char != '.' {
			return false
		}
	}

	// Cannot start or end with hyphen
	if strings.HasPrefix(hostname, "-") || strings.HasSuffix(hostname, "-") {
		return false
	}

	return true
}

// isValidIndexName checks if an index name is valid for SDK compatibility
func isValidIndexName(name string) bool {
	if len(name) == 0 || len(name) > 255 {
		return false
	}

	// Index names should start with a letter or underscore
	if name[0] != '_' && (name[0] < 'a' || name[0] > 'z') && (name[0] < 'A' || name[0] > 'Z') {
		return false
	}

	// Check remaining characters
	for _, char := range name {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') &&
			(char < '0' || char > '9') && char != '_' && char != '-' {
			return false
		}
	}

	return true
}

// isValidToolName checks if a tool name is valid for SDK compatibility
func isValidToolName(name string) bool {
	if len(name) == 0 || len(name) > 100 {
		return false
	}

	// Tool names should be alphanumeric with underscores
	for _, char := range name {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') &&
			(char < '0' || char > '9') && char != '_' {
			return false
		}
	}

	return true
}
