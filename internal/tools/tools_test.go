package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/phi/internal/tools"
)

func TestDefinitionsNarrowSDKSchema(t *testing.T) {
	list := []tools.Tool{
		{
			Name:        "read",
			Description: "read a file",
			Readable:    true,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "file path",
						"enum":        []string{"a.go", "b.go"}, // non-core keywords pass through
					},
				},
				"required": []string{"path"},
			},
		},
		{Name: "bash", Description: "run a command"},
	}

	defs := tools.Definitions(list)
	require.Len(t, defs, 2)

	assert.Equal(t, "read", defs[0].Name)
	assert.Equal(t, "read a file", defs[0].Description)
	assert.True(t, defs[0].Readable)
	require.NotNil(t, defs[0].Params)
	assert.Equal(t, "object", defs[0].Params.Type)
	assert.Equal(t, []string{"path"}, defs[0].Params.Required)
	props, ok := defs[0].Params.Properties["path"].(map[string]any)
	require.True(t, ok, "property keywords must survive verbatim")
	assert.Equal(t, []string{"a.go", "b.go"}, props["enum"])

	// Nil Parameters still yield a valid object schema.
	assert.Equal(t, "object", defs[1].Params.Type)
	assert.False(t, defs[1].Readable)
}

func TestNewRegistryIndexesByName(t *testing.T) {
	reg := tools.NewRegistry([]tools.Tool{
		{
			Name:    "bash",
			Execute: func(context.Context, json.RawMessage) (tools.Result, error) { return tools.Result{}, nil },
		},
		{Name: "grep"},
	})
	require.Len(t, reg, 2)
	assert.Contains(t, reg, "bash")
	assert.Contains(t, reg, "grep")
}
