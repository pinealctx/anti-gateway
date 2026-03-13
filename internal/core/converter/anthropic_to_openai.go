package converter

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/SilkageNet/anti-gateway/internal/models"
)

// AnthropicToOpenAI converts an Anthropic messages request to OpenAI chat completion format.
func AnthropicToOpenAI(req *models.AnthropicRequest) (*models.ChatCompletionRequest, error) {
	openaiReq := &models.ChatCompletionRequest{
		Model:       req.Model,
		Stream:      req.Stream,
		Temperature: req.Temperature,
	}
	if req.MaxTokens > 0 {
		openaiReq.MaxTokens = &req.MaxTokens
	}

	var messages []models.ChatMessage

	// 1. Convert system
	if len(req.System) > 0 {
		sysText := extractSystemText(req.System)
		if sysText != "" {
			messages = append(messages, models.ChatMessage{
				Role:    "system",
				Content: models.RawString(sysText),
			})
		}
	}

	// 2. Convert messages
	for _, msg := range req.Messages {
		converted, err := convertAnthropicMessage(msg)
		if err != nil {
			return nil, err
		}
		messages = append(messages, converted...)
	}

	openaiReq.Messages = messages

	// 3. Convert tools
	if len(req.Tools) > 0 {
		for _, t := range req.Tools {
			openaiReq.Tools = append(openaiReq.Tools, models.Tool{
				Type: "function",
				Function: models.ToolFunction{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  t.InputSchema,
				},
			})
		}
	}

	// 4. Convert tool_choice
	openaiReq.ToolChoice = convertToolChoice(req.ToolChoice)

	return openaiReq, nil
}

func extractSystemText(system json.RawMessage) string {
	if len(system) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(system, &s) == nil {
		return s
	}
	var blocks []map[string]any
	if json.Unmarshal(system, &blocks) == nil {
		var parts []string
		for _, block := range blocks {
			if t, ok := block["type"].(string); ok && t == "text" {
				if text, ok := block["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return string(system)
}

func convertAnthropicMessage(msg models.AnthropicMessage) ([]models.ChatMessage, error) {
	var result []models.ChatMessage

	switch msg.Role {
	case "user":
		result = append(result, convertUserMessage(msg)...)
	case "assistant":
		result = append(result, convertAssistantMessage(msg))
	}

	return result, nil
}

func convertUserMessage(msg models.AnthropicMessage) []models.ChatMessage {
	var msgs []models.ChatMessage

	blocks, ok := models.AnthropicBlocks(msg.Content)
	if !ok {
		// Simple string content
		return []models.ChatMessage{{
			Role:    "user",
			Content: models.RawString(models.ContentText(msg.Content)),
		}}
	}

	var textParts []string
	var imageParts []map[string]any

	for _, block := range blocks {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
		case "image":
			if block.Source != nil {
				// Convert to OpenAI image_url format (data URI)
				dataURI := fmt.Sprintf("data:%s;base64,%s", block.Source.MediaType, block.Source.Data)
				imageParts = append(imageParts, map[string]any{
					"type": "image_url",
					"image_url": map[string]any{
						"url": dataURI,
					},
				})
			}
		case "tool_result":
			// Convert to OpenAI tool message
			content := ""
			if len(block.Content) > 0 {
				content = models.ContentText(block.Content)
			}
			msgs = append(msgs, models.ChatMessage{
				Role:       "tool",
				Content:    models.RawString(content),
				ToolCallID: block.ToolUseID,
			})
		}
	}

	if len(imageParts) > 0 {
		// Multi-modal: combine text + images as content parts
		var parts []map[string]any
		if len(textParts) > 0 {
			parts = append(parts, map[string]any{
				"type": "text",
				"text": strings.Join(textParts, "\n"),
			})
		}
		parts = append(parts, imageParts...)
		msgs = append([]models.ChatMessage{{
			Role:    "user",
			Content: models.MustMarshal(parts),
		}}, msgs...)
	} else if len(textParts) > 0 {
		msgs = append([]models.ChatMessage{{
			Role:    "user",
			Content: models.RawString(strings.Join(textParts, "\n")),
		}}, msgs...)
	}

	return msgs
}

func convertAssistantMessage(msg models.AnthropicMessage) models.ChatMessage {
	blocks, ok := models.AnthropicBlocks(msg.Content)
	if !ok {
		return models.ChatMessage{
			Role:    "assistant",
			Content: models.RawString(models.ContentText(msg.Content)),
		}
	}

	var textParts []string
	var toolCalls []models.ToolCall

	for _, block := range blocks {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
		case "thinking":
			// Skip thinking blocks
		case "tool_use":
			inputJSON, _ := json.Marshal(block.Input)
			toolCalls = append(toolCalls, models.ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: models.ToolCallFunction{
					Name:      block.Name,
					Arguments: string(inputJSON),
				},
			})
		}
	}

	result := models.ChatMessage{
		Role:    "assistant",
		Content: models.RawString(strings.Join(textParts, "\n")),
	}
	if len(toolCalls) > 0 {
		result.ToolCalls = toolCalls
	}
	return result
}

func convertToolChoice(choice json.RawMessage) json.RawMessage {
	if len(choice) == 0 {
		return nil
	}
	var s string
	if json.Unmarshal(choice, &s) == nil {
		switch s {
		case "auto":
			return models.MustMarshal("auto")
		case "any":
			return models.MustMarshal("required")
		case "none":
			return models.MustMarshal("none")
		}
	}
	var m map[string]any
	if json.Unmarshal(choice, &m) == nil {
		if t, ok := m["type"].(string); ok && t == "tool" {
			if name, ok := m["name"].(string); ok {
				return models.MustMarshal(map[string]any{
					"type": "function",
					"function": map[string]any{
						"name": name,
					},
				})
			}
		}
	}
	return nil
}
