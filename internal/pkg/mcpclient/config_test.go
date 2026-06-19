package mcpclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfigJSONC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp-config.jsonc")
	data := []byte(`{
		// standard MCP client config shape
		"mcpServers": {
			"local": {
				"command": "ragent",
				"args": ["mcp-server"],
				"env": {"TOKEN": "placeholder",},
			},
		},
		"timeoutSeconds": 3,
	}`)
	require.NoError(t, os.WriteFile(path, data, 0o600))

	cfg, err := LoadConfig(path)
	require.NoError(t, err)
	require.Contains(t, cfg.MCPServers, "local")
	assert.Equal(t, "ragent", cfg.MCPServers["local"].Command)
	assert.Equal(t, []string{"mcp-server"}, cfg.MCPServers["local"].Args)
	assert.Equal(t, 3, cfg.TimeoutSeconds)
}

func TestLoadConfigSourceSecretsManager(t *testing.T) {
	old := loadMCPConfigSecret
	t.Cleanup(func() { loadMCPConfigSecret = old })

	var gotSecretID string
	loadMCPConfigSecret = func(ctx context.Context, secretID, region string) (string, error) {
		gotSecretID = secretID
		return `{
			// raw JSONC stored in Secrets Manager
			"mcpServers": {"local": {"command": "ragent", "args": ["mcp-server"]}},
		}`, nil
	}

	cfg, err := LoadConfigSource(context.Background(), "secretsmanager://ragent/mcp-config")
	require.NoError(t, err)
	assert.Equal(t, "ragent/mcp-config", gotSecretID)
	assert.Equal(t, "ragent", cfg.MCPServers["local"].Command)
}

func TestLoadConfigSourceSecretsManagerRequiresSecretID(t *testing.T) {
	_, err := LoadConfigSource(context.Background(), "secretsmanager://")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "secret ID")
}

func TestSelectToolsUsesAllowlist(t *testing.T) {
	tools := []*mcp.Tool{
		{Name: "search", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}, InputSchema: &jsonschema.Schema{Properties: map[string]*jsonschema.Schema{"query": {}}}},
		{Name: "write", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}, InputSchema: &jsonschema.Schema{Properties: map[string]*jsonschema.Schema{"content": {}}}},
	}

	selected, err := selectTools("test", ServerConfig{Tools: []string{"search"}}, tools)
	require.NoError(t, err)
	require.Len(t, selected, 1)
	assert.Equal(t, "search", selected[0].Name)
}

func TestAvailableToolsReturnsSelectedToolInfo(t *testing.T) {
	manager := &Manager{sessions: []*serverSession{{
		name: "docs",
		selected: []*mcp.Tool{{
			Name:        "search",
			Description: "Search internal docs",
			InputSchema: &jsonschema.Schema{Properties: map[string]*jsonschema.Schema{"query": {}}},
		}},
	}}}

	tools := manager.AvailableTools()

	require.Len(t, tools, 1)
	assert.Equal(t, "docs", tools[0].Server)
	assert.Equal(t, "search", tools[0].Name)
	assert.Equal(t, "Search internal docs", tools[0].Description)
	assert.JSONEq(t, `{"properties":{"query":true}}`, string(tools[0].InputSchema))
}

func TestSelectToolsRequiresAllowlist(t *testing.T) {
	_, err := selectTools("test", ServerConfig{}, []*mcp.Tool{{Name: "search"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "allowlist")
}

func TestSelectToolsFailsOnMissingExplicitTool(t *testing.T) {
	_, err := selectTools("test", ServerConfig{Tools: []string{"missing"}}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing")
}

func TestSelectToolsRejectsUnannotatedToolWithoutOptIn(t *testing.T) {
	_, err := selectTools("test", ServerConfig{Tools: []string{"search"}}, []*mcp.Tool{{Name: "search"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read-only")
}

func TestSelectToolsAllowsReadOnlyTool(t *testing.T) {
	selected, err := selectTools("test", ServerConfig{Tools: []string{"search"}}, []*mcp.Tool{{
		Name:        "search",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}})
	require.NoError(t, err)
	require.Len(t, selected, 1)
}

func TestQueryRejectsRecursiveMCPCall(t *testing.T) {
	manager := &Manager{}
	_, err := manager.Query(WithRecursionDepth(context.Background(), maxRecursionDepth), "query")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "recursion")
}
