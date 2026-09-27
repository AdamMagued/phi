package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	ext "github.com/pulseaiclub/phi/ext/go"
	"github.com/pulseaiclub/phi/internal/extension"
)

// Plugin adapts a loaded pool to the built-in plugin bus. It registers the
// mcp_list / mcp_inspect / mcp_call meta-tools, contributes the prompt block
// that tells the model those tools exist, and owns the pool's lifetime so the
// host app does not have to thread a separate Close through shutdown.
//
// Registering a plugin is what makes MCP visible to the agent: it is not part
// of the core tool set, so an engine built without it (a sub-agent, or a host
// running with MCP disabled) simply has no mcp_* tools.
//
// A nil pool yields the zero plugin, which the bus ignores.
func Plugin(pool *Pool) extension.Plugin {
	if pool == nil {
		return extension.Plugin{}
	}
	api := ext.NewAPI()
	api.RegisterTool(listTool(pool))
	api.RegisterTool(inspectTool(pool))
	api.RegisterTool(callTool(pool))

	api.RegisterAssembler(ext.ScopeMain, promptSection(pool.ServerNames()))

	return extension.Plugin{API: api, Close: pool.Close}
}

func listTool(pool *Pool) ext.Tool {
	return ext.Tool{
		Name: "mcp_list",
		Description: `List MCP tool names on one server (compact text, not full JSON schemas).

Returns space-separated tool names. Schemas never enter the model context — use mcp_inspect for one tool's params.`,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"server": map[string]any{
					"type":        "string",
					"description": "MCP server name",
				},
			},
			"required": []string{"server"},
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in struct {
				Server string `json:"server"`
			}
			_ = json.Unmarshal(input, &in)
			return in.Server
		},
		Execute: func(ctx context.Context, input json.RawMessage) (ext.ToolResult, error) {
			var in struct {
				Server string `json:"server"`
			}
			if err := json.Unmarshal(input, &in); err != nil {
				return ext.ToolResult{}, fmt.Errorf("mcp_list: %w", err)
			}
			if in.Server == "" {
				return ext.ToolResult{}, errors.New("mcp_list: server is required")
			}
			listed, err := pool.ListTools(ctx, in.Server)
			if err != nil {
				return ext.ToolResult{}, err
			}
			body := CompactToolNames(listed)
			return ext.ToolResult{
				Content: body,
				Detail:  fmt.Sprintf("%s: %d tools", in.Server, len(listed)),
				Output:  body,
			}, nil
		},
	}
}

func inspectTool(pool *Pool) ext.Tool {
	return ext.Tool{
		Name: "mcp_inspect",
		Description: `Show a compact parameter summary for one MCP tool (slim text).

Use after mcp_list to learn required args before mcp_call.`,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"server": map[string]any{
					"type":        "string",
					"description": "MCP server name",
				},
				"tool": map[string]any{
					"type":        "string",
					"description": "Tool name on that server",
				},
			},
			"required": []string{"server", "tool"},
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in struct {
				Server string `json:"server"`
				Tool   string `json:"tool"`
			}
			_ = json.Unmarshal(input, &in)
			return in.Server + "/" + in.Tool
		},
		Execute: func(ctx context.Context, input json.RawMessage) (ext.ToolResult, error) {
			var in struct {
				Server string `json:"server"`
				Tool   string `json:"tool"`
			}
			if err := json.Unmarshal(input, &in); err != nil {
				return ext.ToolResult{}, fmt.Errorf("mcp_inspect: %w", err)
			}
			def, err := pool.Inspect(ctx, in.Server, in.Tool)
			if err != nil {
				return ext.ToolResult{}, err
			}
			body := SlimTool(*def)
			return ext.ToolResult{Content: body, Detail: in.Server + "/" + in.Tool, Output: body}, nil
		},
	}
}

func callTool(pool *Pool) ext.Tool {
	return ext.Tool{
		Name: "mcp_call",
		Description: `Call one MCP tool on a configured server.

Prefer mcp_list then mcp_inspect before calling unfamiliar tools.`,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"server": map[string]any{
					"type":        "string",
					"description": "MCP server name",
				},
				"tool": map[string]any{
					"type":        "string",
					"description": "Tool name on that server",
				},
				"args": map[string]any{
					"type":        "object",
					"description": "JSON object of tool arguments",
				},
			},
			"required": []string{"server", "tool"},
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in struct {
				Server string `json:"server"`
				Tool   string `json:"tool"`
			}
			_ = json.Unmarshal(input, &in)
			return in.Server + "/" + in.Tool
		},
		Execute: func(ctx context.Context, input json.RawMessage) (ext.ToolResult, error) {
			var in struct {
				Server string         `json:"server"`
				Tool   string         `json:"tool"`
				Args   map[string]any `json:"args"`
			}
			if err := json.Unmarshal(input, &in); err != nil {
				return ext.ToolResult{}, fmt.Errorf("mcp_call: %w", err)
			}
			out, err := pool.Call(ctx, in.Server, in.Tool, in.Args)
			if err != nil {
				return ext.ToolResult{}, err
			}
			body := FormatCallResult(out, 32_000)
			return ext.ToolResult{Content: body, Detail: in.Server + "/" + in.Tool, Output: body}, nil
		},
	}
}

// promptSection renders the system-prompt block announcing the configured
// servers, or "" when there are none so the prompt stays clean. Server names
// only: tool schemas stay out of the context and are discovered on demand
// through mcp_list / mcp_inspect.
func promptSection(serverNames []string) string {
	servers := make([]string, 0, len(serverNames))
	for _, name := range serverNames {
		if name = strings.TrimSpace(name); name != "" {
			servers = append(servers, name)
		}
	}
	if len(servers) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("# MCP\n\n")
	b.WriteString(
		"MCP servers are configured for this session. Tool schemas stay out of context — discover on demand.\n\n",
	)
	b.WriteString("## How to use MCP\n")
	b.WriteString(
		"- When a task needs an external capability these servers might provide (including docs/URLs the repo cannot answer), use the mcp_* tools.\n",
	)
	b.WriteString("- `mcp_list` with a server name → tool names on that server.\n")
	b.WriteString("- `mcp_inspect` → compact params for one tool, then `mcp_call` to invoke.\n")
	b.WriteString("- Do not guess tool names or arguments; list/inspect first.\n\n")
	b.WriteString("## Configured servers\n")
	for _, name := range servers {
		b.WriteString("- " + name + "\n")
	}
	return strings.TrimSpace(b.String())
}
