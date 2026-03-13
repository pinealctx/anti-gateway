package models

// ========================
// Anthropic-compatible types
// ========================

type AnthropicRequest struct {
	Model       string             `json:"model"`
	Messages    []AnthropicMessage `json:"messages"`
	System      any                `json:"system,omitempty"` // string or []ContentBlock
	MaxTokens   int                `json:"max_tokens"`
	Temperature *float32           `json:"temperature,omitempty"`
	Stream      bool               `json:"stream,omitempty"`
	Tools       []AnthropicTool    `json:"tools,omitempty"`
	ToolChoice  any                `json:"tool_choice,omitempty"`
}

type AnthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []AnthropicContentBlock
}

type AnthropicContentBlock struct {
	Type      string       `json:"type"`
	Text      string       `json:"text,omitempty"`
	ID        string       `json:"id,omitempty"`          // for tool_use
	Name      string       `json:"name,omitempty"`        // for tool_use
	Input     any          `json:"input,omitempty"`       // for tool_use
	ToolUseID string       `json:"tool_use_id,omitempty"` // for tool_result
	Content   any          `json:"content,omitempty"`     // for tool_result (string or blocks)
	Source    *ImageSource `json:"source,omitempty"`      // for image
}

type ImageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // "image/png"
	Data      string `json:"data"`
}

type AnthropicTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"input_schema"` // JSON Schema
}

// ========================
// Anthropic SSE response types
// ========================

type AnthropicResponse struct {
	ID           string                  `json:"id"`
	Type         string                  `json:"type"`
	Role         string                  `json:"role"`
	Content      []AnthropicContentBlock `json:"content"`
	Model        string                  `json:"model"`
	StopReason   string                  `json:"stop_reason"`
	StopSequence *string                 `json:"stop_sequence"`
	Usage        AnthropicUsage          `json:"usage"`
}

type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// AnthropicSSEEvent represents a single event in the Anthropic SSE stream.
type AnthropicSSEEvent struct {
	Type    string // event type: message_start, content_block_start, etc.
	Payload any    // the JSON data payload
}
