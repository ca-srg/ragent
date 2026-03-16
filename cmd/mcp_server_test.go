package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPServerCmdRegistered(t *testing.T) {
	require.NotNil(t, mcpServerCmd)
	assert.Equal(t, "mcp-server", mcpServerCmd.Use)
	require.NotNil(t, mcpServerCmd.RunE)

	require.NotNil(t, mcpServerCmd.Flags().Lookup("host"))
	require.NotNil(t, mcpServerCmd.Flags().Lookup("port"))
	require.NotNil(t, mcpServerCmd.Flags().Lookup("default-index"))
}

func TestMCPServerCmdFlags(t *testing.T) {
	hostFlag := mcpServerCmd.Flags().Lookup("host")
	portFlag := mcpServerCmd.Flags().Lookup("port")
	defaultIndexFlag := mcpServerCmd.Flags().Lookup("default-index")

	require.NotNil(t, hostFlag)
	require.NotNil(t, portFlag)
	require.NotNil(t, defaultIndexFlag)

	assert.Equal(t, "localhost", hostFlag.DefValue)
	assert.Equal(t, "8080", portFlag.DefValue)
	assert.Equal(t, "ragent-docs", defaultIndexFlag.DefValue)
}
