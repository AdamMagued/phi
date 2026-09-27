package ext

// Scope is the engine a system prompt is being assembled for. A prompt section
// carries one to gate which engines see it.
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

// PromptSection is a static system-prompt block registered via
// [API.RegisterAssembler]. It is data, not a closure: prompt assembly runs at
// engine construction time with no request context, so a block cannot depend on
// per-call state. Scope gates which engines see it; an empty Scope means every
// scope.
type PromptSection struct {
	// Scope is the engine scope this block applies to. Empty matches all.
	Scope Scope
	// Body is the block text, appended after the core prompt in registration
	// order.
	Body string
}
