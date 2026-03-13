package models

import "encoding/json"

// ========================
// OpenAI-compatible types — transparent pass-through design
//
// Only fields actively inspected/modified by the gateway are concrete.
// All other request parameters are captured in Extras and re-emitted
// during serialization, ensuring new API fields pass through unchanged.
//
// Ref: https://platform.openai.com/docs/api-reference/chat/create
// ========================

// ChatCompletionRequest is the OpenAI /v1/chat/completions request.
// Concrete fields are those the gateway reads/writes for routing, conversion,
// or continuation logic. Everything else lives in Extras.
type ChatCompletionRequest struct {
	Model       string          `json:"model"`
	Messages    []ChatMessage   `json:"messages"`
	Stream      bool            `json:"stream,omitempty"`
	MaxTokens   *int            `json:"max_tokens,omitempty"`
	Temperature *float32        `json:"temperature,omitempty"`
	Tools       []Tool          `json:"tools,omitempty"`
	ToolChoice  json.RawMessage `json:"tool_choice,omitempty"` // "none" | "auto" | "required" | object

	// Extras captures all other JSON fields (top_p, stop, response_format, etc.)
	// for transparent pass-through to upstream providers.
	Extras map[string]json.RawMessage `json:"-"`
}

var chatRequestKnownFields = map[string]bool{
	"model": true, "messages": true, "stream": true,
	"max_tokens": true, "temperature": true,
	"tools": true, "tool_choice": true,
}

func (r *ChatCompletionRequest) UnmarshalJSON(data []byte) error {
	type alias ChatCompletionRequest
	if err := json.Unmarshal(data, (*alias)(r)); err != nil {
		return err
	}
	r.Extras = captureExtras(data, chatRequestKnownFields)
	return nil
}

func (r ChatCompletionRequest) MarshalJSON() ([]byte, error) {
	type alias ChatCompletionRequest
	base, err := json.Marshal(alias(r))
	if err != nil {
		return nil, err
	}
	return mergeExtras(base, r.Extras)
}

// ChatMessage represents a single message in the conversation.
// Content is json.RawMessage so it can hold either a JSON string ("hello")
// or an array of content parts ([{"type":"text","text":"..."}]) transparently.
type ChatMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	Name       string          `json:"name,omitempty"`
	ToolCalls  []ToolCall      `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	Refusal    string          `json:"refusal,omitempty"`
}

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"` // JSON Schema
	Strict      *bool           `json:"strict,omitempty"`
}

type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
	Index    *int             `json:"index,omitempty"` // used in streaming deltas
}

type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatCompletionResponse is the standard OpenAI response (non-streaming).
type ChatCompletionResponse struct {
	ID                string                 `json:"id"`
	Object            string                 `json:"object"`
	Created           int64                  `json:"created"`
	Model             string                 `json:"model"`
	Choices           []ChatCompletionChoice `json:"choices"`
	Usage             *Usage                 `json:"usage,omitempty"`
	SystemFingerprint string                 `json:"system_fingerprint,omitempty"`
	ServiceTier       string                 `json:"service_tier,omitempty"`

	Extras map[string]json.RawMessage `json:"-"`
}

var chatResponseKnownFields = map[string]bool{
	"id": true, "object": true, "created": true, "model": true,
	"choices": true, "usage": true, "system_fingerprint": true,
	"service_tier": true,
}

func (r *ChatCompletionResponse) UnmarshalJSON(data []byte) error {
	type alias ChatCompletionResponse
	if err := json.Unmarshal(data, (*alias)(r)); err != nil {
		return err
	}
	r.Extras = captureExtras(data, chatResponseKnownFields)
	return nil
}

func (r ChatCompletionResponse) MarshalJSON() ([]byte, error) {
	type alias ChatCompletionResponse
	base, err := json.Marshal(alias(r))
	if err != nil {
		return nil, err
	}
	return mergeExtras(base, r.Extras)
}

type ChatCompletionChoice struct {
	Index        int             `json:"index"`
	Message      ChatMessage     `json:"message"`
	FinishReason string          `json:"finish_reason"`
	Logprobs     json.RawMessage `json:"logprobs,omitempty"`
}

type Usage struct {
	PromptTokens            int          `json:"prompt_tokens"`
	CompletionTokens        int          `json:"completion_tokens"`
	TotalTokens             int          `json:"total_tokens"`
	PromptTokensDetails     *TokenDetail `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *TokenDetail `json:"completion_tokens_details,omitempty"`
}

// TokenDetail provides a breakdown of token counts.
type TokenDetail struct {
	CachedTokens    int `json:"cached_tokens,omitempty"`
	AudioTokens     int `json:"audio_tokens,omitempty"`
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
}

// ========================
// OpenAI SSE streaming types
// ========================

type ChatCompletionChunk struct {
	ID                string                      `json:"id"`
	Object            string                      `json:"object"`
	Created           int64                       `json:"created"`
	Model             string                      `json:"model"`
	Choices           []ChatCompletionChunkChoice `json:"choices"`
	Usage             *Usage                      `json:"usage,omitempty"`
	SystemFingerprint string                      `json:"system_fingerprint,omitempty"`
	ServiceTier       string                      `json:"service_tier,omitempty"`
}

type ChatCompletionChunkChoice struct {
	Index        int                 `json:"index"`
	Delta        ChatCompletionDelta `json:"delta"`
	FinishReason *string             `json:"finish_reason"`
	Logprobs     json.RawMessage     `json:"logprobs,omitempty"`
}

type ChatCompletionDelta struct {
	Role      string     `json:"role,omitempty"`
	Content   string     `json:"content,omitempty"`
	Refusal   string     `json:"refusal,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}
