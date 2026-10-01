package writetool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	ext "github.com/pulseaiclub/phi/ext/go"
	"github.com/pulseaiclub/phi/internal/tools/tooldef"
)

var writeDescription = `Write content to a file. Creates the file if it does not exist; overwrites the entire file if it does. Creates parent directories.`

// WriteTool returns the write tool definition in the ext API shape.
func WriteTool() ext.Tool {
	return ext.Tool{
		Name:        "write",
		Description: writeDescription,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "File path to write (created or overwritten). Example: src/new.go",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "Content to write to the file.",
				},
			},
			"required": []string{"path", "content"},
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in writeInput
			_ = json.Unmarshal(input, &in)
			return strings.TrimSpace(in.Path)
		},
		Execute: runWrite,
	}
}

type writeInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func runWrite(ctx context.Context, input json.RawMessage) (ext.ToolResult, error) {
	var in writeInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ext.ToolResult{}, fmt.Errorf("failed to parse write arguments: %w", err)
	}
	path := strings.TrimSpace(in.Path)
	if path == "" {
		return ext.ToolResult{}, errors.New("path is required")
	}
	path, err := tooldef.ResolveToCwd(ctx, path)
	if err != nil {
		return ext.ToolResult{}, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return ext.ToolResult{}, fmt.Errorf("failed to create parent directories: %w", err)
	}

	//nolint:gosec // G306: source files should stay world-readable
	if err := os.WriteFile(path, []byte(in.Content), 0o644); err != nil {
		return ext.ToolResult{}, fmt.Errorf("failed to write file %s: %w", path, err)
	}

	display := tooldef.RelToCwd(ctx, path)
	detail := fmt.Sprintf("wrote %d bytes to %s", len(in.Content), display)
	return ext.ToolResult{Content: detail, Detail: display, Output: detail}, nil
}
