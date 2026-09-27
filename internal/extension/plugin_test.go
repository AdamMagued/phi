package extension_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ext "github.com/pulseaiclub/phi/ext/go"
	"github.com/pulseaiclub/phi/internal/extension"
)

// fakePlugin is a built-in plugin with a tool, a command and a prompt block.
func fakePlugin(t *testing.T, closed *bool) extension.Plugin {
	t.Helper()
	api := ext.NewAPI()
	api.RegisterTool(ext.Tool{
		Name:        "fake_tool",
		Description: "a built-in tool",
		Parameters:  map[string]any{"type": "object"},
		Execute: func(context.Context, json.RawMessage) (ext.ToolResult, error) {
			return ext.ToolResult{Content: "ok", Output: "ok"}, nil
		},
	})
	api.RegisterCommand("fake_cmd", ext.Command{
		Handler: func(string, *ext.Context) error { return nil },
	})

	api.RegisterAssembler("", "# fake\n\nbuilt in")
	return extension.Plugin{
		API: api,
		Close: func() error {
			*closed = true
			return nil
		},
	}
}

// assemble runs the pipeline with a stub core, the way the engine does.
func assemble(t *testing.T, runner *extension.Runner, core ...string) []string {
	t.Helper()
	return runner.AssemblePrompt(extension.AssembleContext{}, func() []string { return core })
}

func TestAddPluginReachesTheHostLikeASubprocessExtension(t *testing.T) {
	runner := extension.NewRunner()

	closed := false
	plugin := fakePlugin(t, &closed)
	runner.AddPlugin(plugin)

	tools := runner.ExtensionTools()
	require.Len(t, tools, 1)
	assert.Equal(t, "fake_tool", tools[0].Definition.Name)
	assert.Equal(t, "a built-in tool", tools[0].Definition.Description)

	entries := runner.CommandEntries()
	require.Len(t, entries, 1)
	assert.Equal(t, "fake_cmd", entries[0].Name)

	assert.Equal(t, []string{"core", "# fake\n\nbuilt in"}, assemble(t, runner, "core"))

	// An extension asking the host what exists must see built-in tools as
	// builtin, not as something from another source.
	runner.Bind(ext.HostOpts{})
	infos := plugin.API.GetAllTools()
	require.Len(t, infos, 1)
	assert.Equal(t, "fake_tool", infos[0].Name)
	assert.Equal(t, "builtin", infos[0].Source)

	runner.Close()
	assert.True(t, closed, "Runner.Close must release plugin resources")
}

// Registered order is prompt order: the first plugin's block sits closest to
// the core prompt, so adding a plugin later never reshuffles what came before.
func TestAssemblersRunInRegistrationOrder(t *testing.T) {
	runner := extension.NewRunner()
	t.Cleanup(runner.Close)

	for _, section := range []string{"# one", "# two"} {
		api := ext.NewAPI()
		api.RegisterAssembler("", section)
		runner.AddPlugin(extension.Plugin{API: api})
	}

	assert.Equal(t, []string{"core", "# one", "# two"}, assemble(t, runner, "core"))
}

// Blank blocks are dropped and the rest trimmed, so a plugin that builds its
// block from possibly-empty config cannot leave ragged gaps in the prompt.
func TestAssemblerDropsBlanksAndTrims(t *testing.T) {
	runner := extension.NewRunner()
	t.Cleanup(runner.Close)

	api := ext.NewAPI()
	api.RegisterAssembler("", "  ")
	api.RegisterAssembler("", "")
	api.RegisterAssembler("", "  # kept  ")
	runner.AddPlugin(extension.Plugin{API: api})

	assert.Equal(t, []string{"core", "# kept"}, assemble(t, runner, "core"))
}

// A section's Scope gates which engines see it: a sub-agent registers no
// extension tools, so a block announcing them must be skippable. This is the
// hook that keeps the prompt honest about what the model can actually call.
func TestAssemblerFiltersByScope(t *testing.T) {
	runner := extension.NewRunner()
	t.Cleanup(runner.Close)

	api := ext.NewAPI()
	api.RegisterAssembler(ext.ScopeMain, "main-only")
	api.RegisterAssembler(ext.ScopeSubagent, "sub-only")
	runner.AddPlugin(extension.Plugin{API: api})

	core := func() []string { return []string{"core"} }
	main := runner.AssemblePrompt(extension.AssembleContext{Scope: ext.ScopeMain}, core)
	assert.Equal(t, []string{"core", "main-only"}, main)

	sub := runner.AssemblePrompt(extension.AssembleContext{Scope: ext.ScopeSubagent}, core)
	assert.Equal(t, []string{"core", "sub-only"}, sub)
}

// A plugin with no API and nothing to close is inert, so a caller can build one
// from a nil dependency (an MCP-less session) without branching.
func TestZeroPluginIsIgnored(t *testing.T) {
	runner := extension.NewRunner()
	t.Cleanup(runner.Close)

	runner.AddPlugin(extension.Plugin{})

	assert.Nil(t, runner.ExtensionTools())
	assert.Equal(t, []string{"core"}, assemble(t, runner, "core"))
}

// A plugin can carry prompt sections and nothing to call: its API registers no
// tools, but the bus still renders its blocks.
func TestSectionsOnlyPluginAssembles(t *testing.T) {
	runner := extension.NewRunner()
	t.Cleanup(runner.Close)

	api := ext.NewAPI()
	api.RegisterAssembler("", "# text only")
	runner.AddPlugin(extension.Plugin{API: api})

	assert.Nil(t, runner.ExtensionTools())
	assert.Equal(t, []string{"core", "# text only"}, assemble(t, runner, "core"))
}

func TestNopHostAssemblesTheCorePrompt(t *testing.T) {
	sections := extension.Nop.AssemblePrompt(extension.AssembleContext{}, func() []string {
		return []string{"core"}
	})

	assert.Equal(t, []string{"core"}, sections)
}
