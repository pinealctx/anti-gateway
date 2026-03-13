package converter

import (
	"encoding/json"
	"testing"

	"github.com/pinealctx/anti-gateway/internal/models"
)

// ============================================================
// OpenAIToAnthropic — basic conversion
// ============================================================

func TestOpenAIToAnthropic_SystemMessage(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model: "claude-opus-4.6",
		Messages: []models.ChatMessage{
			{Role: "system", Content: models.RawString("You are helpful.")},
			{Role: "user", Content: models.RawString("Hello")},
		},
	}
	result, err := OpenAIToAnthropic(req)
	if err != nil {
		t.Fatal(err)
	}
	if models.ContentText(result.System) != "You are helpful." {
		t.Errorf("System = %v, want 'You are helpful.'", result.System)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	if result.Messages[0].Role != "user" {
		t.Errorf("Role = %s, want user", result.Messages[0].Role)
	}
}

func TestOpenAIToAnthropic_ModelPassthrough(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model:    "claude-sonnet-4",
		Messages: []models.ChatMessage{{Role: "user", Content: models.RawString("hi")}},
	}
	result, err := OpenAIToAnthropic(req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Model != "claude-sonnet-4" {
		t.Errorf("Model = %s, want claude-sonnet-4", result.Model)
	}
}

func TestOpenAIToAnthropic_StreamFlag(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model:    "test",
		Stream:   true,
		Messages: []models.ChatMessage{{Role: "user", Content: models.RawString("hi")}},
	}
	result, err := OpenAIToAnthropic(req)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Stream {
		t.Error("Stream should be true")
	}
}

func TestOpenAIToAnthropic_Temperature(t *testing.T) {
	temp := float32(0.7)
	req := &models.ChatCompletionRequest{
		Model:       "test",
		Temperature: &temp,
		Messages:    []models.ChatMessage{{Role: "user", Content: models.RawString("hi")}},
	}
	result, err := OpenAIToAnthropic(req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Temperature == nil || *result.Temperature != 0.7 {
		t.Errorf("Temperature = %v, want 0.7", result.Temperature)
	}
}

func TestOpenAIToAnthropic_MaxTokens(t *testing.T) {
	max := 100
	req := &models.ChatCompletionRequest{
		Model:     "test",
		MaxTokens: &max,
		Messages:  []models.ChatMessage{{Role: "user", Content: models.RawString("hi")}},
	}
	result, err := OpenAIToAnthropic(req)
	if err != nil {
		t.Fatal(err)
	}
	if result.MaxTokens != 100 {
		t.Errorf("MaxTokens = %d, want 100", result.MaxTokens)
	}
}

func TestOpenAIToAnthropic_DefaultMaxTokens(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model:    "test",
		Messages: []models.ChatMessage{{Role: "user", Content: models.RawString("hi")}},
	}
	result, err := OpenAIToAnthropic(req)
	if err != nil {
		t.Fatal(err)
	}
	if result.MaxTokens != 8192 {
		t.Errorf("MaxTokens = %d, want 8192 (default)", result.MaxTokens)
	}
}

// ============================================================
// User message conversion
// ============================================================

func TestOpenAIToAnthropic_SimpleUserMessage(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model:    "test",
		Messages: []models.ChatMessage{{Role: "user", Content: models.RawString("Hello world")}},
	}
	result, err := OpenAIToAnthropic(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	msg := result.Messages[0]
	if msg.Role != "user" {
		t.Errorf("Role = %s, want user", msg.Role)
	}
	// Content should be string
	if models.ContentText(msg.Content) != "Hello world" {
		t.Errorf("Content = %v, want 'Hello world'", msg.Content)
	}
}

func TestOpenAIToAnthropic_UserVisionMessage(t *testing.T) {
	// Multi-part content with text + image
	req := &models.ChatCompletionRequest{
		Model: "test",
		Messages: []models.ChatMessage{
			{
				Role: "user",
				Content: models.MustMarshal([]any{
					map[string]any{"type": "text", "text": "What is this?"},
					map[string]any{
						"type": "image_url",
						"image_url": map[string]any{
							"url": "data:image/png;base64,iVBOR",
						},
					},
				}),
			},
		},
	}
	result, err := OpenAIToAnthropic(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	msg := result.Messages[0]
	blocks, ok := models.AnthropicBlocks(msg.Content)
	if !ok {
		t.Fatalf("Content should be []AnthropicContentBlock, got %s", string(msg.Content))
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0].Type != "text" || blocks[0].Text != "What is this?" {
		t.Errorf("block 0: type=%s text=%s", blocks[0].Type, blocks[0].Text)
	}
	if blocks[1].Type != "image" || blocks[1].Source == nil {
		t.Fatalf("block 1 should be image with source")
	}
	if blocks[1].Source.Type != "base64" {
		t.Errorf("source type = %s, want base64", blocks[1].Source.Type)
	}
	if blocks[1].Source.MediaType != "image/png" {
		t.Errorf("media type = %s, want image/png", blocks[1].Source.MediaType)
	}
	if blocks[1].Source.Data != "iVBOR" {
		t.Errorf("data = %s, want iVBOR", blocks[1].Source.Data)
	}
}

// ============================================================
// Assistant message conversion
// ============================================================

func TestOpenAIToAnthropic_AssistantSimple(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model: "test",
		Messages: []models.ChatMessage{
			{Role: "user", Content: models.RawString("hi")},
			{Role: "assistant", Content: models.RawString("Hello!")},
		},
	}
	result, err := OpenAIToAnthropic(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result.Messages))
	}
	if models.ContentText(result.Messages[1].Content) != "Hello!" {
		t.Errorf("assistant content = %v", result.Messages[1].Content)
	}
}

func TestOpenAIToAnthropic_AssistantWithToolCalls(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model: "test",
		Messages: []models.ChatMessage{
			{Role: "user", Content: models.RawString("What's the weather?")},
			{
				Role:    "assistant",
				Content: models.RawString("Let me check."),
				ToolCalls: []models.ToolCall{
					{
						ID:   "call_123",
						Type: "function",
						Function: models.ToolCallFunction{
							Name:      "get_weather",
							Arguments: `{"city":"NYC"}`,
						},
					},
				},
			},
		},
	}
	result, err := OpenAIToAnthropic(req)
	if err != nil {
		t.Fatal(err)
	}

	assistantMsg := result.Messages[1]
	blocks, ok := models.AnthropicBlocks(assistantMsg.Content)
	if !ok {
		t.Fatalf("Content should be blocks, got %s", string(assistantMsg.Content))
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks (text + tool_use), got %d", len(blocks))
	}

	// Text block
	if blocks[0].Type != "text" || blocks[0].Text != "Let me check." {
		t.Errorf("text block: type=%s text=%s", blocks[0].Type, blocks[0].Text)
	}

	// Tool use block
	if blocks[1].Type != "tool_use" {
		t.Errorf("tool_use block type = %s", blocks[1].Type)
	}
	if blocks[1].ID != "call_123" {
		t.Errorf("tool_use ID = %s", blocks[1].ID)
	}
	if blocks[1].Name != "get_weather" {
		t.Errorf("tool_use name = %s", blocks[1].Name)
	}
	// Check input was parsed from JSON
	var inputMap map[string]any
	json.Unmarshal(blocks[1].Input, &inputMap)
	if inputMap["city"] != "NYC" {
		t.Errorf("input city = %v", inputMap["city"])
	}
}

// ============================================================
// Tool message conversion
// ============================================================

func TestOpenAIToAnthropic_ToolMessage(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model: "test",
		Messages: []models.ChatMessage{
			{Role: "user", Content: models.RawString("hi")},
			{
				Role:       "tool",
				Content:    models.RawString(`{"temperature": 72}`),
				ToolCallID: "call_123",
			},
		},
	}
	result, err := OpenAIToAnthropic(req)
	if err != nil {
		t.Fatal(err)
	}

	toolMsg := result.Messages[1]
	if toolMsg.Role != "user" {
		t.Errorf("tool message role = %s, want user", toolMsg.Role)
	}
	blocks, ok := models.AnthropicBlocks(toolMsg.Content)
	if !ok {
		t.Fatalf("Content should be blocks, got %s", string(toolMsg.Content))
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Type != "tool_result" {
		t.Errorf("type = %s, want tool_result", blocks[0].Type)
	}
	if blocks[0].ToolUseID != "call_123" {
		t.Errorf("tool_use_id = %s, want call_123", blocks[0].ToolUseID)
	}
}

// ============================================================
// Tools conversion
// ============================================================

func TestOpenAIToAnthropic_Tools(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"city": map[string]any{"type": "string"},
		},
	}
	req := &models.ChatCompletionRequest{
		Model:    "test",
		Messages: []models.ChatMessage{{Role: "user", Content: models.RawString("hi")}},
		Tools: []models.Tool{
			{
				Type: "function",
				Function: models.ToolFunction{
					Name:        "get_weather",
					Description: "Get weather info",
					Parameters:  models.MustMarshal(schema),
				},
			},
		},
	}
	result, err := OpenAIToAnthropic(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result.Tools))
	}
	tool := result.Tools[0]
	if tool.Name != "get_weather" {
		t.Errorf("tool name = %s", tool.Name)
	}
	if tool.Description != "Get weather info" {
		t.Errorf("tool description = %s", tool.Description)
	}
	// InputSchema should match the parameters
	schemaJSON, _ := json.Marshal(tool.InputSchema)
	if len(schemaJSON) == 0 {
		t.Error("input_schema should not be empty")
	}
}

// ============================================================
// ToolChoice conversion
// ============================================================

func TestConvertToolChoice_Auto(t *testing.T) {
	result := convertOpenAIToolChoiceToAnthropic(models.RawString("auto"))
	var m map[string]any
	json.Unmarshal(result, &m)
	if m["type"] != "auto" {
		t.Errorf("type = %v, want auto", m["type"])
	}
}

func TestConvertToolChoice_Required(t *testing.T) {
	result := convertOpenAIToolChoiceToAnthropic(models.RawString("required"))
	var m map[string]any
	json.Unmarshal(result, &m)
	if m["type"] != "any" {
		t.Errorf("type = %v, want any", m["type"])
	}
}

func TestConvertToolChoice_None(t *testing.T) {
	result := convertOpenAIToolChoiceToAnthropic(models.RawString("none"))
	if result != nil {
		t.Errorf("none should return nil, got %s", string(result))
	}
}

func TestConvertToolChoice_Nil(t *testing.T) {
	result := convertOpenAIToolChoiceToAnthropic(nil)
	if result != nil {
		t.Errorf("nil should return nil, got %s", string(result))
	}
}

func TestConvertToolChoice_SpecificFunction(t *testing.T) {
	choice := models.MustMarshal(map[string]any{
		"type": "function",
		"function": map[string]any{
			"name": "get_weather",
		},
	})
	result := convertOpenAIToolChoiceToAnthropic(choice)
	var m map[string]any
	json.Unmarshal(result, &m)
	if m["type"] != "tool" || m["name"] != "get_weather" {
		t.Errorf("got %v", m)
	}
}

// ============================================================
// Edge cases
// ============================================================

func TestOpenAIToAnthropic_EmptyMessages(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model:    "test",
		Messages: []models.ChatMessage{},
	}
	result, err := OpenAIToAnthropic(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(result.Messages))
	}
}

func TestOpenAIToAnthropic_NoSystem(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model:    "test",
		Messages: []models.ChatMessage{{Role: "user", Content: models.RawString("hi")}},
	}
	result, err := OpenAIToAnthropic(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.System) > 0 && models.ContentText(result.System) != "" {
		t.Errorf("System should be empty/nil, got %v", result.System)
	}
}

func TestOpenAIToAnthropic_AssistantEmptyToolCallArgs(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model: "test",
		Messages: []models.ChatMessage{
			{Role: "user", Content: models.RawString("hi")},
			{
				Role: "assistant",
				ToolCalls: []models.ToolCall{
					{
						ID:   "call_1",
						Type: "function",
						Function: models.ToolCallFunction{
							Name:      "noop",
							Arguments: "", // empty
						},
					},
				},
			},
		},
	}
	result, err := OpenAIToAnthropic(req)
	if err != nil {
		t.Fatal(err)
	}
	assistantMsg := result.Messages[1]
	blocks, ok := models.AnthropicBlocks(assistantMsg.Content)
	if !ok {
		t.Fatalf("expected blocks, got %s", string(assistantMsg.Content))
	}
	// Empty arguments should default to empty object
	if blocks[0].Input == nil {
		t.Error("input should default to {} not nil")
	}
}
