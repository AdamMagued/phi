package extension

import "strings"

// Scope is the engine a system prompt is being assembled for.
type Scope string

const (
	// ScopeMain is the interactive agent: every registered tool is available.
	ScopeMain Scope = "main"
	// ScopeSubagent is a job child. It dispatches extension events but registers
	// no extension or plugin tools, so a block announcing those tools must be
	// skipped: sending the model after a tool it cannot call is worse than
	// saying nothing at all.
	ScopeSubagent Scope = "subagent"
)

// AssembleContext describes the prompt being built. The fields mirror what the
// core prompt depends on, so an assembler can branch on the scope it runs in
// without asking the host again.
type AssembleContext struct {
	// Scope is the engine the prompt is for.
	Scope Scope
	// SkillPath is the skills directory the core prompt catalogs.
	SkillPath string
	// AgentsEnabled mirrors whether agent_* tools are registered.
	AgentsEnabled bool
	// MaxConcurrent is the sub-agent concurrency cap (0 when agents are off).
	MaxConcurrent int
}

// PromptAssembler returns system-prompt sections to append after the core
// blocks, in registration order. Sections are joined with a blank line between
// them to form the final prompt.
//
// An assembler may branch on the context — notably Scope: a sub-agent runs
// without extension tools, so a block announcing them must be skipped, or the
// model goes after tools it cannot call. Return nil for no contribution.
//
// Prompt assembly runs while an engine is being (re)built — construction, model
// switch, sub-agent setup — where there is no error channel and no request
// context to thread. An assembler that cannot build its block should log and
// return nil.
type PromptAssembler func(ac AssembleContext) []string

// AppendSections is the assembler for the common case: add static blocks after
// the core prompt. Blank blocks are dropped and the rest trimmed, so the
// blank-line join never produces ragged gaps.
func AppendSections(sections ...string) PromptAssembler {
	kept := make([]string, 0, len(sections))
	for _, section := range sections {
		if section = strings.TrimSpace(section); section != "" {
			kept = append(kept, section)
		}
	}
	return func(AssembleContext) []string { return kept }
}
