package converter

import (
	"encoding/json"

	"github.com/pinealctx/anti-gateway/models"
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
			text := models.ContentText(msg.Content)
			if text != "" {
				anthReq.System = models.RawString(text)
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
	if parts, ok := models.ContentParts(msg.Content); ok {
		var blocks []models.AnthropicContentBlock
		for _, pm := range parts {
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
			return models.AnthropicMessage{Role: "user", Content: models.MustMarshal(blocks)}
		}
	}

	// Simple text
	return models.AnthropicMessage{
		Role:    "user",
		Content: models.RawString(models.ContentText(msg.Content)),
	}
}

func convertOpenAIAssistantToAnthropic(msg models.ChatMessage) models.AnthropicMessage {
	if len(msg.ToolCalls) == 0 {
		return models.AnthropicMessage{
			Role:    "assistant",
			Content: models.RawString(models.ContentText(msg.Content)),
		}
	}

	// Assistant with tool calls → content blocks
	var blocks []models.AnthropicContentBlock
	text := models.ContentText(msg.Content)
	if text != "" {
		blocks = append(blocks, models.AnthropicContentBlock{
			Type: "text",
			Text: text,
		})
	}
	for _, tc := range msg.ToolCalls {
		inputRaw := json.RawMessage(tc.Function.Arguments)
		if !json.Valid(inputRaw) || len(inputRaw) == 0 {
			inputRaw = json.RawMessage(`{}`)
		}
		blocks = append(blocks, models.AnthropicContentBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: inputRaw,
		})
	}
	return models.AnthropicMessage{Role: "assistant", Content: models.MustMarshal(blocks)}
}

func convertOpenAIToolToAnthropic(msg models.ChatMessage) models.AnthropicMessage {
	content := models.ContentText(msg.Content)
	return models.AnthropicMessage{
		Role: "user",
		Content: models.MustMarshal([]models.AnthropicContentBlock{
			{
				Type:      "tool_result",
				ToolUseID: msg.ToolCallID,
				Content:   models.RawString(content),
			},
		}),
	}
}

func convertOpenAIToolChoiceToAnthropic(choice json.RawMessage) json.RawMessage {
	if len(choice) == 0 {
		return nil
	}
	var s string
	if json.Unmarshal(choice, &s) == nil {
		switch s {
		case "auto":
			return models.MustMarshal(map[string]any{"type": "auto"})
		case "required":
			return models.MustMarshal(map[string]any{"type": "any"})
		case "none":
			return nil
		}
	}
	var m map[string]any
	if json.Unmarshal(choice, &m) == nil {
		if fn, ok := m["function"].(map[string]any); ok {
			if name, ok := fn["name"].(string); ok {
				return models.MustMarshal(map[string]any{
					"type": "tool",
					"name": name,
				})
			}
		}
	}
	return nil
}
