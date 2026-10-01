package extension

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"

	ext "github.com/pulseaiclub/phi/ext/go"
	"github.com/pulseaiclub/phi/internal/debuglog"
	"github.com/pulseaiclub/phi/internal/llm"
	"github.com/pulseaiclub/phi/internal/tools"
)

const maxContextBytes = 4 * 1024

// bus is the in-process dispatch core: it fans host calls out to every
// registered [ext.API] and aggregates their registrations. Where an API came
// from is invisible here — subprocess registrations are replayed onto a
// host-side API by Proc.BuildAPI, so a built-in plugin only has to add an API.
// Process lifecycle (spawn, command RPC, shutdown) lives in Runner, which owns
// a bus.
type bus struct {
	mu      sync.Mutex
	apis    []*ext.API
	ui      ext.UI
	cwd     string
	session string
	hasUI   bool

	baseTools   []tools.Tool
	activeNames map[string]bool // nil = all active
	host        ext.HostOpts

	plugins []Plugin          // built-in plugins: Close lifecycle
	builtin map[*ext.API]bool // APIs from addPlugin, not from a subprocess
}

var _ Host = (*bus)(nil)

// addAPI registers one extension API. Callers append the matching Proc when the
// API came from a subprocess.
func (b *bus) addAPI(api *ext.API) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.apis = append(b.apis, api)
}

// addPlugin registers a built-in plugin. Its API joins the same dispatch list as
// subprocess extensions, so tools, commands and events take the identical path;
// the host-side extras stay here. The zero Plugin is ignored.
func (b *bus) addPlugin(p Plugin) {
	if p.API == nil && p.Close == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if p.API != nil {
		if b.builtin == nil {
			b.builtin = make(map[*ext.API]bool)
		}
		b.builtin[p.API] = true
		b.apis = append(b.apis, p.API)
	}
	b.plugins = append(b.plugins, p)
}

// AssemblePrompt builds the system prompt: the core blocks followed by each
// API's prompt sections, in registration order. Subprocess extensions cannot
// register prompt sections — the PXB protocol has no call for this — so only
// built-in plugins contribute today.
//
// The API list is snapshotted so rendering never runs under b.mu.
func (b *bus) AssemblePrompt(ac AssembleContext, core func() []string) []string {
	b.mu.Lock()
	apis := append([]*ext.API(nil), b.apis...)
	b.mu.Unlock()

	out := core()
	for _, api := range apis {
		for _, s := range api.Sections() {
			if body := renderSection(s, ac.Scope); body != "" {
				out = append(out, body)
			}
		}
	}
	return out
}

// closePlugins releases resources owned by built-in plugins. Runner.Close calls
// it once; later calls are no-ops.
func (b *bus) closePlugins() {
	b.mu.Lock()
	plugins := b.plugins
	b.plugins = nil
	b.mu.Unlock()

	for _, p := range plugins {
		if p.Close == nil {
			continue
		}
		if err := p.Close(); err != nil {
			debuglog.Logf("extension: plugin close: %v", err)
		}
	}
}

// bind attaches host capabilities to every extension API and returns opts with
// the tool-introspection callbacks filled in.
func (b *bus) bind(opts ext.HostOpts) ext.HostOpts {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ui = opts.UI
	b.cwd = opts.Cwd
	b.session = opts.SessionID
	b.hasUI = opts.HasUI
	if opts.GetActiveTools == nil {
		opts.GetActiveTools = func() []string {
			b.mu.Lock()
			defer b.mu.Unlock()
			return b.getActiveToolsLocked()
		}
	}
	if opts.SetActiveTools == nil {
		opts.SetActiveTools = func(names []string) {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.setActiveToolsLocked(names)
		}
	}
	if opts.GetAllTools == nil {
		opts.GetAllTools = func() []ext.ToolInfo {
			b.mu.Lock()
			defer b.mu.Unlock()
			return b.getAllToolsLocked()
		}
	}
	b.host = opts
	for _, api := range b.apis {
		api.BindHost(opts)
	}
	return opts
}

// SetMeta updates cwd/session on bound APIs.
func (b *bus) SetMeta(sessionID, cwd string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.session = sessionID
	b.cwd = cwd
	b.host.SessionID = sessionID
	b.host.Cwd = cwd
	for _, api := range b.apis {
		api.BindHost(b.host)
	}
}

// SetBaseTools records built-in tools for GetAllTools / active filtering.
func (b *bus) SetBaseTools(base []tools.Tool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.baseTools = base
}

func (b *bus) getActiveToolsLocked() []string {
	all := b.allToolNamesLocked()
	if b.activeNames == nil {
		return all
	}
	var out []string
	for _, n := range all {
		if b.activeNames[n] {
			out = append(out, n)
		}
	}
	return out
}

func (b *bus) setActiveToolsLocked(names []string) {
	b.activeNames = make(map[string]bool, len(names))
	for _, n := range names {
		b.activeNames[n] = true
	}
}

func (b *bus) getAllToolsLocked() []ext.ToolInfo {
	var out []ext.ToolInfo
	for _, t := range b.baseTools {
		out = append(out, ext.ToolInfo{
			Name:        t.Definition.Name,
			Description: t.Definition.Description,
			Source:      "builtin",
		})
	}
	for _, api := range b.apis {
		source := "extension"
		if b.builtin[api] {
			source = "builtin"
		}
		for _, t := range api.Tools() {
			out = append(out, ext.ToolInfo{
				Name:        t.Name,
				Description: t.Description,
				Source:      source,
			})
		}
	}
	return out
}

func (b *bus) allToolNamesLocked() []string {
	infos := b.getAllToolsLocked()
	out := make([]string, len(infos))
	for i, info := range infos {
		out[i] = info.Name
	}
	return out
}

// ExtensionTools converts registered extension tools to tools.Tool.
func (b *bus) ExtensionTools() []tools.Tool {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []tools.Tool
	for _, api := range b.apis {
		for _, def := range api.Tools() {
			out = append(out, ToolFromDef(def))
		}
	}
	return out
}

// ToolFromDef converts an extension tool definition to the host tools.Tool
// shape. The bus uses it for every plugin tool; consumers that bypass the bus
// (sub-agent ChildSpec lists, which register no plugin tools) call it directly.
func ToolFromDef(def ext.Tool) tools.Tool {
	params := schemaFromMap(def.Parameters)
	exec := def.Execute
	return tools.Tool{
		Definition: llm.ToolDefinition{
			Name:        def.Name,
			Description: def.Description,
			Params:      params,
			Readable:    def.Readable,
		},
		DetailFromArgs: def.DetailFromArgs,
		Run: func(ctx context.Context, input json.RawMessage) (tools.Result, error) {
			res, err := exec(ctx, input)
			if err != nil {
				return tools.Result{}, err
			}
			out := res.Output
			if out == "" {
				out = res.Content
			}
			return tools.Result{Content: res.Content, Detail: res.Detail, Output: out, Expanded: res.Expanded}, nil
		},
	}
}

func schemaFromMap(m map[string]any) *llm.FunctionParameters {
	if m == nil {
		return &llm.FunctionParameters{Type: "object", Properties: llm.Object{}}
	}
	fp := &llm.FunctionParameters{Type: "object", Properties: llm.Object{}}
	if t, ok := m["type"].(string); ok && t != "" {
		fp.Type = t
	}
	if props, ok := m["properties"].(map[string]any); ok {
		fp.Properties = props
	}
	switch req := m["required"].(type) {
	case []string:
		fp.Required = req
	case []any:
		for _, v := range req {
			if s, ok := v.(string); ok {
				fp.Required = append(fp.Required, s)
			}
		}
	}
	return fp
}

// CommandEntries lists slash commands from all extensions.
func (b *bus) CommandEntries() []ext.CommandEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []ext.CommandEntry
	seen := make(map[string]bool)
	for _, api := range b.apis {
		for _, e := range api.CommandEntries() {
			if seen[e.Name] {
				continue
			}
			seen[e.Name] = true
			out = append(out, e)
		}
	}
	return out
}

// CommandOutcome is returned from RunCommand for host UI side effects.
type CommandOutcome struct {
	Submit string
}

// RunCommand invokes a slash command registered in-process.
func (b *bus) RunCommand(name, args string) (CommandOutcome, error) {
	b.mu.Lock()
	apis := append([]*ext.API(nil), b.apis...)
	b.mu.Unlock()

	for _, api := range apis {
		cmds := api.Commands()
		cmd, ok := cmds[name]
		if !ok {
			continue
		}
		return CommandOutcome{}, cmd.Handler(args, api.NewContext())
	}
	return CommandOutcome{}, fmt.Errorf("extension: command %q not found", name)
}

// PreTool runs tool_call handlers serially. First block wins; input rewrites chain.
func (b *bus) PreTool(
	ctx context.Context,
	toolName, toolCallID string,
	input json.RawMessage,
) (json.RawMessage, bool, string, string) {
	ev := ext.ToolCallEvent{ToolName: toolName, ToolCallID: toolCallID, Input: input}
	var contexts []string
	for _, h := range b.handlers(ext.EventToolCall) {
		if ctx.Err() != nil {
			break
		}
		res := callToolCall(h, ev, b.context())
		if res == nil {
			continue
		}
		if len(res.Input) > 0 {
			ev.Input = res.Input
		}
		if res.Context != "" {
			contexts = append(contexts, res.Context)
		}
		if res.Block {
			return ev.Input, true, res.Reason, capContext(strings.Join(contexts, "\n"))
		}
	}
	return ev.Input, false, "", capContext(strings.Join(contexts, "\n"))
}

// PostTool runs tool_result handlers.
func (b *bus) PostTool(
	ctx context.Context,
	toolName, toolCallID string,
	input json.RawMessage,
	content string,
	isError bool,
	errText string,
) (string, string, bool, string) {
	ev := ext.ToolResultEvent{
		ToolName:   toolName,
		ToolCallID: toolCallID,
		Input:      input,
		Content:    content,
		IsError:    isError,
		Err:        errText,
	}
	var (
		contexts []string
		stop     bool
		reason   string
		out      = content
	)
	for _, h := range b.handlers(ext.EventToolResult) {
		if ctx.Err() != nil {
			break
		}
		res := callToolResult(h, ev, b.context())
		if res == nil {
			continue
		}
		if res.Content != "" {
			out = res.Content
			ev.Content = out
		}
		if res.Context != "" {
			contexts = append(contexts, res.Context)
		}
		if res.Stop {
			stop = true
			reason = res.Reason
		}
	}
	return out, capContext(strings.Join(contexts, "\n")), stop, reason
}

// EmitSessionStart notifies session_start handlers.
func (b *bus) EmitSessionStart(ev ext.SessionStartEvent) ext.SessionEffects {
	return b.emitSession(ext.EventSessionStart, ev)
}

// EmitSessionShutdown notifies session_shutdown handlers.
func (b *bus) EmitSessionShutdown(ev ext.SessionShutdownEvent) ext.SessionEffects {
	return b.emitSession(ext.EventSessionShutdown, ev)
}

// EmitSessionBeforeSwitch runs before_switch; cancel if any handler cancels.
func (b *bus) EmitSessionBeforeSwitch(ev ext.SessionBeforeSwitchEvent) ext.SessionEffects {
	var effects ext.SessionEffects
	for _, h := range b.handlers(ext.EventSessionBeforeSwitch) {
		res := callSessionBeforeSwitch(h, ev, b.context())
		if res == nil {
			continue
		}
		if res.Toast != "" {
			effects.Toast = res.Toast
		}
		if res.Cancel {
			effects.Denied = true
			effects.Reason = res.Reason
			return effects
		}
	}
	return effects
}

func (b *bus) emitSession(event string, payload any) ext.SessionEffects {
	var effects ext.SessionEffects
	for _, h := range b.handlers(event) {
		callNotify(h, payload, b.context())
	}
	return effects
}

// EmitAgentStart / EmitAgentEnd / EmitTurn* / EmitToolExecution* are fire-and-forget.
func (b *bus) EmitAgentStart() {
	b.emitNotify(ext.EventAgentStart, ext.AgentStartEvent{})
}

func (b *bus) EmitAgentEnd() {
	b.emitNotify(ext.EventAgentEnd, ext.AgentEndEvent{})
}

func (b *bus) EmitTurnStart(idx int) {
	b.emitNotify(ext.EventTurnStart, ext.TurnStartEvent{TurnIndex: idx})
}

func (b *bus) EmitTurnEnd(idx int) {
	b.emitNotify(ext.EventTurnEnd, ext.TurnEndEvent{TurnIndex: idx})
}

func (b *bus) EmitToolExecutionStart(toolName, id string, args json.RawMessage) {
	b.emitNotify(
		ext.EventToolExecutionStart,
		ext.ToolExecutionStartEvent{ToolName: toolName, ToolCallID: id, Args: args},
	)
}

func (b *bus) EmitToolExecutionEnd(toolName, id string, isError bool) {
	b.emitNotify(
		ext.EventToolExecutionEnd,
		ext.ToolExecutionEndEvent{ToolName: toolName, ToolCallID: id, IsError: isError},
	)
}

// EmitBeforeAgentStart runs before_agent_start; returns prompt rewrite + append text.
func (b *bus) EmitBeforeAgentStart(prompt string) (newPrompt, appendText string) {
	ev := ext.BeforeAgentStartEvent{Prompt: prompt}
	out := prompt
	var appends []string
	for _, h := range b.handlers(ext.EventBeforeAgentStart) {
		res := callBeforeAgentStart(h, ev, b.context())
		if res == nil {
			continue
		}
		if res.Prompt != "" {
			out = res.Prompt
			ev.Prompt = out
		}
		if res.SystemPromptAppend != "" {
			appends = append(appends, res.SystemPromptAppend)
		}
	}
	return out, strings.Join(appends, "\n")
}

// EmitUserInput runs user_input intercepts. handled=true means skip the agent loop.
func (b *bus) EmitUserInput(text string) (out string, handled bool) {
	ev := ext.UserInputEvent{Text: text}
	out = text
	for _, h := range b.handlers(ext.EventUserInput) {
		res := callUserInput(h, ev, b.context())
		if res == nil {
			continue
		}
		if res.Text != "" {
			out = res.Text
			ev.Text = out
		}
		if res.Handled {
			return out, true
		}
	}
	return out, false
}

// EmitTurnStopping asks extensions whether to steer another step.
func (b *bus) EmitTurnStopping(idx int) (continueTurn bool, message string) {
	ev := ext.TurnStoppingEvent{TurnIndex: idx}
	for _, h := range b.handlers(ext.EventTurnStopping) {
		res := callTurnStopping(h, ev, b.context())
		if res == nil {
			continue
		}
		if res.Continue {
			return true, res.Message
		}
	}
	return false, ""
}

// EmitSessionCompact notifies listeners that compaction ran.
func (b *bus) EmitSessionCompact(reason string) {
	b.emitNotify(ext.EventSessionCompact, ext.SessionCompactEvent{Reason: reason})
}

func (b *bus) emitNotify(event string, payload any) {
	for _, h := range b.handlers(event) {
		callNotify(h, payload, b.context())
	}
}

func (b *bus) handlers(event string) []any {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []any
	for _, api := range b.apis {
		out = append(out, api.Handlers(event)...)
	}
	return out
}

func (b *bus) context() *ext.Context {
	b.mu.Lock()
	defer b.mu.Unlock()
	return &ext.Context{Cwd: b.cwd, SessionID: b.session, HasUI: b.hasUI, UI: b.ui}
}

func callToolCall(h any, ev ext.ToolCallEvent, ctx *ext.Context) *ext.ToolCallResult {
	switch fn := h.(type) {
	case func(ext.ToolCallEvent, *ext.Context) *ext.ToolCallResult:
		return fn(ev, ctx)
	case func(ext.ToolCallEvent, *ext.Context) (*ext.ToolCallResult, error):
		res, err := fn(ev, ctx)
		if err != nil {
			debuglog.Logf("extension: tool_call: %v", err)
		}
		return res
	default:
		return callViaReflect[ext.ToolCallResult](h, ev, ctx)
	}
}

func callToolResult(h any, ev ext.ToolResultEvent, ctx *ext.Context) *ext.ToolResultResult {
	switch fn := h.(type) {
	case func(ext.ToolResultEvent, *ext.Context) *ext.ToolResultResult:
		return fn(ev, ctx)
	default:
		return callViaReflect[ext.ToolResultResult](h, ev, ctx)
	}
}

func callSessionBeforeSwitch(h any, ev ext.SessionBeforeSwitchEvent, ctx *ext.Context) *ext.SessionBeforeSwitchResult {
	switch fn := h.(type) {
	case func(ext.SessionBeforeSwitchEvent, *ext.Context) *ext.SessionBeforeSwitchResult:
		return fn(ev, ctx)
	default:
		return callViaReflect[ext.SessionBeforeSwitchResult](h, ev, ctx)
	}
}

func callBeforeAgentStart(h any, ev ext.BeforeAgentStartEvent, ctx *ext.Context) *ext.BeforeAgentStartResult {
	switch fn := h.(type) {
	case func(ext.BeforeAgentStartEvent, *ext.Context) *ext.BeforeAgentStartResult:
		return fn(ev, ctx)
	default:
		return callViaReflect[ext.BeforeAgentStartResult](h, ev, ctx)
	}
}

func callUserInput(h any, ev ext.UserInputEvent, ctx *ext.Context) *ext.UserInputResult {
	switch fn := h.(type) {
	case func(ext.UserInputEvent, *ext.Context) *ext.UserInputResult:
		return fn(ev, ctx)
	default:
		return callViaReflect[ext.UserInputResult](h, ev, ctx)
	}
}

func callTurnStopping(h any, ev ext.TurnStoppingEvent, ctx *ext.Context) *ext.TurnStoppingResult {
	switch fn := h.(type) {
	case func(ext.TurnStoppingEvent, *ext.Context) *ext.TurnStoppingResult:
		return fn(ev, ctx)
	default:
		return callViaReflect[ext.TurnStoppingResult](h, ev, ctx)
	}
}

func callNotify(h, payload any, ctx *ext.Context) {
	v := reflect.ValueOf(h)
	if v.Kind() != reflect.Func {
		return
	}
	t := v.Type()
	if t.NumIn() < 2 {
		return
	}
	args := []reflect.Value{reflect.ValueOf(payload), reflect.ValueOf(ctx)}
	// Coerce payload type if needed.
	if args[0].Type() != t.In(0) && args[0].Type().ConvertibleTo(t.In(0)) {
		args[0] = args[0].Convert(t.In(0))
	}
	if args[0].Type() != t.In(0) {
		// Build zero of expected and try assign from interface.
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			debuglog.Logf("extension: handler panic: %v", rec)
		}
	}()
	v.Call(args)
}

func callViaReflect[T any](h, payload any, ctx *ext.Context) *T {
	v := reflect.ValueOf(h)
	if v.Kind() != reflect.Func {
		return nil
	}
	defer func() {
		if rec := recover(); rec != nil {
			debuglog.Logf("extension: handler panic: %v", rec)
		}
	}()
	outs := v.Call([]reflect.Value{reflect.ValueOf(payload), reflect.ValueOf(ctx)})
	if len(outs) == 0 || !outs[0].IsValid() || outs[0].IsNil() {
		return nil
	}
	if res, ok := reflect.TypeAssert[*T](outs[0]); ok {
		return res
	}
	return nil
}

func capContext(s string) string {
	if len(s) <= maxContextBytes {
		return s
	}
	return s[:maxContextBytes]
}
