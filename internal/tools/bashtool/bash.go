package bashtool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	ext "github.com/pulseaiclub/phi/ext/go"
	"github.com/pulseaiclub/phi/internal/extension"
)

const (
	bashDefaultTimeout = 300
)

var bashDescription = `Run a shell command and return combined stdout/stderr.

Use for build, test, git, and OS tasks that read/ls/find/grep/edit/write cannot
do. Do not use for cat, head, tail, ls(1), find(1), grep, or rg — those have dedicated
tools. Large output is truncated with the retained output written to a temp file.`

// Tool returns the bash tool definition in the ext API shape. It is the
// single source of truth: the plugin bus serves it to main engines, and
// sub-agent ChildSpec lists use it directly.
func Tool() ext.Tool {
	return ext.Tool{
		Name:        "bash",
		Description: bashDescription,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "Shell command to run. Example: go test ./...",
				},
				"timeout": map[string]any{
					"type":        "integer",
					"description": "Timeout in seconds, 1-3600. Example: 120 (default: 300).",
				},
			},
			"required": []string{"command"},
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in bashInput
			_ = json.Unmarshal(input, &in)
			return strings.TrimSpace(in.Command)
		},
		Execute: runBash,
	}
}

// Plugin adapts the bash tool to the built-in plugin bus. Bash owns no
// resources, so the plugin carries no Close.
func Plugin() extension.Plugin {
	return extension.ToolPlugin(Tool())
}

type bashInput struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

func runBash(ctx context.Context, input json.RawMessage) (ext.ToolResult, error) {
	var in bashInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ext.ToolResult{}, fmt.Errorf("failed to parse bash arguments: %w", err)
	}
	cmd := strings.TrimSpace(in.Command)
	if cmd == "" {
		return ext.ToolResult{}, errors.New("empty command")
	}

	timeout := in.Timeout
	if timeout <= 0 {
		timeout = bashDefaultTimeout
	}
	if timeout > 3600 {
		timeout = 3600
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	c, err := buildShellCommand(ctx, cmd)
	if err != nil {
		return ext.ToolResult{}, err
	}
	// Bound the shared stdout/stderr collector so runaway output cannot be
	// buffered unboundedly.
	cb := newCappedBuffer(BashMaxCollectBytes)
	c.Stdout = cb
	c.Stderr = cb
	err = c.Run()

	out := formatBashOutput(cb.String(), cb.Truncated())
	if strings.TrimSpace(out) == "" {
		out = "(no output)"
	}

	content := out
	if err != nil {
		if ctx.Err() != nil {
			content = out + "\n(command canceled or timed out)"
		} else {
			content = fmt.Sprintf("%s\n(exit error: %v)", out, err)
		}
	}
	return ext.ToolResult{Content: content, Detail: cmd, Output: content}, nil
}
