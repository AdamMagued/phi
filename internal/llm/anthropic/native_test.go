package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pulseaiclub/phi/internal/llm"
)

var nativeSSE = strings.Join([]string{
	`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":" start\n","signature":"sig/"}}`,
	`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"next"}}`,
	`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"opaque"}}`,
	`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"+=="}}`,
	`data: {"type":"content_block_stop","index":0}`,
	`data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":"before"}}`,
	`data: {"type":"content_block_stop","index":1}`,
	`data: {"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu.native","name":"read","input":{}}}`,
	`data: {"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"a.go\",\"cache_control\":9007199254740993}"}}`,
	`data: {"type":"content_block_stop","index":2}`,
	`data: {"type":"content_block_start","index":3,"content_block":{"type":"redacted_thinking","data":"encrypted+==","extra":"keep"}}`,
	`data: {"type":"content_block_stop","index":3}`,
	`data: {"type":"content_block_start","index":4,"content_block":{"type":"text","text":"after"}}`,
	`data: {"type":"content_block_stop","index":4}`,
	`data: {"type":"message_delta","usage":{"output_tokens":12}}`,
	`data: {"type":"message_stop"}`,
}, "\n\n") + "\n\n"

func nativeResponse(t *testing.T, req AnthropicRequest) llm.Message {
	t.Helper()
	events := processForTest(nativeSSE)
	require.NotEmpty(t, events)
	done := events[len(events)-1]
	require.Equal(t, llm.StreamEventTypeDone, done.Type)
	require.NotNil(t, done.Final.Native)
	state := *req.native
	state.Items = done.Final.Native.Items
	done.Final.Native = &state
	return *done.Final
}

func TestNativeContentOrder(t *testing.T) {
	cfg := llm.ModelConfig{Name: "claude", Think: llm.ThinkConfig{Enabled: true}}
	user := llm.Message{Role: llm.RoleUser, Content: "read"}
	msg := nativeResponse(t, BuildRequest(cfg, "system", []llm.Message{user}, nil))
	assert.Equal(t, " start\nnext", msg.ReasoningContent)
	assert.Equal(t, "beforeafter", msg.Content)
	require.Len(t, msg.Native.Items, 5)
	for i, want := range []string{
		`{"type":"thinking","thinking":" start\nnext","signature":"sig/opaque+=="}`,
		`{"type":"text","text":"before"}`,
		`{"type":"tool_use","id":"toolu.native","name":"read","input":{"path":"a.go","cache_control":9007199254740993}}`,
		`{"type":"redacted_thinking","data":"encrypted+==","extra":"keep"}`,
		`{"type":"text","text":"after"}`,
	} {
		assert.JSONEq(t, want, string(msg.Native.Items[i]))
	}
	result := llm.Message{Role: llm.RoleTool, ToolCallID: msg.ToolCalls[0].ID, Content: "file"}
	req := BuildRequest(cfg, "system", []llm.Message{user, msg, result}, nil)
	body, err := json.Marshal(req)
	require.NoError(t, err)
	assert.Contains(t, string(body), `"tool_use_id":"toolu.native"`)
	assert.Contains(t, string(body), `"signature":"sig/opaque+=="`)
	assert.NotContains(t, string(body), `"native"`)
	assert.Equal(t, msg.Native.Items, req.Messages[1].Content)
}

func TestNativePrefixFallback(t *testing.T) {
	cfg := llm.ModelConfig{Name: "claude", Think: llm.ThinkConfig{Enabled: true}}
	tools := []llm.ToolDefinition{{Name: "read", Description: "read a file"}}
	user := llm.Message{Role: llm.RoleUser, Content: "read"}
	msg := nativeResponse(t, BuildRequest(cfg, "system", []llm.Message{user}, tools))
	result := llm.Message{Role: llm.RoleTool, ToolCallID: msg.ToolCalls[0].ID, Content: "file"}
	cases := []struct {
		name   string
		change func(*llm.ModelConfig, *string, *[]llm.Message, *[]llm.ToolDefinition)
		keep   bool
	}{
		{"unchanged", func(*llm.ModelConfig, *string, *[]llm.Message, *[]llm.ToolDefinition) {}, true},
		{
			"system",
			func(_ *llm.ModelConfig, s *string, _ *[]llm.Message, _ *[]llm.ToolDefinition) { *s = "new" },
			false,
		},
		{"tools", func(_ *llm.ModelConfig, _ *string, _ *[]llm.Message, ts *[]llm.ToolDefinition) {
			(*ts)[0].Description = "changed"
		}, false},
		{"history", func(_ *llm.ModelConfig, _ *string, ms *[]llm.Message, _ *[]llm.ToolDefinition) {
			(*ms)[0].Content = "changed"
		}, false},
		{
			"model",
			func(c *llm.ModelConfig, _ *string, _ *[]llm.Message, _ *[]llm.ToolDefinition) { c.Name = "other" },
			false,
		},
		{"endpoint", func(c *llm.ModelConfig, _ *string, _ *[]llm.Message, _ *[]llm.ToolDefinition) {
			c.BaseURL = "https://proxy.test"
		}, false},
		{"normalized endpoint", func(c *llm.ModelConfig, _ *string, _ *[]llm.Message, _ *[]llm.ToolDefinition) {
			c.BaseURL = "https://api.anthropic.com/v1/"
		}, true},
		{"thinking off", func(c *llm.ModelConfig, _ *string, _ *[]llm.Message, _ *[]llm.ToolDefinition) {
			c.Think.Enabled = false
		}, false},
		{"budget", func(c *llm.ModelConfig, _ *string, _ *[]llm.Message, _ *[]llm.ToolDefinition) {
			c.Think.Mode = llm.High
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config, system := cfg, "system"
			messages := []llm.Message{user, msg, result}
			defs := append([]llm.ToolDefinition(nil), tools...)
			tc.change(&config, &system, &messages, &defs)
			req := BuildRequest(config, system, messages, defs)
			body, err := json.Marshal(req)
			require.NoError(t, err)
			if tc.keep {
				assert.Contains(t, string(body), `"signature"`)
				assert.Contains(t, string(body), `"redacted_thinking"`)
			} else {
				assert.NotContains(t, string(body), `"signature"`)
				assert.NotContains(t, string(body), `"redacted_thinking"`)
				blocks := req.Messages[1].Content.([]anthropicContentBlock)
				assert.Equal(t, "beforeafter", blocks[0].Text)
				assert.Equal(t, "read", blocks[1].Name)
				assert.JSONEq(t, msg.ToolCalls[0].Function.Arguments, string(blocks[1].Input))
				results := req.Messages[2].Content.([]anthropicContentBlock)
				assert.Equal(t, blocks[1].ID, results[0].ToolUseID)
			}
			assert.Equal(t, "sig/opaque+==", nativeSignature(t, msg.Native.Items[0]))
		})
	}
}

func nativeSignature(t *testing.T, item json.RawMessage) string {
	t.Helper()
	var block struct {
		Signature string `json:"signature"`
	}
	require.NoError(t, json.Unmarshal(item, &block))
	return block.Signature
}

func TestNativePrefixDoesNotLeaveGap(t *testing.T) {
	cfg := llm.ModelConfig{Name: "claude", Think: llm.ThinkConfig{Enabled: true}}
	messages := []llm.Message{{Role: llm.RoleUser, Content: "first"}}
	for range 3 {
		msg := nativeResponse(t, BuildRequest(cfg, "", messages, nil))
		messages = append(
			messages,
			msg,
			llm.Message{Role: llm.RoleTool, ToolCallID: msg.ToolCalls[0].ID, Content: "ok"},
		)
	}
	messages[3].Native = nil
	req := BuildRequest(cfg, "", messages, nil)
	body, err := json.Marshal(req)
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(body), `"signature"`), "the later thinking must be stripped with the gap")

	// A new response belongs to the degraded prefix and can be replayed normally.
	msg := nativeResponse(t, req)
	messages = append(messages, msg, llm.Message{Role: llm.RoleTool, ToolCallID: msg.ToolCalls[0].ID, Content: "new"})
	req = BuildRequest(cfg, "", messages, nil)
	body, err = json.Marshal(req)
	require.NoError(t, err)
	assert.Equal(t, 2, strings.Count(string(body), `"signature"`))
}

func TestIncompleteNativeFallsBack(t *testing.T) {
	for _, tc := range []struct{ name, sse string }{
		{"missing message stop", strings.ReplaceAll(nativeSSE, `data: {"type":"message_stop"}`, "")},
		{"missing signature", strings.ReplaceAll(nativeSSE, "signature", "unknown")},
		{"missing block stop", strings.ReplaceAll(nativeSSE, `data: {"type":"content_block_stop","index":4}`, "")},
		{"wrong order", strings.ReplaceAll(nativeSSE, `"index":3`, `"index":8`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := processForTest(tc.sse)
			require.NotEmpty(t, events)
			done := events[len(events)-1]
			require.Equal(t, llm.StreamEventTypeDone, done.Type)
			require.NotNil(t, done.Final)
			assert.Nil(t, done.Final.Native)
			assert.Equal(t, "beforeafter", done.Final.Content)
			assert.Equal(t, " start\nnext", done.Final.ReasoningContent)
			require.Len(t, done.Final.ToolCalls, 1)
			assert.Equal(t, "read", done.Final.ToolCalls[0].Function.Name)
		})
	}
}

func TestNativeOnlyAndLegacyHistory(t *testing.T) {
	cfg := llm.ModelConfig{Name: "claude", Think: llm.ThinkConfig{Enabled: true}}
	user := llm.Message{Role: llm.RoleUser, Content: "think"}
	state := *BuildRequest(cfg, "", []llm.Message{user}, nil).native
	state.Items = []json.RawMessage{json.RawMessage(`{"type":"thinking","thinking":"","signature":"signed"}`)}
	msg := llm.Message{Role: llm.RoleAssistant, Native: &state}
	req := BuildRequest(cfg, "", []llm.Message{user, msg}, nil)
	require.Len(t, req.Messages, 2)
	assert.Equal(t, state.Items, req.Messages[1].Content)
	msg.Native = nil
	msg.ReasoningContent = "legacy display text"
	req = BuildRequest(cfg, "", []llm.Message{user, msg}, nil)
	assert.Len(t, req.Messages, 1, "legacy thinking has no signed wire content")
}
