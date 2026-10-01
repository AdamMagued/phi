package bashtool_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/phi/internal/tools/bashtool"
)

func TestPluginRegistersBashTool(t *testing.T) {
	plugin := bashtool.Plugin()
	require.NotNil(t, plugin.API)
	assert.Nil(t, plugin.Close, "bash owns no resources")

	tools := plugin.API.Tools()
	require.Len(t, tools, 1)

	bash := tools[0]
	assert.Equal(t, "bash", bash.Name)
	assert.False(t, bash.Readable, "bash has side effects and must not run in parallel batches")
	require.NotNil(t, bash.DetailFromArgs)
	assert.Equal(t, "go test ./...", bash.DetailFromArgs(json.RawMessage(`{"command":"go test ./..."}`)))

	// Required args must survive the ext.Tool → tools.Tool conversion the host
	// applies when merging plugin tools into the registry.
	assert.Equal(t, []string{"command"}, bash.Parameters["required"])
	assert.Equal(t, "object", bash.Parameters["type"])
}
