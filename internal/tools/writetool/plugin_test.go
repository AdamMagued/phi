package writetool_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/phi/internal/tools/writetool"
)

func TestPluginRegistersEditAndWrite(t *testing.T) {
	plugin := writetool.Plugin()
	require.NotNil(t, plugin.API)
	assert.Nil(t, plugin.Close, "edit and write own no resources")

	tools := plugin.API.Tools()
	require.Len(t, tools, 2)

	edit := tools[0]
	assert.Equal(t, "edit", edit.Name)
	assert.False(t, edit.Readable, "edit modifies files and must not run in parallel batches")

	write := tools[1]
	assert.Equal(t, "write", write.Name)
	assert.False(t, write.Readable, "write modifies files and must not run in parallel batches")

	// Required args must survive the ext.Tool → tools.Tool conversion the host
	// applies when merging plugin tools into the registry.
	assert.Equal(t, []string{"path", "hash", "edits"}, edit.Parameters["required"])
	assert.Equal(t, "object", edit.Parameters["type"])
	assert.Equal(t, []string{"path", "content"}, write.Parameters["required"])
	assert.Equal(t, "object", write.Parameters["type"])
}
