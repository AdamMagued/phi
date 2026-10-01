package readtool

import (
	"github.com/pulseaiclub/phi/internal/extension"
)

// Plugin adapts the read tool to the built-in plugin bus. Read owns no
// resources, so the plugin carries no Close.
func Plugin() extension.Plugin {
	return extension.ToolPlugin(Tool())
}
