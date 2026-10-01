// Package tools is the host-side tool seam. Tool is an alias of the plugin
// SDK ext.Tool — the only tool shape in the host — plus the schema/registry
// adapters the agent loop needs. There is no second tool API here.
package tools

import (
	ext "github.com/pulseaiclub/phi/ext/go"
	"github.com/pulseaiclub/phi/internal/llm"
	"github.com/pulseaiclub/phi/internal/tools/agenttool"
	"github.com/pulseaiclub/phi/internal/tools/tooldef"
)

type (
	// Tool is the plugin SDK tool. Built-in plugins, extension buses, and the
	// agent core all speak this shape.
	Tool = ext.Tool
	// Result is what a tool Execute returns.
	Result = ext.ToolResult
	// Registry maps tool name → tool.
	Registry map[string]Tool
)

// Definitions extracts LLM schemas from SDK tools. Readable rides along: it
// never serializes to the model but gates concurrent read-only batches.
func Definitions(list []Tool) []llm.ToolDefinition {
	out := make([]llm.ToolDefinition, len(list))
	for i, t := range list {
		out[i] = llm.ToolDefinition{
			Name:        t.Name,
			Description: t.Description,
			Params:      schemaFromMap(t.Parameters),
			Readable:    t.Readable,
		}
	}
	return out
}

// NewRegistry indexes tools by name.
func NewRegistry(list []Tool) Registry {
	m := make(Registry, len(list))
	for _, t := range list {
		m[t.Name] = t
	}
	return m
}

// schemaFromMap narrows the SDK's free-form JSON Schema map to the typed
// shape the LLM client marshals. Keywords beyond type/properties/required
// (enum, defaults, nested descriptions) live inside Properties verbatim.
func schemaFromMap(m map[string]any) *llm.FunctionParameters {
	if m == nil {
		return &llm.FunctionParameters{Type: "object", Properties: llm.Object{}}
	}
	fp := &llm.FunctionParameters{Type: "object", Properties: llm.Object{}}
	if t, ok := m["type"].(string); ok && t != "" {
		fp.Type = t
	}
	if props, ok := m["properties"].(map[string]any); ok {
		fp.Properties = props
	}
	switch req := m["required"].(type) {
	case []string:
		fp.Required = req
	case []any:
		for _, v := range req {
			if s, ok := v.(string); ok {
				fp.Required = append(fp.Required, s)
			}
		}
	}
	return fp
}

// Context helpers re-exported from tooldef.
var (
	WithToolCallID = tooldef.WithToolCallID
	WithCwd        = tooldef.WithCwd
)

type (
	// AgentDeps re-exports agenttool.AgentDeps.
	AgentDeps = agenttool.AgentDeps
	// AgentResult re-exports agenttool.AgentResult.
	AgentResult = agenttool.AgentResult
)

// AgentTools and ParseAgentResult are re-exported tool helpers.
var (
	AgentTools       = agenttool.AgentTools
	ParseAgentResult = agenttool.ParseAgentResult
)
