package extension

import ext "github.com/pulseaiclub/phi/ext/go"

// Plugin is a built-in, in-process extension: an [ext.API] plus the host-side
// extras that only code linked into the host binary can provide.
//
// Subprocess (PXB) extensions reach the same dispatch path through
// Proc.BuildAPI, but they cannot contribute to the system prompt, and the host
// reaps them by killing the process rather than calling Close.
//
// The zero Plugin is ignored by [Runner.AddPlugin], so a loader can build one
// unconditionally from a possibly-nil pool.
type Plugin struct {
	// API carries the tools, commands and prompt sections the plugin registers
	// via [ext.API.RegisterTool], [ext.API.RegisterCommand] and
	// [ext.API.RegisterPromptSection]. Optional: a plugin with none is inert.
	API *ext.API
	// Close releases plugin-owned resources. [Runner.Close] calls it after the
	// subprocesses are gone. Nil means the plugin owns nothing.
	Close func() error
}

// ToolPlugin builds a built-in plugin that registers tools on a fresh API.
// Stateless tool plugins are pure registration; plugins that own resources
// (e.g. the MCP pool) hand-roll Plugin and set Close themselves.
func ToolPlugin(defs ...ext.Tool) Plugin {
	api := ext.NewAPI()
	for _, def := range defs {
		api.RegisterTool(def)
	}
	return Plugin{API: api}
}
