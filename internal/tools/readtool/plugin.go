package readtool

import (
	ext "github.com/pulseaiclub/phi/ext/go"
	"github.com/pulseaiclub/phi/internal/extension"
)

// Plugin adapts the read tool to the built-in plugin bus, mirroring mcp.Plugin.
// Read owns no resources, so the plugin carries no Close.
func Plugin() extension.Plugin {
	api := ext.NewAPI()
	api.RegisterTool(Tool())
	return extension.Plugin{API: api}
}
