package readtool_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/phi/internal/tools/readtool"
)

func TestPluginRegistersReadTool(t *testing.T) {
	plugin := readtool.Plugin()
	require.NotNil(t, plugin.API)
	assert.Nil(t, plugin.Close, "read owns no resources")

	tools := plugin.API.Tools()
	require.Len(t, tools, 1)

	read := tools[0]
	assert.Equal(t, "read", read.Name)
	assert.True(t, read.Readable, "read is side-effect-free and may run in parallel batches")
	require.NotNil(t, read.DetailFromArgs)
	assert.Equal(t, "src/main.go", read.DetailFromArgs(json.RawMessage(`{"path":" src/main.go "}`)))

	// Required args must survive the ext.Tool → tools.Tool conversion the host
	// applies when merging plugin tools into the registry.
	assert.Equal(t, []string{"path"}, read.Parameters["required"])
	assert.Equal(t, "object", read.Parameters["type"])
}
