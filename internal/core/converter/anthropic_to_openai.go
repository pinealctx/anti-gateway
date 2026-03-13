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
	if req.System != nil {
		sysText := extractSystemText(req.System)
		if sysText != "" {
			messages = append(messages, models.ChatMessage{
				Role:    "system",
				Content: sysText,
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

func extractSystemText(system any) string {
	switch v := system.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, block := range v {
			if m, ok := block.(map[string]any); ok {
				if t, ok := m["type"].(string); ok && t == "text" {
					if text, ok := m["text"].(string); ok {
						parts = append(parts, text)
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return fmt.Sprintf("%v", system)
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

	blocks := toContentBlocks(msg.Content)
	if blocks == nil {
		// Simple string content
		return []models.ChatMessage{{
			Role:    "user",
			Content: fmt.Sprintf("%v", msg.Content),
		}}
	}

	var textParts []string
	var imageParts []any

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
			if block.Content != nil {
				content = fmt.Sprintf("%v", block.Content)
			}
			msgs = append(msgs, models.ChatMessage{
				Role:       "tool",
				Content:    content,
				ToolCallID: block.ToolUseID,
			})
		}
	}

	if len(imageParts) > 0 {
		// Multi-modal: combine text + images as content parts
		var parts []any
		if len(textParts) > 0 {
			parts = append(parts, map[string]any{
				"type": "text",
				"text": strings.Join(textParts, "\n"),
			})
		}
		parts = append(parts, imageParts...)
		msgs = append([]models.ChatMessage{{
			Role:    "user",
			Content: parts,
		}}, msgs...)
	} else if len(textParts) > 0 {
		msgs = append([]models.ChatMessage{{
			Role:    "user",
			Content: strings.Join(textParts, "\n"),
		}}, msgs...)
	}

	return msgs
}

func convertAssistantMessage(msg models.AnthropicMessage) models.ChatMessage {
	blocks := toContentBlocks(msg.Content)
	if blocks == nil {
		return models.ChatMessage{
			Role:    "assistant",
			Content: fmt.Sprintf("%v", msg.Content),
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
		Content: strings.Join(textParts, "\n"),
	}
	if len(toolCalls) > 0 {
		result.ToolCalls = toolCalls
	}
	return result
}

func convertToolChoice(choice any) any {
	if choice == nil {
		return nil
	}
	switch v := choice.(type) {
	case string:
		switch v {
		case "auto":
			return "auto"
		case "any":
			return "required"
		case "none":
			return "none"
		}
	case map[string]any:
		if t, ok := v["type"].(string); ok && t == "tool" {
			if name, ok := v["name"].(string); ok {
				return map[string]any{
					"type": "function",
					"function": map[string]any{
						"name": name,
					},
				}
			}
		}
	}
	return nil
}

// toContentBlocks attempts to parse content as []AnthropicContentBlock.
func toContentBlocks(content any) []models.AnthropicContentBlock {
	arr, ok := content.([]any)
	if !ok {
		return nil
	}

	var blocks []models.AnthropicContentBlock
	for _, item := range arr {
		data, err := json.Marshal(item)
		if err != nil {
			continue
		}
		var block models.AnthropicContentBlock
		if err := json.Unmarshal(data, &block); err != nil {
			continue
		}
		blocks = append(blocks, block)
	}
	return blocks
}
