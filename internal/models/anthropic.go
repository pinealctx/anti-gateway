package models

import "encoding/json"

// ========================
// Anthropic-compatible types
// Ref: https://docs.anthropic.com/en/api/messages
// ========================

type AnthropicRequest struct {
	Model         string             `json:"model"`
	Messages      []AnthropicMessage `json:"messages"`
	System        json.RawMessage    `json:"system,omitempty"` // string or []ContentBlock
	MaxTokens     int                `json:"max_tokens"`
	Temperature   *float32           `json:"temperature,omitempty"`
	TopP          *float32           `json:"top_p,omitempty"`
	TopK          *int               `json:"top_k,omitempty"`
	Stream        bool               `json:"stream,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	Tools         []AnthropicTool    `json:"tools,omitempty"`
	ToolChoice    json.RawMessage    `json:"tool_choice,omitempty"` // "auto" | "any" | {"type":"tool","name":"..."}
	Metadata      *AnthropicMetadata `json:"metadata,omitempty"`
}

type AnthropicMetadata struct {
	UserID string `json:"user_id,omitempty"`
}

type AnthropicMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // string or []AnthropicContentBlock
}

type AnthropicContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`          // for tool_use
	Name      string          `json:"name,omitempty"`        // for tool_use
	Input     json.RawMessage `json:"input,omitempty"`       // for tool_use
	ToolUseID string          `json:"tool_use_id,omitempty"` // for tool_result
	Content   json.RawMessage `json:"content,omitempty"`     // for tool_result (string or blocks)
	IsError   *bool           `json:"is_error,omitempty"`    // for tool_result
	Source    *ImageSource    `json:"source,omitempty"`      // for image

	// Cache control (prompt caching)
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

type CacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

type ImageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // "image/png", "image/jpeg", "image/gif", "image/webp"
	Data      string `json:"data"`
}

type AnthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"` // JSON Schema

	// Cache control (prompt caching)
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// ========================
// Anthropic response types
// Ref: https://docs.anthropic.com/en/api/messages
// ========================

type AnthropicResponse struct {
	ID           string                  `json:"id"`
	Type         string                  `json:"type"` // "message"
	Role         string                  `json:"role"`
	Content      []AnthropicContentBlock `json:"content"`
	Model        string                  `json:"model"`
	StopReason   string                  `json:"stop_reason"`
	StopSequence *string                 `json:"stop_sequence"`
	Usage        AnthropicUsage          `json:"usage"`
}

type AnthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// AnthropicSSEEvent represents a single event in the Anthropic SSE stream.
type AnthropicSSEEvent struct {
	Type    string // event type: message_start, content_block_start, etc.
	Payload any    // the JSON data payload
}
