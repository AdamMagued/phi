package agent_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/phi/internal/agent"
	"github.com/pulseaiclub/phi/internal/extension"
	"github.com/pulseaiclub/phi/internal/llm"
	"github.com/pulseaiclub/phi/internal/mcp"
)

// MCP reaches the engine as a built-in plugin on the extension bus, so these
// assertions also cover the plugin seam end to end.
func TestEngineRegistersMCPMetaTools(t *testing.T) {
	pool := mcp.NewPool(map[string]mcp.ServerConfig{
		"echo": {Command: []string{"true"}},
	})

	runner := extension.NewRunner()
	t.Cleanup(runner.Close)
	runner.AddPlugin(mcp.Plugin(pool))

	sess, err := agent.NewSession(agent.WithCwd(t.TempDir()))
	require.NoError(t, err)
	eng, err := agent.NewEngine(
		llm.ModelConfig{Name: "test", APIKey: "x", BaseURL: "http://127.0.0.1:9"},
		sess,
		agent.WithExtensions(runner),
	)
	require.NoError(t, err)
	for _, name := range []string{"mcp_list", "mcp_inspect", "mcp_call", "bash"} {
		require.True(t, eng.HasTool(name), "missing tool %s", name)
	}

	// Explicit child tool list without MCP → no meta tools (sub-agent path).
	sess2, err := agent.NewSession(agent.WithCwd(t.TempDir()))
	require.NoError(t, err)
	eng2, err := agent.NewEngine(
		llm.ModelConfig{Name: "test", APIKey: "x", BaseURL: "http://127.0.0.1:9"},
		sess2,
		agent.WithTools(agent.ChildTools()),
	)
	require.NoError(t, err)
	require.False(t, eng2.HasTool("mcp_list"), "child tools should not include mcp_list")

	// Without the plugin the engine has no MCP surface at all: the core tool
	// list no longer knows MCP exists.
	sess3, err := agent.NewSession(agent.WithCwd(t.TempDir()))
	require.NoError(t, err)
	eng3, err := agent.NewEngine(
		llm.ModelConfig{Name: "test", APIKey: "x", BaseURL: "http://127.0.0.1:9"},
		sess3,
	)
	require.NoError(t, err)
	require.False(t, eng3.HasTool("mcp_list"))
}
