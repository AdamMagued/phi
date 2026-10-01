package findtool_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/phi/internal/tools/findtool"
)

func TestPluginRegistersFindTool(t *testing.T) {
	plugin := findtool.Plugin()
	require.NotNil(t, plugin.API)
	assert.Nil(t, plugin.Close, "find owns no resources")

	tools := plugin.API.Tools()
	require.Len(t, tools, 1)

	find := tools[0]
	assert.Equal(t, "find", find.Name)
	assert.True(t, find.Readable, "find is side-effect-free and may run in parallel batches")
	require.NotNil(t, find.DetailFromArgs)
	assert.Equal(t, `find "*.go" in .`, find.DetailFromArgs(json.RawMessage(`{"pattern":"*.go"}`)))

	// Required args must survive the ext.Tool → tools.Tool conversion the host
	// applies when merging plugin tools into the registry.
	assert.Equal(t, []string{"pattern"}, find.Parameters["required"])
	assert.Equal(t, "object", find.Parameters["type"])
}
