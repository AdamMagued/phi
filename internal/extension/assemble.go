package extension

import (
	"strings"

	ext "github.com/pulseaiclub/phi/ext/go"
)

// AssembleContext describes the prompt being built. The fields mirror what the
// core prompt depends on, so the core builder can branch without asking the
// engine again.
type AssembleContext struct {
	// Scope is the engine the prompt is for.
	Scope ext.Scope
	// SkillPath is the skills directory the core prompt catalogs.
	SkillPath string
	// AgentsEnabled mirrors whether agent_* tools are registered.
	AgentsEnabled bool
	// MaxConcurrent is the sub-agent concurrency cap (0 when agents are off).
	MaxConcurrent int
}

// renderSection returns the trimmed body, or "" if the section is empty or out
// of scope. Rendering stays host-side: the SDK exposes the data, not how the
// host lays it into the prompt.
func renderSection(s ext.PromptSection, scope ext.Scope) string {
	if s.Scope != "" && s.Scope != scope {
		return ""
	}
	return strings.TrimSpace(s.Body)
}
