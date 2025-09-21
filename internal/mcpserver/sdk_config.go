package mcpserver

import (
	"fmt"
	"net"
	"time"

	"github.com/ca-srg/ragent/internal/types"
)

// SDKServerConfig represents the configuration structure expected by the MCP SDK
// This adapter bridges RAGent's configuration with SDK requirements
type SDKServerConfig struct {
	// Server configuration
	Host             string        `json:"host"`
	Port             int           `json:"port"`
	ReadTimeout      time.Duration `json:"read_timeout"`
	WriteTimeout     time.Duration `json:"write_timeout"`
	IdleTimeout      time.Duration `json:"idle_timeout"`
	MaxHeaderBytes   int           `json:"max_header_bytes"`
	GracefulShutdown bool          `json:"graceful_shutdown"`
	ShutdownTimeout  time.Duration `json:"shutdown_timeout"`

	// Authentication configuration
	IPAuthEnabled       bool     `json:"ip_auth_enabled"`
	AllowedIPs          []string `json:"allowed_ips"`
	IPAuthEnableLogging bool     `json:"ip_auth_enable_logging"`

	// Tool configuration
	ToolPrefix            string  `json:"tool_prefix"`
	HybridSearchToolName  string  `json:"hybrid_search_tool_name"`
	DefaultIndexName      string  `json:"default_index_name"`
	DefaultSearchSize     int     `json:"default_search_size"`
	DefaultBM25Weight     float64 `json:"default_bm25_weight"`
	DefaultVectorWeight   float64 `json:"default_vector_weight"`
	DefaultUseJapaneseNLP bool    `json:"default_use_japanese_nlp"`
	DefaultTimeoutSeconds int     `json:"default_timeout_seconds"`

	// SSE (Server-Sent Events) configuration
	SSEEnabled           bool          `json:"sse_enabled"`
	SSEHeartbeatInterval time.Duration `json:"sse_heartbeat_interval"`
	SSEBufferSize        int           `json:"sse_buffer_size"`
	SSEMaxClients        int           `json:"sse_max_clients"`
	SSEHistorySize       int           `json:"sse_history_size"`
}

// ConfigAdapter handles conversion between RAGent Config and SDK configuration
type ConfigAdapter struct {
	config *types.Config
}

// NewConfigAdapter creates a new configuration adapter
func NewConfigAdapter(config *types.Config) *ConfigAdapter {
	return &ConfigAdapter{
		config: config,
	}
}

// ToSDKConfig converts RAGent configuration to SDK-compatible configuration
func (ca *ConfigAdapter) ToSDKConfig() (*SDKServerConfig, error) {
	if ca.config == nil {
		return nil, fmt.Errorf("RAGent configuration is nil")
	}

	// Validate the configuration before conversion
	if err := ca.ValidateSDKCompatibility(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	sdkConfig := &SDKServerConfig{
		// Server configuration mapping
		Host:             ca.config.MCPServerHost,
		Port:             ca.config.MCPServerPort,
		ReadTimeout:      ca.config.MCPServerReadTimeout,
		WriteTimeout:     ca.config.MCPServerWriteTimeout,
		IdleTimeout:      ca.config.MCPServerIdleTimeout,
		MaxHeaderBytes:   ca.config.MCPServerMaxHeaderBytes,
		GracefulShutdown: ca.config.MCPServerGracefulShutdown,
		ShutdownTimeout:  ca.config.MCPServerShutdownTimeout,

		// Authentication configuration mapping
		IPAuthEnabled:       ca.config.MCPIPAuthEnabled,
		AllowedIPs:          ca.config.MCPAllowedIPs,
		IPAuthEnableLogging: ca.config.MCPIPAuthEnableLogging,

		// Tool configuration mapping - use OpenSearch index
		ToolPrefix:            ca.config.MCPToolPrefix,
		HybridSearchToolName:  ca.config.MCPHybridSearchToolName,
		DefaultIndexName:      ca.config.OpenSearchIndex,
		DefaultSearchSize:     ca.config.MCPDefaultSearchSize,
		DefaultBM25Weight:     ca.config.MCPDefaultBM25Weight,
		DefaultVectorWeight:   ca.config.MCPDefaultVectorWeight,
		DefaultUseJapaneseNLP: ca.config.MCPDefaultUseJapaneseNLP,
		DefaultTimeoutSeconds: ca.config.MCPDefaultTimeoutSeconds,

		// SSE configuration mapping
		SSEEnabled:           ca.config.MCPSSEEnabled,
		SSEHeartbeatInterval: ca.config.MCPSSEHeartbeatInterval,
		SSEBufferSize:        ca.config.MCPSSEBufferSize,
		SSEMaxClients:        ca.config.MCPSSEMaxClients,
		SSEHistorySize:       ca.config.MCPSSEHistorySize,
	}

	return sdkConfig, nil
}

// ValidateSDKCompatibility validates that the RAGent configuration is compatible with SDK requirements
func (ca *ConfigAdapter) ValidateSDKCompatibility() error {
	if ca.config == nil {
		return fmt.Errorf("configuration is nil")
	}

	// Validate server configuration
	if err := ca.validateServerConfig(); err != nil {
		return fmt.Errorf("server configuration validation failed: %w", err)
	}

	// Validate authentication configuration
	if err := ca.validateAuthConfig(); err != nil {
		return fmt.Errorf("authentication configuration validation failed: %w", err)
	}

	// Validate tool configuration
	if err := ca.validateToolConfig(); err != nil {
		return fmt.Errorf("tool configuration validation failed: %w", err)
	}

	// Validate SSE configuration
	if err := ca.validateSSEConfig(); err != nil {
		return fmt.Errorf("SSE configuration validation failed: %w", err)
	}

	return nil
}

// validateServerConfig validates server-related configuration fields
func (ca *ConfigAdapter) validateServerConfig() error {
	// Validate host
	if ca.config.MCPServerHost == "" {
		return fmt.Errorf("MCP server host cannot be empty")
	}

	// Validate port range
	if ca.config.MCPServerPort < 1 || ca.config.MCPServerPort > 65535 {
		return fmt.Errorf("MCP server port must be between 1 and 65535, got: %d", ca.config.MCPServerPort)
	}

	// Validate timeout values
	if ca.config.MCPServerReadTimeout <= 0 {
		return fmt.Errorf("MCP server read timeout must be positive, got: %v", ca.config.MCPServerReadTimeout)
	}

	if ca.config.MCPServerWriteTimeout <= 0 {
		return fmt.Errorf("MCP server write timeout must be positive, got: %v", ca.config.MCPServerWriteTimeout)
	}

	if ca.config.MCPServerIdleTimeout <= 0 {
		return fmt.Errorf("MCP server idle timeout must be positive, got: %v", ca.config.MCPServerIdleTimeout)
	}

	if ca.config.MCPServerShutdownTimeout <= 0 {
		return fmt.Errorf("MCP server shutdown timeout must be positive, got: %v", ca.config.MCPServerShutdownTimeout)
	}

	// Validate max header bytes
	if ca.config.MCPServerMaxHeaderBytes <= 0 {
		return fmt.Errorf("MCP server max header bytes must be positive, got: %d", ca.config.MCPServerMaxHeaderBytes)
	}

	if ca.config.MCPServerMaxHeaderBytes > 10<<20 { // 10MB limit
		return fmt.Errorf("MCP server max header bytes cannot exceed 10MB, got: %d", ca.config.MCPServerMaxHeaderBytes)
	}

	return nil
}

// validateAuthConfig validates authentication-related configuration fields
func (ca *ConfigAdapter) validateAuthConfig() error {
	// If IP authentication is enabled, validate allowed IPs
	if ca.config.MCPIPAuthEnabled && len(ca.config.MCPAllowedIPs) == 0 {
		return fmt.Errorf("MCP allowed IPs cannot be empty when IP authentication is enabled")
	}

	// Validate IP addresses format
	for _, ip := range ca.config.MCPAllowedIPs {
		if net.ParseIP(ip) == nil {
			return fmt.Errorf("invalid IP address in allowed IPs: %s", ip)
		}
	}

	return nil
}

// validateToolConfig validates tool-related configuration fields
func (ca *ConfigAdapter) validateToolConfig() error {
	// Validate OpenSearch index name (MCP will use the same index as other commands)
	if ca.config.OpenSearchIndex == "" {
		return fmt.Errorf("OpenSearch index name cannot be empty")
	}

	// Validate search size limits
	if ca.config.MCPDefaultSearchSize < 1 || ca.config.MCPDefaultSearchSize > 100 {
		return fmt.Errorf("MCP default search size must be between 1 and 100, got: %d", ca.config.MCPDefaultSearchSize)
	}

	// Validate weight values (should be between 0 and 1)
	if ca.config.MCPDefaultBM25Weight < 0 || ca.config.MCPDefaultBM25Weight > 1 {
		return fmt.Errorf("MCP default BM25 weight must be between 0.0 and 1.0, got: %f", ca.config.MCPDefaultBM25Weight)
	}

	if ca.config.MCPDefaultVectorWeight < 0 || ca.config.MCPDefaultVectorWeight > 1 {
		return fmt.Errorf("MCP default vector weight must be between 0.0 and 1.0, got: %f", ca.config.MCPDefaultVectorWeight)
	}

	// Validate timeout seconds
	if ca.config.MCPDefaultTimeoutSeconds <= 0 {
		return fmt.Errorf("MCP default timeout seconds must be positive, got: %d", ca.config.MCPDefaultTimeoutSeconds)
	}

	if ca.config.MCPDefaultTimeoutSeconds > 300 { // 5 minutes max
		return fmt.Errorf("MCP default timeout seconds cannot exceed 300, got: %d", ca.config.MCPDefaultTimeoutSeconds)
	}

	// Validate tool name
	if ca.config.MCPHybridSearchToolName == "" {
		return fmt.Errorf("MCP hybrid search tool name cannot be empty")
	}

	return nil
}

// validateSSEConfig validates SSE-related configuration fields
func (ca *ConfigAdapter) validateSSEConfig() error {
	// Only validate SSE settings if SSE is enabled
	if !ca.config.MCPSSEEnabled {
		return nil
	}

	// Validate heartbeat interval
	if ca.config.MCPSSEHeartbeatInterval <= 0 {
		return fmt.Errorf("MCP SSE heartbeat interval must be positive when SSE is enabled, got: %v", ca.config.MCPSSEHeartbeatInterval)
	}

	// Validate buffer size
	if ca.config.MCPSSEBufferSize <= 0 {
		return fmt.Errorf("MCP SSE buffer size must be positive when SSE is enabled, got: %d", ca.config.MCPSSEBufferSize)
	}

	// Validate max clients
	if ca.config.MCPSSEMaxClients <= 0 {
		return fmt.Errorf("MCP SSE max clients must be positive when SSE is enabled, got: %d", ca.config.MCPSSEMaxClients)
	}

	// Validate history size
	if ca.config.MCPSSEHistorySize < 0 {
		return fmt.Errorf("MCP SSE history size cannot be negative, got: %d", ca.config.MCPSSEHistorySize)
	}

	return nil
}

// GetServerAddress returns the full server address from the configuration
func (ca *ConfigAdapter) GetServerAddress() string {
	return fmt.Sprintf("%s:%d", ca.config.MCPServerHost, ca.config.MCPServerPort)
}

// IsSecureTransport returns whether the server should use secure transport
func (ca *ConfigAdapter) IsSecureTransport() bool {
	// For now, assume HTTP transport. This could be extended in the future
	// to support HTTPS based on additional configuration fields
	return false
}
