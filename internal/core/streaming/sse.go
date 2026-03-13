package streaming

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pinealctx/anti-gateway/internal/models"
)

// OpenAISSEWriter writes OpenAI-compatible SSE chunks to a Gin response.
type OpenAISSEWriter struct {
	c     *gin.Context
	model string
	id    string
}

func NewOpenAISSEWriter(c *gin.Context, model, id string) *OpenAISSEWriter {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Transfer-Encoding", "chunked")
	c.Header("X-Accel-Buffering", "no")
	return &OpenAISSEWriter{c: c, model: model, id: id}
}

func (w *OpenAISSEWriter) WriteContentDelta(content string) error {
	chunk := models.ChatCompletionChunk{
		ID:      w.id,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   w.model,
		Choices: []models.ChatCompletionChunkChoice{
			{
				Index: 0,
				Delta: models.ChatCompletionDelta{Content: content},
			},
		},
	}
	return w.writeEvent(chunk)
}

func (w *OpenAISSEWriter) WriteRoleDelta() error {
	chunk := models.ChatCompletionChunk{
		ID:      w.id,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   w.model,
		Choices: []models.ChatCompletionChunkChoice{
			{
				Index: 0,
				Delta: models.ChatCompletionDelta{Role: "assistant"},
			},
		},
	}
	return w.writeEvent(chunk)
}

func (w *OpenAISSEWriter) WriteToolCallDelta(toolCalls []models.ToolCall) error {
	chunk := models.ChatCompletionChunk{
		ID:      w.id,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   w.model,
		Choices: []models.ChatCompletionChunkChoice{
			{
				Index: 0,
				Delta: models.ChatCompletionDelta{ToolCalls: toolCalls},
			},
		},
	}
	return w.writeEvent(chunk)
}

func (w *OpenAISSEWriter) WriteFinish(reason string) error {
	chunk := models.ChatCompletionChunk{
		ID:      w.id,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   w.model,
		Choices: []models.ChatCompletionChunkChoice{
			{
				Index:        0,
				Delta:        models.ChatCompletionDelta{},
				FinishReason: &reason,
			},
		},
	}
	if err := w.writeEvent(chunk); err != nil {
		return err
	}
	// Write [DONE]
	_, err := fmt.Fprintf(w.c.Writer, "data: [DONE]\n\n")
	w.c.Writer.Flush()
	return err
}

func (w *OpenAISSEWriter) writeEvent(data any) error {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w.c.Writer, "data: %s\n\n", jsonBytes)
	if err != nil {
		return err
	}
	w.c.Writer.Flush()
	return nil
}

// AnthropicSSEWriter writes Anthropic-compatible SSE events.
type AnthropicSSEWriter struct {
	c          *gin.Context
	model      string
	id         string
	blockIndex int
}

func NewAnthropicSSEWriter(c *gin.Context, model, id string) *AnthropicSSEWriter {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	return &AnthropicSSEWriter{c: c, model: model, id: id, blockIndex: 0}
}

func (w *AnthropicSSEWriter) WriteMessageStart(inputTokens int) error {
	return w.writeTypedEvent("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            w.id,
			"type":          "message",
			"role":          "assistant",
			"content":       []any{},
			"model":         w.model,
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         map[string]any{"input_tokens": inputTokens, "output_tokens": 0},
		},
	})
}

func (w *AnthropicSSEWriter) WriteContentBlockStart() error {
	err := w.writeTypedEvent("content_block_start", map[string]any{
		"type":  "content_block_start",
		"index": w.blockIndex,
		"content_block": map[string]any{
			"type": "text",
			"text": "",
		},
	})
	return err
}

func (w *AnthropicSSEWriter) WriteContentDelta(text string) error {
	return w.writeTypedEvent("content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": w.blockIndex,
		"delta": map[string]any{
			"type": "text_delta",
			"text": text,
		},
	})
}

func (w *AnthropicSSEWriter) WriteContentBlockStop() error {
	err := w.writeTypedEvent("content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": w.blockIndex,
	})
	w.blockIndex++
	return err
}

// WriteToolUseBlockStart emits a content_block_start event for a tool_use block.
func (w *AnthropicSSEWriter) WriteToolUseBlockStart(toolID, toolName string) error {
	err := w.writeTypedEvent("content_block_start", map[string]any{
		"type":  "content_block_start",
		"index": w.blockIndex,
		"content_block": map[string]any{
			"type":  "tool_use",
			"id":    toolID,
			"name":  toolName,
			"input": map[string]any{},
		},
	})
	return err
}

// WriteToolUseInputDelta emits a content_block_delta with input_json_delta for tool_use streaming.
func (w *AnthropicSSEWriter) WriteToolUseInputDelta(partialJSON string) error {
	return w.writeTypedEvent("content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": w.blockIndex,
		"delta": map[string]any{
			"type":         "input_json_delta",
			"partial_json": partialJSON,
		},
	})
}

func (w *AnthropicSSEWriter) WriteMessageDelta(stopReason string, outputTokens int) error {
	return w.writeTypedEvent("message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": map[string]any{
			"output_tokens": outputTokens,
		},
	})
}

func (w *AnthropicSSEWriter) WriteMessageStop() error {
	return w.writeTypedEvent("message_stop", map[string]any{
		"type": "message_stop",
	})
}

func (w *AnthropicSSEWriter) writeTypedEvent(eventType string, data any) error {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w.c.Writer, "event: %s\ndata: %s\n\n", eventType, jsonBytes)
	if err != nil {
		return err
	}
	w.c.Writer.Flush()
	return nil
}
