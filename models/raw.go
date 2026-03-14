package models

import (
	"encoding/json"
	"strings"
)

// RawString marshals a Go string to a JSON string value (json.RawMessage).
func RawString(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

// MustMarshal marshals any value to json.RawMessage.
func MustMarshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// ContentText extracts plain text from a Content field (json.RawMessage).
// Handles JSON string ("hello") and content-part arrays ([{"type":"text","text":"..."}]).
func ContentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var parts []map[string]any
	if err := json.Unmarshal(raw, &parts); err == nil {
		var texts []string
		for _, p := range parts {
			if t, ok := p["type"].(string); ok && t == "text" {
				if text, ok := p["text"].(string); ok {
					texts = append(texts, text)
				}
			}
		}
		return strings.Join(texts, "\n")
	}
	return string(raw)
}

// ContentParts tries to parse a Content json.RawMessage as an array of content-part maps.
// Returns nil, false if the content is not an array (e.g. a plain string).
func ContentParts(raw json.RawMessage) ([]map[string]any, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var parts []map[string]any
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, false
	}
	return parts, true
}

// AnthropicBlocks tries to parse a Content json.RawMessage as []AnthropicContentBlock.
// Returns nil, false if the content is not an array of blocks (e.g. a plain string).
func AnthropicBlocks(raw json.RawMessage) ([]AnthropicContentBlock, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var blocks []AnthropicContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, false
	}
	return blocks, true
}

// mergeExtras merges known-field JSON (base) with an extras map.
// Used by custom MarshalJSON implementations.
func mergeExtras(base []byte, extras map[string]json.RawMessage) ([]byte, error) {
	if len(extras) == 0 {
		return base, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(base, &m); err != nil {
		return nil, err
	}
	for k, v := range extras {
		if _, exists := m[k]; !exists {
			m[k] = v
		}
	}
	return json.Marshal(m)
}

// captureExtras unmarshals data into a raw map and removes known keys.
// Returns the remaining (unknown) fields.
func captureExtras(data []byte, known map[string]bool) map[string]json.RawMessage {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	for k := range known {
		delete(raw, k)
	}
	if len(raw) == 0 {
		return nil
	}
	return raw
}
