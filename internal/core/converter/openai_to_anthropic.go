package converter

import (
	"encoding/json"

	"github.com/SilkageNet/anti-gateway/internal/models"
)

// OpenAIToAnthropic converts an OpenAI chat completion request to Anthropic messages format.
func OpenAIToAnthropic(req *models.ChatCompletionRequest) (*models.AnthropicRequest, error) {
	anthReq := &models.AnthropicRequest{
		Model:       req.Model,
		Stream:      req.Stream,
		Temperature: req.Temperature,
	}
	if req.MaxTokens != nil {
		anthReq.MaxTokens = *req.MaxTokens
	}
	if anthReq.MaxTokens == 0 {
		anthReq.MaxTokens = 8192
	}

	// Convert messages: extract system, then user/assistant
	var anthMsgs []models.AnthropicMessage
	for _, msg := range req.Messages {
		switch msg.Role {
		case "system":
			text := contentToString(msg.Content)
			if text != "" {
				anthReq.System = text
			}
		case "user":
			anthMsgs = append(anthMsgs, convertOpenAIUserToAnthropic(msg))
		case "assistant":
			anthMsgs = append(anthMsgs, convertOpenAIAssistantToAnthropic(msg))
		case "tool":
			anthMsgs = append(anthMsgs, convertOpenAIToolToAnthropic(msg))
		}
	}
	anthReq.Messages = anthMsgs

	// Convert tools
	if len(req.Tools) > 0 {
		for _, t := range req.Tools {
			anthReq.Tools = append(anthReq.Tools, models.AnthropicTool{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				InputSchema: t.Function.Parameters,
			})
		}
	}

	// Convert tool_choice
	anthReq.ToolChoice = convertOpenAIToolChoiceToAnthropic(req.ToolChoice)

	return anthReq, nil
}

func convertOpenAIUserToAnthropic(msg models.ChatMessage) models.AnthropicMessage {
	// Check if content is a multi-part array (vision)
	if parts, ok := msg.Content.([]any); ok {
		var blocks []models.AnthropicContentBlock
		for _, part := range parts {
			pm, ok := part.(map[string]any)
			if !ok {
				continue
			}
			ptype, _ := pm["type"].(string)
			switch ptype {
			case "text":
				text, _ := pm["text"].(string)
				blocks = append(blocks, models.AnthropicContentBlock{
					Type: "text",
					Text: text,
				})
			case "image_url":
				if imgURL, ok := pm["image_url"].(map[string]any); ok {
					urlStr, _ := imgURL["url"].(string)
					format, data, ok := parseDataURI(urlStr)
					if ok {
						mediaType := "image/" + format
						blocks = append(blocks, models.AnthropicContentBlock{
							Type: "image",
							Source: &models.ImageSource{
								Type:      "base64",
								MediaType: mediaType,
								Data:      data,
							},
						})
					}
				}
			}
		}
		if len(blocks) > 0 {
			return models.AnthropicMessage{Role: "user", Content: blocks}
		}
	}

	// Simple text
	return models.AnthropicMessage{
		Role:    "user",
		Content: contentToString(msg.Content),
	}
}

func convertOpenAIAssistantToAnthropic(msg models.ChatMessage) models.AnthropicMessage {
	if len(msg.ToolCalls) == 0 {
		return models.AnthropicMessage{
			Role:    "assistant",
			Content: contentToString(msg.Content),
		}
	}

	// Assistant with tool calls → content blocks
	var blocks []models.AnthropicContentBlock
	text := contentToString(msg.Content)
	if text != "" {
		blocks = append(blocks, models.AnthropicContentBlock{
			Type: "text",
			Text: text,
		})
	}
	for _, tc := range msg.ToolCalls {
		var input any
		if tc.Function.Arguments != "" {
			json.Unmarshal([]byte(tc.Function.Arguments), &input)
		}
		if input == nil {
			input = map[string]any{}
		}
		blocks = append(blocks, models.AnthropicContentBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: input,
		})
	}
	return models.AnthropicMessage{Role: "assistant", Content: blocks}
}

func convertOpenAIToolToAnthropic(msg models.ChatMessage) models.AnthropicMessage {
	content := contentToString(msg.Content)
	return models.AnthropicMessage{
		Role: "user",
		Content: []models.AnthropicContentBlock{
			{
				Type:      "tool_result",
				ToolUseID: msg.ToolCallID,
				Content:   content,
			},
		},
	}
}

func convertOpenAIToolChoiceToAnthropic(choice any) any {
	if choice == nil {
		return nil
	}
	switch v := choice.(type) {
	case string:
		switch v {
		case "auto":
			return map[string]any{"type": "auto"}
		case "required":
			return map[string]any{"type": "any"}
		case "none":
			return nil
		}
	case map[string]any:
		if fn, ok := v["function"].(map[string]any); ok {
			if name, ok := fn["name"].(string); ok {
				return map[string]any{
					"type": "tool",
					"name": name,
				}
			}
		}
	}
	return nil
}
