package anthropic

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"hash"
	"strings"
)

type nativeBlock map[string]json.RawMessage

// Capture whole blocks: flattening text and tool calls loses signed block order.
type nativeCapture struct {
	items       []json.RawMessage
	block       nativeBlock
	index       int
	kind        string
	text        strings.Builder
	signature   strings.Builder
	hasThinking bool
	invalid     bool
}

func (c *nativeCapture) start(index int, raw json.RawMessage) {
	if c.block != nil || index != len(c.items) {
		c.invalid = true
	}
	c.block = nil
	c.index = index
	c.text.Reset()
	c.signature.Reset()
	if json.Unmarshal(raw, &c.block) != nil || c.block == nil {
		c.invalid = true
		return
	}
	c.kind = ""
	_ = json.Unmarshal(c.block["type"], &c.kind)
	c.invalid = c.invalid || c.kind == ""
	c.hasThinking = c.hasThinking || c.kind == "thinking" || c.kind == "redacted_thinking"
	key := "text"
	if c.kind == "thinking" {
		key = "thinking"
	}
	var text, signature string
	_ = json.Unmarshal(c.block[key], &text)
	_ = json.Unmarshal(c.block["signature"], &signature)
	c.text.WriteString(text)
	c.signature.WriteString(signature)
}

func (c *nativeCapture) delta(index int, kind, value string) {
	c.hasThinking = c.hasThinking || kind == "thinking_delta" || kind == "signature_delta"
	if c.block == nil || index != c.index {
		c.invalid = true
		return
	}
	switch {
	case kind == "text_delta" && c.kind == "text", kind == "thinking_delta" && c.kind == "thinking":
		c.text.WriteString(value)
	case kind == "signature_delta" && c.kind == "thinking":
		c.signature.WriteString(value)
	case kind == "input_json_delta" && c.kind == "tool_use":
	default:
		c.invalid = true
	}
}

func (c *nativeCapture) stop(index int, args string) {
	if c.block == nil || index != c.index {
		c.invalid = true
		return
	}
	switch c.kind {
	case "thinking":
		c.block["thinking"], _ = json.Marshal(c.text.String())
		c.block["signature"], _ = json.Marshal(c.signature.String())
		c.invalid = c.invalid || c.signature.Len() == 0
	case "redacted_thinking":
		var data string
		_ = json.Unmarshal(c.block["data"], &data)
		c.invalid = c.invalid || data == ""
	case "text":
		c.block["text"], _ = json.Marshal(c.text.String())
	case "tool_use":
		if args != "" {
			c.block["input"] = json.RawMessage(args)
		}
	}
	item, err := json.Marshal(c.block)
	c.invalid = c.invalid || err != nil
	c.items = append(c.items, item)
	c.block = nil
}

func endpointFingerprint(baseURL string) string {
	// Session files should not expose credentials embedded in endpoint URLs.
	return fmt.Sprintf("%x", sha256.Sum256([]byte(normalizeBaseURL(baseURL))))
}

// Hash the wire prefix incrementally. Strings and text arrays are equivalent,
// object key order is irrelevant, and cache markers are not signature inputs.
func writePrefix(h hash.Hash, value any) bool {
	if msg, ok := value.(anthropicMessage); ok {
		if text, ok := msg.Content.(string); ok {
			msg.Content = []anthropicContentBlock{{Type: "text", Text: text}}
		}
		value = msg
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var result map[string]any
	if decoder.Decode(&result) != nil {
		return false
	}
	for _, key := range []string{"system", "tools", "content"} {
		if blocks, ok := result[key].([]any); ok {
			for _, block := range blocks {
				if fields, ok := block.(map[string]any); ok {
					delete(fields, "cache_control")
				}
			}
		}
	}
	return json.NewEncoder(h).Encode(result) == nil
}
