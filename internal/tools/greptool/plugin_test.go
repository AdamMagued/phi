package greptool_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/phi/internal/tools/greptool"
)

func TestPluginRegistersGrepTool(t *testing.T) {
	plugin := greptool.Plugin()
	require.NotNil(t, plugin.API)
	assert.Nil(t, plugin.Close, "grep owns no resources")

	tools := plugin.API.Tools()
	require.Len(t, tools, 1)

	grep := tools[0]
	assert.Equal(t, "grep", grep.Name)
	assert.True(t, grep.Readable, "grep is side-effect-free and may run in parallel batches")
	require.NotNil(t, grep.DetailFromArgs)
	assert.Equal(t, `grep "func Test" in .`, grep.DetailFromArgs(json.RawMessage(`{"pattern":"func Test"}`)))

	// Required args must survive the ext.Tool → tools.Tool conversion the host
	// applies when merging plugin tools into the registry.
	assert.Equal(t, []string{"pattern"}, grep.Parameters["required"])
	assert.Equal(t, "object", grep.Parameters["type"])
}
