package extension

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	ext "github.com/pulseaiclub/phi/ext/go"
	"github.com/pulseaiclub/phi/ext/go/pxb"
	"github.com/pulseaiclub/phi/internal/debuglog"
	"github.com/pulseaiclub/phi/internal/tools"
)

// Runner owns the external PXB extension subprocesses. It implements [Host] by
// forwarding to its dispatch bus; only what is genuinely process-specific lives
// here: spawn bookkeeping, command RPC over the wire, and shutdown.
//
// Forwarded calls resolve the bus through host(), which falls back to [Nop] for
// a nil Runner, so every Host method stays safe to call on nil.
type Runner struct {
	bus *bus

	mu     sync.Mutex
	procs  []*Proc
	loaded []Discovered
	warns  []Warning
}

var _ Host = (*Runner)(nil)

// NewRunner returns an empty Runner: no subprocess extensions, no plugins. It
// is what discovery falls back to when scanning fails, so that built-in
// plugins (MCP) still work for the rest of the session.
func NewRunner() *Runner {
	return &Runner{bus: &bus{}}
}

// host resolves the dispatch bus, falling back to Nop when the Runner (or its
// bus) is nil so that forwarded calls cannot panic.
func (r *Runner) host() Host {
	if r == nil || r.bus == nil {
		return Nop
	}
	return r.bus
}

// Close shuts down every extension subprocess.
func (r *Runner) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	procs := append([]*Proc(nil), r.procs...)
	r.procs = nil
	r.mu.Unlock()
	for _, p := range procs {
		_ = p.Close()
	}
	if r.bus != nil {
		r.bus.closePlugins()
	}
}

// AddPlugin registers a built-in plugin. The zero Plugin is ignored, so callers
// can build one from a possibly-nil dependency without branching.
func (r *Runner) AddPlugin(p Plugin) {
	if r == nil || r.bus == nil {
		return
	}
	r.bus.addPlugin(p)
}

// Loaded returns discovered extensions that were loaded.
func (r *Runner) Loaded() []Discovered {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Discovered, len(r.loaded))
	copy(out, r.loaded)
	return out
}

// Warnings returns non-fatal load issues.
func (r *Runner) Warnings() []Warning {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Warning, len(r.warns))
	copy(out, r.warns)
	return out
}

// Bind attaches host capabilities to every extension API and forwards
// spontaneous Notify frames / host requests from the extension processes.
func (r *Runner) Bind(opts ext.HostOpts) {
	if r == nil || r.bus == nil {
		return
	}
	opts = r.bus.bind(opts)
	ui := opts.UI
	sendUser := opts.SendUserMessage

	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.procs {
		proc := p
		p.onNotify = func(n pxb.NotifyMsg) {
			if ui == nil {
				return
			}
			if n.Message != "" {
				kind := n.Level
				if kind == "" {
					kind = "info"
				}
				ui.Notify(n.Message, kind)
			}
			if n.StatusSet {
				ui.SetStatus("", n.Status)
			}
		}
		p.onHostRequest = func(id uint32, hasID bool, req pxb.HostRequest) {
			r.handleHostRequest(proc, id, hasID, req, ui, sendUser)
		}
	}
}

func (*Runner) handleHostRequest(
	p *Proc,
	id uint32,
	hasID bool,
	req pxb.HostRequest,
	ui ext.UI,
	sendUser func(string),
) {
	switch req.Method {
	case "send_user_message":
		if sendUser != nil && req.Arg != "" {
			sendUser(req.Arg)
		}
		if hasID {
			p.ReplyHost(id, pxb.HostResult{OK: true})
		}
	case "confirm":
		go func() {
			reply := ext.ConfirmReply{}
			if ui != nil {
				var cr ext.ConfirmRequest
				if req.Arg != "" {
					_ = json.Unmarshal([]byte(req.Arg), &cr)
				}
				if cr.Title == "" && cr.Message == "" {
					cr.Message = req.Arg
				}
				reply = ui.ConfirmOpts(cr)
			}
			if hasID {
				p.ReplyHost(id, pxb.HostResult{OK: reply.OK})
			}
		}()
	default:
		debuglog.Logf("extension: unknown host request %q", req.Method)
		if hasID {
			p.ReplyHost(id, pxb.HostResult{OK: false, Error: "unknown method"})
		}
	}
}

// SetMeta updates cwd/session on bound APIs and pushes to subprocesses.
func (r *Runner) SetMeta(sessionID, cwd string) {
	if r == nil {
		return
	}
	r.host().SetMeta(sessionID, cwd)
	r.mu.Lock()
	procs := append([]*Proc(nil), r.procs...)
	r.mu.Unlock()
	for _, p := range procs {
		p.PushSessionMeta(sessionID, cwd)
	}
}

// RunCommand invokes a registered slash command: subprocess extensions first,
// then in-process ones.
func (r *Runner) RunCommand(name, args string) (CommandOutcome, error) {
	if r == nil {
		return CommandOutcome{}, errors.New("extension: no runner")
	}
	if out, ok, err := r.runProcCommand(name, args); ok {
		return out, err
	}
	return r.host().RunCommand(name, args)
}

// runProcCommand dispatches name to a subprocess extension. ok=false means no
// subprocess registered that command, so the caller falls back to the bus.
func (r *Runner) runProcCommand(name, args string) (CommandOutcome, bool, error) {
	r.mu.Lock()
	procs := append([]*Proc(nil), r.procs...)
	r.mu.Unlock()

	for _, p := range procs {
		for _, c := range p.cmds {
			if c.Name != name {
				continue
			}
			resp, err := p.CallCommand(context.Background(), name, args)
			if err != nil {
				return CommandOutcome{}, true, err
			}
			if !resp.OK {
				if resp.Error != "" {
					return CommandOutcome{}, true, errors.New(resp.Error)
				}
				return CommandOutcome{}, true, fmt.Errorf("extension command %q failed", name)
			}
			return CommandOutcome{Submit: resp.Submit}, true, nil
		}
	}
	return CommandOutcome{}, false, nil
}

// The remaining [Host] methods are pure forwarding; behavior lives in bus.go.

func (r *Runner) SetBaseTools(base []tools.Tool) { r.host().SetBaseTools(base) }

func (r *Runner) ExtensionTools() []tools.Tool { return r.host().ExtensionTools() }

func (r *Runner) CommandEntries() []ext.CommandEntry { return r.host().CommandEntries() }

// AssemblePrompt forwards to the in-process bus: prompt assemblers are built-in
// plugins only, so a Runner with no bus contributes the core prompt alone.
func (r *Runner) AssemblePrompt(ac AssembleContext, core func() []string) []string {
	if r == nil || r.bus == nil {
		return core()
	}
	return r.bus.AssemblePrompt(ac, core)
}

func (r *Runner) PreTool(
	ctx context.Context,
	toolName, toolCallID string,
	input json.RawMessage,
) (json.RawMessage, bool, string, string) {
	return r.host().PreTool(ctx, toolName, toolCallID, input)
}

func (r *Runner) PostTool(
	ctx context.Context,
	toolName, toolCallID string,
	input json.RawMessage,
	content string,
	isError bool,
	errText string,
) (string, string, bool, string) {
	return r.host().PostTool(ctx, toolName, toolCallID, input, content, isError, errText)
}

func (r *Runner) EmitAgentStart()       { r.host().EmitAgentStart() }
func (r *Runner) EmitAgentEnd()         { r.host().EmitAgentEnd() }
func (r *Runner) EmitTurnStart(idx int) { r.host().EmitTurnStart(idx) }
func (r *Runner) EmitTurnEnd(idx int)   { r.host().EmitTurnEnd(idx) }

func (r *Runner) EmitToolExecutionStart(toolName, id string, args json.RawMessage) {
	r.host().EmitToolExecutionStart(toolName, id, args)
}

func (r *Runner) EmitToolExecutionEnd(toolName, id string, isError bool) {
	r.host().EmitToolExecutionEnd(toolName, id, isError)
}

func (r *Runner) EmitSessionCompact(reason string) { r.host().EmitSessionCompact(reason) }

func (r *Runner) EmitTurnStopping(idx int) (bool, string) { return r.host().EmitTurnStopping(idx) }

func (r *Runner) EmitUserInput(text string) (string, bool) { return r.host().EmitUserInput(text) }

func (r *Runner) EmitBeforeAgentStart(prompt string) (string, string) {
	return r.host().EmitBeforeAgentStart(prompt)
}

func (r *Runner) EmitSessionStart(ev ext.SessionStartEvent) ext.SessionEffects {
	return r.host().EmitSessionStart(ev)
}

func (r *Runner) EmitSessionShutdown(ev ext.SessionShutdownEvent) ext.SessionEffects {
	return r.host().EmitSessionShutdown(ev)
}

func (r *Runner) EmitSessionBeforeSwitch(ev ext.SessionBeforeSwitchEvent) ext.SessionEffects {
	return r.host().EmitSessionBeforeSwitch(ev)
}
