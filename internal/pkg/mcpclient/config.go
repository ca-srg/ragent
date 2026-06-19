package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	appconfig "github.com/ca-srg/ragent/internal/pkg/config"
)

const (
	secretsManagerSourcePrefix = "secretsmanager://"
	secretManagerSourcePrefix  = "secretmanager://"
)

var loadMCPConfigSecret = appconfig.LoadSecretString

// Config is a minimal JSONC-compatible MCP client config.
type Config struct {
	MCPServers       map[string]ServerConfig `json:"mcpServers"`
	Servers          map[string]ServerConfig `json:"servers,omitempty"`
	TimeoutSeconds   int                     `json:"timeoutSeconds,omitempty"`
	MaxTools         int                     `json:"maxTools,omitempty"`
	MaxResponseChars int                     `json:"maxResponseChars,omitempty"`
}

// ServerConfig describes one MCP server entry.
type ServerConfig struct {
	Command       string            `json:"command,omitempty"`
	Args          []string          `json:"args,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	CWD           string            `json:"cwd,omitempty"`
	URL           string            `json:"url,omitempty"`
	Endpoint      string            `json:"endpoint,omitempty"`
	Transport     string            `json:"transport,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	Tools         []string          `json:"tools,omitempty"`
	DisabledTools []string          `json:"disabledTools,omitempty"`
	// Explicit tool allowlists are trusted operator config; mutating tools need opt-in.
	AllowDestructiveTools bool           `json:"allowDestructiveTools,omitempty"`
	AllowMutatingTools    bool           `json:"allowMutatingTools,omitempty"`
	QueryParam            string         `json:"queryParam,omitempty"`
	Arguments             map[string]any `json:"arguments,omitempty"`
	Disabled              bool           `json:"disabled,omitempty"`
}

func (c *Config) serverMap() map[string]ServerConfig {
	if len(c.MCPServers) > 0 {
		return c.MCPServers
	}
	return c.Servers
}

// LoadConfigSource reads mcp-config.jsonc from a file path or secretsmanager://SECRET_ID.
func LoadConfigSource(ctx context.Context, source string) (*Config, error) {
	source = strings.TrimSpace(source)
	if secretID, ok := mcpConfigSecretID(source); ok {
		if secretID == "" {
			return nil, fmt.Errorf("MCP config Secrets Manager source must include a secret ID")
		}
		secret, err := loadMCPConfigSecret(ctx, secretID, "")
		if err != nil {
			return nil, fmt.Errorf("failed to read MCP config from Secrets Manager: %w", err)
		}
		return ParseConfig([]byte(secret))
	}
	return LoadConfig(source)
}

// LoadConfig reads mcp-config.jsonc from a file. It supports //, /* */ comments and trailing commas.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read MCP config: %w", err)
	}
	return ParseConfig(data)
}

// ParseConfig parses JSONC-compatible MCP client config bytes.
func ParseConfig(data []byte) (*Config, error) {
	clean := stripTrailingCommas(stripJSONComments(data))
	var cfg Config
	if err := json.Unmarshal(clean, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse MCP config: %w", err)
	}
	if len(cfg.serverMap()) == 0 {
		return nil, fmt.Errorf("MCP config must contain at least one mcpServers entry")
	}
	return &cfg, nil
}

func mcpConfigSecretID(source string) (string, bool) {
	lower := strings.ToLower(source)
	for _, prefix := range []string{secretsManagerSourcePrefix, secretManagerSourcePrefix} {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(source[len(prefix):]), true
		}
	}
	return "", false
}

func stripJSONComments(in []byte) []byte {
	out := make([]byte, 0, len(in))
	inString := false
	escape := false

	for i := 0; i < len(in); i++ {
		ch := in[i]
		if inString {
			out = append(out, ch)
			if escape {
				escape = false
				continue
			}
			switch ch {
			case '\\':
				escape = true
			case '"':
				inString = false
			}
			continue
		}

		if ch == '"' {
			inString = true
			out = append(out, ch)
			continue
		}
		if ch == '/' && i+1 < len(in) {
			switch in[i+1] {
			case '/':
				i += 2
				for i < len(in) && in[i] != '\n' {
					i++
				}
				if i < len(in) {
					out = append(out, '\n')
				}
				continue
			case '*':
				i += 2
				for i+1 < len(in) && (in[i] != '*' || in[i+1] != '/') {
					if in[i] == '\n' {
						out = append(out, '\n')
					}
					i++
				}
				i++
				continue
			}
		}
		out = append(out, ch)
	}
	return out
}

func stripTrailingCommas(in []byte) []byte {
	out := make([]byte, 0, len(in))
	inString := false
	escape := false

	for i := 0; i < len(in); i++ {
		ch := in[i]
		if inString {
			out = append(out, ch)
			if escape {
				escape = false
				continue
			}
			switch ch {
			case '\\':
				escape = true
			case '"':
				inString = false
			}
			continue
		}

		if ch == '"' {
			inString = true
			out = append(out, ch)
			continue
		}
		if ch == ',' {
			j := i + 1
			for j < len(in) && (in[j] == ' ' || in[j] == '\n' || in[j] == '\r' || in[j] == '\t') {
				j++
			}
			if j < len(in) && (in[j] == '}' || in[j] == ']') {
				continue
			}
		}
		out = append(out, ch)
	}
	return out
}
