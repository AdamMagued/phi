package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/phi/internal/extension"
	"github.com/pulseaiclub/phi/internal/job"
	"github.com/pulseaiclub/phi/internal/llm"
	"github.com/pulseaiclub/phi/internal/mcp"
	"github.com/pulseaiclub/phi/internal/permission"
)

func probeRunner(t *testing.T, seen *extension.AssembleContext, text string) *extension.Runner {
	t.Helper()
	runner := extension.NewRunner()
	t.Cleanup(runner.Close)
	runner.AddPlugin(extension.Plugin{Assemble: func(ac extension.AssembleContext) []string {
		*seen = ac
		if text == "" {
			return nil
		}
		return []string{text}
	}})
	return runner
}

func probeModel(t *testing.T) llm.ModelConfig {
	t.Helper()
	return llm.ModelConfig{
		Name:      "fake",
		BaseURL:   "http://127.0.0.1:9",
		APIKey:    "x",
		SkillPath: t.TempDir(),
	}
}

func newPromptEngine(
	t *testing.T,
	host extension.Host,
	model llm.ModelConfig,
	opts ...EngineOption,
) *Engine {
	t.Helper()
	sess, err := NewSession(WithCwd(t.TempDir()))
	require.NoError(t, err)
	engine, err := NewEngine(
		model,
		sess,
		append([]EngineOption{WithGate(permission.AllowAll{}), WithExtensions(host)}, opts...)...,
	)
	require.NoError(t, err)
	return engine
}

func TestSystemPromptPlacesPluginBlocksLast(t *testing.T) {
	seen := &extension.AssembleContext{}
	model := probeModel(t)

	plain := newPromptEngine(t, extension.Nop, model).systemPrompt()
	require.NotContains(t, plain, "# probe")

	prompt := newPromptEngine(t, probeRunner(t, seen, "# probe"), model).systemPrompt()
	require.True(t, strings.HasSuffix(prompt, "# probe"), "plugin blocks come after the built-in prompt")
}

// The context is what an assembler has instead of asking the engine: it must
// describe the same prompt the core blocks were built from, on every rebuild.
func TestAssembleContextMirrorsTheEngine(t *testing.T) {
	seen := &extension.AssembleContext{}
	runner := probeRunner(t, seen, "# probe")
	model := probeModel(t)
	newPromptEngine(t, runner, model)

	require.Equal(t, extension.ScopeMain, seen.Scope)
	require.Equal(t, model.SkillPath, seen.SkillPath)
	require.False(t, seen.AgentsEnabled)
	require.Zero(t, seen.MaxConcurrent)

	// A model switch rebuilds the prompt, and the context follows it.
	switched := probeModel(t)
	newPromptEngine(t, runner, model).SetModel(switched)
	require.Equal(t, switched.SkillPath, seen.SkillPath)
}

func TestAssembleContextReportsSubAgentTools(t *testing.T) {
	seen := &extension.AssembleContext{}
	mgr, err := job.New(job.Options{
		Root: t.TempDir(),
		Runner: job.RunnerFunc(func(context.Context, job.RunEnv) (string, error) {
			return "ok", nil
		}),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = mgr.Close(t.Context()) })

	engine := newPromptEngine(t, probeRunner(t, seen, "# probe"), probeModel(t), WithJobs(mgr))

	require.True(t, seen.AgentsEnabled)
	require.Equal(t, mgr.MaxConcurrent(), seen.MaxConcurrent)
	require.Contains(t, engine.systemPrompt(), "At most")
}

// A sub-agent dispatches extension events but registers no extension tools, so
// a block announcing those tools must not be added: it would send the model
// after tools it cannot call. MCP ships exactly this way.
func TestSubagentPromptDoesNotAdvertiseExtensionTools(t *testing.T) {
	pool := mcp.NewPool(map[string]mcp.ServerConfig{"echo": {Command: []string{"true"}}})
	runner := extension.NewRunner()
	t.Cleanup(runner.Close)
	runner.AddPlugin(mcp.Plugin(pool))

	main := newPromptEngine(t, runner, probeModel(t))
	require.True(t, main.HasTool("mcp_list"))
	require.Contains(t, main.systemPrompt(), "# MCP")

	child := newPromptEngine(t, runner, probeModel(t), WithOmitExtensionTools(true))
	require.False(t, child.HasTool("mcp_list"))
	require.NotContains(t, child.systemPrompt(), "# MCP")
}
