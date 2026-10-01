package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	ext "github.com/pulseaiclub/phi/ext/go"
	"github.com/pulseaiclub/phi/internal/extension"
	"github.com/pulseaiclub/phi/internal/job"
	"github.com/pulseaiclub/phi/internal/llm"
	"github.com/pulseaiclub/phi/internal/permission"
	"github.com/pulseaiclub/phi/internal/tools/mcp"
)

func probeRunner(t *testing.T, text string) *extension.Runner {
	t.Helper()
	runner := extension.NewRunner()
	t.Cleanup(runner.Close)
	api := ext.NewAPI()
	api.RegisterPromptSection(ext.ScopeMain, text)
	runner.AddPlugin(extension.Plugin{API: api})
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
	model := probeModel(t)

	plain := newPromptEngine(t, extension.Nop, model).systemPrompt()
	require.NotContains(t, plain, "# probe")

	prompt := newPromptEngine(t, probeRunner(t, "# probe"), model).systemPrompt()
	require.True(t, strings.HasSuffix(prompt, "# probe"), "plugin blocks come after the built-in prompt")
}

// A model switch rebuilds the prompt from scratch, so a plugin block stays in
// sync with the new core rather than surviving as a stale cache.
func TestPromptRebuildsOnModelSwitch(t *testing.T) {
	model := probeModel(t)
	runner := probeRunner(t, "# probe")

	engine := newPromptEngine(t, runner, model)
	require.Contains(t, engine.systemPrompt(), "# probe")

	engine.SetModel(probeModel(t))
	require.Contains(t, engine.systemPrompt(), "# probe", "model switch must rebuild the prompt")
}

// agent_* tools and the sub-agent concurrency cap reach the system prompt via
// the core builder, which is what the AssembleContext fields exist to carry.
func TestJobsReachSystemPrompt(t *testing.T) {
	mgr, err := job.New(job.Options{
		Root: t.TempDir(),
		Runner: job.RunnerFunc(func(context.Context, job.RunEnv) (string, error) {
			return "ok", nil
		}),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = mgr.Close(t.Context()) })

	engine := newPromptEngine(t, extension.Nop, probeModel(t), WithJobs(mgr))
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
