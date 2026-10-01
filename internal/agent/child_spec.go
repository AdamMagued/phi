package agent

import (
	"github.com/pulseaiclub/phi/internal/job"
	"github.com/pulseaiclub/phi/internal/permission"
	"github.com/pulseaiclub/phi/internal/tools"
	"github.com/pulseaiclub/phi/internal/tools/bashtool"
	"github.com/pulseaiclub/phi/internal/tools/findtool"
	"github.com/pulseaiclub/phi/internal/tools/greptool"
	"github.com/pulseaiclub/phi/internal/tools/lstool"
	"github.com/pulseaiclub/phi/internal/tools/readtool"
	"github.com/pulseaiclub/phi/internal/tools/writetool"
)

// ChildSpec is the capability profile for a sub-agent role.
type ChildSpec struct {
	Role  job.Role
	Tools []tools.Tool
	Mode  permission.Mode // used when EngineRunner.Gate is nil
	Hint  string          // appended to the child prompt
}

// ChildTools returns the default (explore) tool set.
func ChildTools() []tools.Tool {
	return SpecForRole(job.RoleExplore).Tools
}

// readonlyPlugins returns the read-only plugin tool set for child profiles.
// Sub-agents register no plugin tools, so these — implemented as built-in
// plugins like mcp — have to join the explicit list here.
func readonlyPlugins() []tools.Tool {
	return []tools.Tool{
		readtool.Tool(),
		lstool.Tool(),
		findtool.Tool(),
		bashtool.Tool(),
		greptool.Tool(),
	}
}

// writablePlugins returns all plugin tools including edit and write.
func writablePlugins() []tools.Tool {
	return append(readonlyPlugins(),
		writetool.EditTool(),
		writetool.WriteTool(),
	)
}

// SpecForRole returns tools, permission mode, and closing hint for a role.
func SpecForRole(role job.Role) ChildSpec {
	role = job.NormalizeRole(string(role))
	switch role {
	case job.RoleWorker:
		return ChildSpec{
			Role:  job.RoleWorker,
			Tools: writablePlugins(), // no agent_*; writable
			Mode:  permission.ModeHeadlessStrict,
			Hint:  workerSummaryHint,
		}
	case job.RoleReview:
		return ChildSpec{
			Role:  job.RoleReview,
			Tools: readonlyPlugins(),
			Mode:  permission.ModeReadonly,
			Hint:  reviewSummaryHint,
		}
	default:
		return ChildSpec{
			Role:  job.RoleExplore,
			Tools: readonlyPlugins(),
			Mode:  permission.ModeReadonly,
			Hint:  exploreSummaryHint,
		}
	}
}

const exploreSummaryHint = `You are an explore sub-agent (no file edits). Recon the codebase and return dense findings the parent can act on without re-reading everything.

Notes:
1. Be direct. Prefer cwd-relative paths. Use bash freely for inspection (git, builds, tests, pipelines) — hard-denied destructive commands still fail.
2. Structure the final reply roughly as: Files (paths + why), Key findings (types/APIs/snippets only when useful), Start here (what the parent should do next).
3. The parent sees only this final reply — not your tool transcript.
4. You cannot modify files; if edits are needed, say what should change and where.`

const reviewSummaryHint = `You are a review sub-agent (no file edits). Inspect diffs, run checks, and report findings — do not implement fixes.

Notes:
1. Prefer cwd-relative paths. Cite evidence (commands, failing tests, hunks). Prefer git diff/log/show and targeted tests, but bash is available beyond that.
2. Separate must-fix issues from nits. Do not edit files; recommend concrete changes for the parent.
3. Structure roughly as: Files reviewed, Critical, Warnings, Suggestions, Summary.
4. The parent sees only this final reply — not your tool transcript.`

const workerSummaryHint = `You are a worker sub-agent. Implement the assigned scoped task, verify with bash/tests when useful, then finish with one concise final reply.

Notes:
1. Stay within the assigned scope; do not expand into unrelated refactors.
2. Prefer cwd-relative paths. Summarize what you changed and how you verified (Completed / Files changed / Verification).
3. The parent sees only this final reply — not your tool transcript.
4. You cannot spawn further agents.`
