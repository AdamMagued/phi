package extension

import (
	"context"
	"encoding/json"
	"errors"

	ext "github.com/pulseaiclub/phi/ext/go"
	"github.com/pulseaiclub/phi/internal/tools"
)

type Host interface {
	EventHandler
	ToolHandler
	CommandHandler
	PromptHandler
}

type EventHandler interface {
	EmitAgentStart()
	EmitAgentEnd()
	EmitTurnStart(idx int)
	EmitTurnEnd(idx int)
	EmitTurnStopping(idx int) (bool, string)
	EmitToolExecutionStart(toolName, id string, args json.RawMessage)
	EmitToolExecutionEnd(toolName, id string, isError bool)
	EmitUserInput(text string) (out string, handled bool)
	EmitBeforeAgentStart(prompt string) (newPrompt, appendText string)
	EmitSessionStart(ev ext.SessionStartEvent) ext.SessionEffects
	EmitSessionShutdown(ev ext.SessionShutdownEvent) ext.SessionEffects
	EmitSessionBeforeSwitch(ev ext.SessionBeforeSwitchEvent) ext.SessionEffects
	EmitSessionCompact(reason string)
}

type ToolHandler interface {
	SetBaseTools([]tools.Tool)
	ExtensionTools() []tools.Tool
	SetMeta(sessionID, cwd string)
	PreTool(
		ctx context.Context,
		toolName, toolCallID string,
		input json.RawMessage,
	) (json.RawMessage, bool, string, string)
	PostTool(
		ctx context.Context,
		toolName, toolCallID string,
		input json.RawMessage,
		content string,
		isError bool,
		errText string,
	) (string, string, bool, string)
}

type CommandHandler interface {
	CommandEntries() []ext.CommandEntry
	RunCommand(name, args string) (CommandOutcome, error)
}

// PromptHandler assembles the agent system prompt from the registered
// assemblers.
type PromptHandler interface {
	// AssemblePrompt returns the core blocks followed by each registered
	// assembler's sections, in registration order.
	AssemblePrompt(ac AssembleContext, core func() []string) []string
}

// OrNop normalizes a Host at the boundary between the loader (which deals in
// *Runner and may legitimately hold nil) and the agent core (which must never
// see a nil Host). It also catches a typed-nil *Runner stored in an interface.
func OrNop(h Host) Host {
	if h == nil {
		return Nop
	}
	if r, ok := h.(*Runner); ok && r == nil {
		return Nop
	}
	return h
}

// Nop is the disabled Host: every method is a no-op, and the payload-carrying
// methods pass their input through unchanged. Callers never need nil checks.
var Nop Host = nopHost{}

type nopHost struct{}

func (nopHost) SetBaseTools([]tools.Tool)    {}
func (nopHost) ExtensionTools() []tools.Tool { return nil }
func (nopHost) SetMeta(_, _ string)          {}

// PreTool echoes input: the executor applies the returned args only when they
// are non-empty, so echoing keeps args identical to having no handler at all.
func (nopHost) PreTool(_ context.Context, _, _ string, input json.RawMessage) (json.RawMessage, bool, string, string) {
	return input, false, "", ""
}

// PostTool echoes content: an empty result is treated as pass-through by the
// executor, but echoing keeps the contract explicit for readers.
func (nopHost) PostTool(
	_ context.Context,
	_, _ string,
	_ json.RawMessage,
	content string,
	_ bool,
	_ string,
) (string, string, bool, string) {
	return content, "", false, ""
}

func (nopHost) CommandEntries() []ext.CommandEntry { return nil }

// AssemblePrompt with no assembler registered is just the core prompt.
func (nopHost) AssemblePrompt(_ AssembleContext, core func() []string) []string {
	return core()
}

func (nopHost) RunCommand(_, _ string) (CommandOutcome, error) {
	return CommandOutcome{}, errors.New("extension: no runner")
}

func (nopHost) EmitAgentStart()                                       {}
func (nopHost) EmitAgentEnd()                                         {}
func (nopHost) EmitTurnStart(int)                                     {}
func (nopHost) EmitTurnEnd(int)                                       {}
func (nopHost) EmitTurnStopping(int) (bool, string)                   { return false, "" }
func (nopHost) EmitToolExecutionStart(_, _ string, _ json.RawMessage) {}
func (nopHost) EmitToolExecutionEnd(_, _ string, _ bool)              {}
func (nopHost) EmitSessionCompact(string)                             {}

// EmitUserInput and EmitBeforeAgentStart must return their argument: the engine
// uses the result as the prompt, so a zero value would discard user input.
func (nopHost) EmitUserInput(text string) (string, bool) { return text, false }

func (nopHost) EmitBeforeAgentStart(prompt string) (string, string) { return prompt, "" }

func (nopHost) EmitSessionStart(ext.SessionStartEvent) ext.SessionEffects {
	return ext.SessionEffects{}
}

func (nopHost) EmitSessionShutdown(ext.SessionShutdownEvent) ext.SessionEffects {
	return ext.SessionEffects{}
}

func (nopHost) EmitSessionBeforeSwitch(ext.SessionBeforeSwitchEvent) ext.SessionEffects {
	return ext.SessionEffects{}
}
