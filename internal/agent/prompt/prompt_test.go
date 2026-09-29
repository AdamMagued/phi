package prompt

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildAgentsEnabledToggle(t *testing.T) {
	with := Build("", true, 4, nil, nil)
	without := Build("", false, 0, nil, nil)

	require.Contains(t, with, "agent_spawn")
	require.Contains(t, with, "Sub-agents:")
	require.Contains(t, with, "At most 4 sub-agents run concurrently")
	require.NotContains(t, without, "agent_spawn")
	require.NotContains(t, without, "sub-agents run concurrently")
	require.Contains(t, without, "`find` / `grep` / `ls` yourself")
}

func TestBuildEditHashCopyIsUnambiguous(t *testing.T) {
	got := Build("", false, 0, nil, nil)
	require.NotContains(t, got, "copy `@file path#TAG` into")
	require.Contains(t, got, "4 hex chars after `#`")
	require.NotContains(t, got, "Known path or exact symbol")
	require.NotContains(t, got, "creates a new file only")
	require.NotContains(t, got, "fails if it already exists")
	require.Contains(t, got, "`write` creates or overwrites")
	require.Contains(t, got, "Prefer cwd-relative paths")
}

func TestBuildMCPCatalog(t *testing.T) {
	none := Build("", false, 0, nil, nil)
	require.NotContains(t, none, "# MCP")
	require.NotContains(t, none, "External docs/URLs")
	got := Build("", false, 0, []string{"browsermcp", "github"}, nil)
	require.Contains(t, got, "# MCP")
	require.Contains(t, got, "- browsermcp")
	require.Contains(t, got, "- github")
	require.Contains(t, got, "mcp_list")
	require.Contains(t, got, "mcp_inspect")
	require.Contains(t, got, "mcp_call")
	require.Contains(t, got, "docs/URLs")
	require.NotContains(t, got, `"properties"`)
	require.NotContains(t, got, "inputSchema")
}

func TestBuildToolRoster(t *testing.T) {
	roster := []Tool{
		{Name: "read", Summary: "read one file (known path)"},
		{Name: "bash", Summary: "builds, tests, git, OS tasks"},
		{Name: "ext_tool"},
	}
	got := Build("", false, 0, nil, roster)

	require.Contains(
		t,
		got,
		"# Tools\n- `read` — read one file (known path)\n- `bash` — builds, tests, git, OS tasks\n",
	)
	// A tool without a Summary stays out of the roster.
	require.NotContains(t, got, "ext_tool")
	// Routing names each tool and fences bash off from file work.
	require.Contains(t, got, "file path → `read`; symbol or text → `grep`; directory → `ls`")
	require.Contains(t, got, "not listing, searching, or reading files")

	empty := Build("", false, 0, nil, nil)
	require.Contains(t, empty, "# Tools\n- (none)\n")
}
