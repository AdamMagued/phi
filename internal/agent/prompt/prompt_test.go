package prompt

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// build joins the blocks the way the engine does.
func build(skillPath string, agentsEnabled bool, maxConcurrent int) string {
	return strings.Join(Sections(skillPath, agentsEnabled, maxConcurrent), "\n\n")
}

func TestBuildAgentsEnabledToggle(t *testing.T) {
	with := build("", true, 4)
	without := build("", false, 0)

	require.Contains(t, with, "agent_spawn")
	require.Contains(t, with, "Sub-agents:")
	require.Contains(t, with, "At most 4 sub-agents run concurrently")
	require.NotContains(t, without, "agent_spawn")
	require.NotContains(t, without, "sub-agents run concurrently")
	require.Contains(t, without, "`find` / `grep` / `ls` yourself")
}

func TestBuildEditHashCopyIsUnambiguous(t *testing.T) {
	got := build("", false, 0)
	require.NotContains(t, got, "copy `@file path#TAG` into")
	require.Contains(t, got, "4 hex chars after `#`")
	require.NotContains(t, got, "Known path or exact symbol")
	require.NotContains(t, got, "creates a new file only")
	require.NotContains(t, got, "fails if it already exists")
	require.Contains(t, got, "`write` creates or overwrites")
	require.Contains(t, got, "Prefer cwd-relative paths")
}

// The base prompt is the first block, so callers can prepend nothing and still
// get a coherent prompt when no catalog applies.
func TestSectionsBasePromptComesFirst(t *testing.T) {
	parts := Sections("", false, 0)

	require.NotEmpty(t, parts)
	require.Contains(t, parts[0], "`write` creates or overwrites")
}
