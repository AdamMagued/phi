package mcp_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ext "github.com/pulseaiclub/phi/ext/go"
	"github.com/pulseaiclub/phi/internal/extension"
	"github.com/pulseaiclub/phi/internal/tools/mcp"
)

func testPool() *mcp.Pool {
	return mcp.NewPool(map[string]mcp.ServerConfig{
		"echo": {Command: []string{"true"}},
	})
}

// toolsByName indexes a plugin's registered tools, which is how the host sees
// them: an API plus a flat tool list.
func toolsByName(t *testing.T, api *ext.API) map[string]ext.Tool {
	t.Helper()
	byName := map[string]ext.Tool{}
	for _, tool := range api.Tools() {
		byName[tool.Name] = tool
	}
	return byName
}

// assemble runs the plugin's prompt sections the way an engine does.
func assemble(t *testing.T, plugin extension.Plugin, scope ext.Scope) []string {
	t.Helper()
	runner := extension.NewRunner()
	t.Cleanup(runner.Close)
	runner.AddPlugin(plugin)
	return runner.AssemblePrompt(
		extension.AssembleContext{Scope: scope},
		func() []string { return []string{"core"} },
	)
}

func TestPluginRegistersMetaTools(t *testing.T) {
	plugin := mcp.Plugin(testPool())
	require.NotNil(t, plugin.API)

	byName := toolsByName(t, plugin.API)
	for _, name := range []string{"mcp_list", "mcp_inspect", "mcp_call"} {
		assert.Contains(t, byName, name)
	}
	require.Len(t, byName, 3, "MCP exposes exactly the three meta-tools")

	// Schemas must survive the ext.Tool → tools.Tool round trip the host does:
	// the model only ever sees the required-args list.
	require.Equal(t, []string{"server"}, byName["mcp_list"].Parameters["required"])
	require.Equal(t, []string{"server", "tool"}, byName["mcp_inspect"].Parameters["required"])
	require.Equal(t, []string{"server", "tool"}, byName["mcp_call"].Parameters["required"])
	assert.Equal(t, "object", byName["mcp_call"].Parameters["type"])
}

func TestPluginNilPool(t *testing.T) {
	plugin := mcp.Plugin(nil)
	assert.Nil(t, plugin.API)
	assert.Nil(t, plugin.Close)
}

func TestPluginOwnsPoolLifetime(t *testing.T) {
	plugin := mcp.Plugin(testPool())
	require.NotNil(t, plugin.Close)
	require.NoError(t, plugin.Close())
}

func TestMCPListRequiresServer(t *testing.T) {
	byName := toolsByName(t, mcp.Plugin(testPool()).API)
	_, err := byName["mcp_list"].Execute(t.Context(), []byte(`{}`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "server is required")
}

func TestPluginPromptSection(t *testing.T) {
	pool := mcp.NewPool(map[string]mcp.ServerConfig{
		"browsermcp": {Command: []string{"true"}},
		"github":     {Command: []string{"true"}},
	})
	sections := assemble(t, mcp.Plugin(pool), ext.ScopeMain)
	require.Len(t, sections, 2)
	require.Equal(t, "core", sections[0], "the block goes after everything else")

	got := sections[1]
	require.Contains(t, got, "# MCP")
	require.Contains(t, got, "- browsermcp")
	require.Contains(t, got, "- github")
	require.Contains(t, got, "docs/URLs")
	for _, name := range []string{"mcp_list", "mcp_inspect", "mcp_call"} {
		require.Contains(t, got, name)
	}
	// Tool schemas must never reach the prompt.
	require.NotContains(t, got, `"properties"`)
	require.NotContains(t, got, "inputSchema")
	require.NotContains(t, got, "\n\n\n")
}

// A sub-agent gets no mcp_* tools, so promising them in its prompt would send
// the model after tools it cannot call.
func TestPluginPromptSectionSkippedForSubagents(t *testing.T) {
	sections := assemble(t, mcp.Plugin(testPool()), ext.ScopeSubagent)

	assert.Equal(t, []string{"core"}, sections)
}

func TestPluginPromptSectionEmptyWithoutServers(t *testing.T) {
	assert.Equal(t, []string{"core"}, assemble(t, mcp.Plugin(mcp.NewPool(nil)), ext.ScopeMain))
}
