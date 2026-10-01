package lstool_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/phi/internal/tools/lstool"
)

func TestPluginRegistersLsTool(t *testing.T) {
	plugin := lstool.Plugin()
	require.NotNil(t, plugin.API)
	assert.Nil(t, plugin.Close, "ls owns no resources")

	tools := plugin.API.Tools()
	require.Len(t, tools, 1)

	ls := tools[0]
	assert.Equal(t, "ls", ls.Name)
	assert.True(t, ls.Readable, "ls is side-effect-free and may run in parallel batches")
	require.NotNil(t, ls.DetailFromArgs)
	assert.Equal(t, "src", ls.DetailFromArgs(json.RawMessage(`{"path":"src"}`)))

	// Required args must survive the ext.Tool → tools.Tool conversion the host
	// applies when merging plugin tools into the registry.
	assert.Equal(t, []string{"path"}, ls.Parameters["required"])
	assert.Equal(t, "object", ls.Parameters["type"])
}
