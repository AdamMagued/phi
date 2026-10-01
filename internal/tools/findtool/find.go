// Package findtool provides the find tool (fd-backed filename search).
package findtool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	ext "github.com/pulseaiclub/phi/ext/go"
	"github.com/pulseaiclub/phi/internal/extension"
	"github.com/pulseaiclub/phi/internal/tools/tooldef"
	"github.com/pulseaiclub/phi/internal/util/filesearch"
)

const defaultFindLimit = 100

var findDescription = fmt.Sprintf(
	`Find files matching a glob pattern and return cwd-relative paths.

Uses fd (respects .gitignore). Use path to restrict the search directory.
Supports * / ** / ? / [abc] / {a,b}. Returns at most %d results.
Prefer this over bash find/ls for filename search.`,
	defaultFindLimit,
)

// Tool returns the find tool definition in the ext API shape. It is the
// single source of truth: the plugin bus serves it to main engines, and
// sub-agent ChildSpec lists use it directly.
func Tool() ext.Tool {
	return ext.Tool{
		Name:        "find",
		Description: findDescription,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Directory to search. Example: ./src",
				},
				"pattern": map[string]any{
					"type":        "string",
					"description": "Glob pattern. Example: **/*.go",
				},
				"limit": map[string]any{
					"type": "integer",
					"description": fmt.Sprintf(
						"Maximum results to return. Example: 50 (default: %d)",
						defaultFindLimit,
					),
				},
			},
			"required": []string{"pattern"},
		},
		Readable: true,
		DetailFromArgs: func(input json.RawMessage) string {
			var in findInput
			_ = json.Unmarshal(input, &in)
			pat := strings.TrimSpace(in.Pattern)
			p := strings.TrimSpace(in.Path)
			if p == "" {
				p = "."
			}
			if pat != "" {
				return fmt.Sprintf("find %q in %s", pat, p)
			}
			return "find"
		},
		Execute: runFind,
	}
}

// Plugin adapts the find tool to the built-in plugin bus. Find owns no
// resources, so the plugin carries no Close.
func Plugin() extension.Plugin {
	return extension.ToolPlugin(Tool())
}

type findInput struct {
	Path    string `json:"path,omitempty"`
	Pattern string `json:"pattern"`
	Limit   int    `json:"limit,omitempty"`
}

func runFind(ctx context.Context, input json.RawMessage) (ext.ToolResult, error) {
	var in findInput
	if err := json.Unmarshal(input, &in); err != nil {
		return ext.ToolResult{}, fmt.Errorf("failed to parse find arguments: %w", err)
	}
	pattern := strings.TrimSpace(in.Pattern)
	if pattern == "" {
		return ext.ToolResult{}, errors.New("pattern is required: provide a glob such as *.go or **/*.md")
	}

	searchPath := strings.TrimSpace(in.Path)
	if searchPath == "" {
		searchPath = "."
	}
	absPath, err := tooldef.ResolveToCwd(ctx, searchPath)
	if err != nil {
		return ext.ToolResult{}, err
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return ext.ToolResult{}, fmt.Errorf("path not found: %s. Provide an existing directory", absPath)
	}
	if !info.IsDir() {
		return ext.ToolResult{}, fmt.Errorf("path is not a directory: %s (find expects a directory path)", absPath)
	}

	limit := in.Limit
	if limit <= 0 {
		limit = defaultFindLimit
	}

	files, truncated, err := runFD(ctx, pattern, absPath, limit)
	if err != nil {
		return ext.ToolResult{}, err
	}

	for i, p := range files {
		files[i] = tooldef.RelToCwd(ctx, p)
	}
	content := renderFindResult(files, truncated, limit)
	return ext.ToolResult{
		Content: content,
		Detail:  fmt.Sprintf("%d files", len(files)),
		Output:  content,
	}, nil
}

func runFD(ctx context.Context, pattern, absSearchRoot string, limit int) ([]string, bool, error) {
	bin, err := filesearch.ResolveFD()
	if err != nil {
		return nil, false, err
	}

	args := []string{
		"--glob",
		"--color=never",
		"--hidden",
		"--no-require-git",
		"--type", "f",
		"--ignore-case",
		"--max-results", strconv.Itoa(limit),
	}

	// fd --glob matches the basename unless --full-path is set; path-containing
	// patterns need a leading **/ so they match under the search root.
	effective := pattern
	if strings.Contains(pattern, "/") {
		args = append(args, "--full-path")
		if !strings.HasPrefix(pattern, "/") && !strings.HasPrefix(pattern, "**/") && pattern != "**" {
			effective = "**/" + pattern
		}
	}
	args = append(args, "--", effective, absSearchRoot)

	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()

	out := strings.TrimSpace(stdout.String())
	errMsg := strings.TrimSpace(stderr.String())

	if err != nil {
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok && exitErr.ExitCode() == 1 && out == "" {
			if isFDGlobParseError(errMsg) {
				return nil, false, fmt.Errorf("invalid glob pattern %q: %s", pattern, errMsg)
			}
			// fd exits 1 when there are no matches.
			return nil, false, nil
		}
		if out == "" {
			if errMsg == "" {
				errMsg = err.Error()
			}
			if isFDGlobParseError(errMsg) {
				return nil, false, fmt.Errorf("invalid glob pattern %q: %s", pattern, errMsg)
			}
			return nil, false, fmt.Errorf("fd: %s", errMsg)
		}
		// Partial stdout with non-zero exit: keep what we got.
	}

	if out == "" {
		return nil, false, nil
	}

	lines := strings.Split(out, "\n")
	files := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" {
			continue
		}
		abs := line
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(absSearchRoot, abs)
		}
		files = append(files, filepath.Clean(abs))
		if len(files) >= limit {
			break
		}
	}
	truncated := len(files) >= limit
	return files, truncated, nil
}

func isFDGlobParseError(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "error parsing glob") ||
		strings.Contains(lower, "invalid glob")
}

func renderFindResult(files []string, truncated bool, limit int) string {
	if len(files) == 0 {
		return "No files found"
	}
	result := strings.Join(files, "\n")
	if truncated {
		result += fmt.Sprintf(
			"\n(%d results limit reached. Use limit=%d for more, or refine pattern/path.)",
			limit,
			limit*2,
		)
	}
	return result
}
