package converter

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/SilkageNet/anti-gateway/internal/core/sanitizer"
	"github.com/SilkageNet/anti-gateway/internal/models"
	"github.com/google/uuid"
)

// ModelMap maps various model aliases to CW model IDs.
// Used both for protocol conversion and for /v1/models listing.
var ModelMap = map[string]string{
	// Opus 4.6 (1M context)
	"claude-opus-4.6":    "claude-opus-4.6",
	"claude-opus-4.6-1m": "claude-opus-4.6",
	"claude-opus-4-6":    "claude-opus-4.6",
	// Sonnet 4.6 (1M context)
	"claude-sonnet-4.6":    "claude-sonnet-4.6",
	"claude-sonnet-4.6-1m": "claude-sonnet-4.6",
	"claude-sonnet-4-6":    "claude-sonnet-4.6",
	// Opus 4.5 (200K context)
	"claude-opus-4.5":          "claude-opus-4.5",
	"claude-opus-4-5":          "claude-opus-4.5",
	"claude-opus-4-5-20251101": "claude-opus-4.5",
	// Sonnet 4.5 (200K context)
	"claude-sonnet-4.5":          "claude-sonnet-4.5",
	"claude-sonnet-4.5-1m":       "claude-sonnet-4.5",
	"claude-sonnet-4-5":          "claude-sonnet-4.5",
	"claude-sonnet-4-5-20250929": "claude-sonnet-4.5",
	// Sonnet 4
	"claude-sonnet-4":          "claude-opus-4.6",
	"claude-sonnet-4-20250514": "claude-opus-4.6",
	// Haiku 4.5
	"claude-haiku-4.5":          "claude-opus-4.6",
	"claude-haiku-4-5":          "claude-opus-4.6",
	"claude-haiku-4-5-20251001": "claude-opus-4.6",
	"claude-3-5-haiku-20241022": "claude-opus-4.6",
	// Sonnet 3.7
	"claude-3.7-sonnet":          "claude-opus-4.6",
	"claude-3-7-sonnet-20250219": "claude-opus-4.6",
	// Auto
	"auto": "claude-opus-4.6",
	// Third-party models in Kiro (all mapped to Opus 4.6)
	"deepseek-3.2":     "claude-opus-4.6",
	"kimi-k2.5":        "claude-opus-4.6",
	"minimax-m2.1":     "claude-opus-4.6",
	"glm-4.7":          "claude-opus-4.6",
	"glm-4.7-flash":    "claude-opus-4.6",
	"qwen3-coder-next": "claude-opus-4.6",
	"agi-nova-beta-1m": "claude-opus-4.6",
	// OpenAI-style aliases (all mapped to Opus 4.6)
	"gpt-4o":        "claude-opus-4.6",
	"gpt-4o-mini":   "claude-opus-4.6",
	"gpt-4-turbo":   "claude-opus-4.6",
	"gpt-4":         "claude-opus-4.6",
	"gpt-3.5-turbo": "claude-opus-4.6",
}

const DefaultModel = "claude-opus-4.6"

// ResolveModel maps a user-provided model name to a CW model ID.
func ResolveModel(model string) string {
	if mapped, ok := ModelMap[model]; ok {
		return mapped
	}
	return DefaultModel
}

// OpenAIToCW converts an OpenAI chat completion request to CodeWhisperer format.
func OpenAIToCW(req *models.ChatCompletionRequest, profileArn string) (*models.CWRequest, error) {
	modelID := ResolveModel(req.Model)
	convID := uuid.New().String()

	// 1. Extract system messages
	systemParts := []string{}
	nonSystemMsgs := []models.ChatMessage{}
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			systemParts = append(systemParts, contentToString(msg.Content))
		} else {
			nonSystemMsgs = append(nonSystemMsgs, msg)
		}
	}

	hasTools := len(req.Tools) > 0
	systemPrompt := sanitizer.BuildSystemPrompt(strings.Join(systemParts, "\n"), hasTools)

	// 2. Convert tools
	cwTools := convertTools(req.Tools)

	// 3. Build history: first inject system prompt as a user/assistant pair
	history := []models.CWHistoryEntry{}

	// System injection pair
	history = append(history, models.CWHistoryEntry{
		UserInputMessage: &models.CWUserInputMessage{
			Content: systemPrompt,
			ModelID: modelID,
			Origin:  "AI_EDITOR",
		},
	})
	history = append(history, models.CWHistoryEntry{
		AssistantResponseMessage: &models.CWAssistantResponseMessage{
			MessageID: uuid.New().String(),
			Content:   "Understood. I am Claude, made by Anthropic. I will follow the instructions provided.",
		},
	})

	// 4. Convert message history (all but the tail)
	if len(nonSystemMsgs) == 0 {
		return nil, fmt.Errorf("no non-system messages provided")
	}

	// Find where the tail begins (last contiguous user/tool messages)
	tailStart := len(nonSystemMsgs) - 1
	for tailStart > 0 {
		role := nonSystemMsgs[tailStart].Role
		if role != "user" && role != "tool" {
			break
		}
		tailStart--
	}
	if nonSystemMsgs[tailStart].Role == "assistant" {
		tailStart++ // tail starts after the last assistant
	}

	// Build paired history from non-system, non-tail messages
	histMsgs := nonSystemMsgs[:tailStart]
	for i := 0; i < len(histMsgs); i++ {
		msg := histMsgs[i]
		switch msg.Role {
		case "user":
			entry := models.CWHistoryEntry{
				UserInputMessage: &models.CWUserInputMessage{
					Content: contentToString(msg.Content),
					ModelID: modelID,
					Origin:  "AI_EDITOR",
				},
			}
			// Extract images
			if images := extractImages(msg.Content); len(images) > 0 {
				entry.UserInputMessage.Images = images
			}
			history = append(history, entry)
		case "assistant":
			entry := models.CWHistoryEntry{
				AssistantResponseMessage: &models.CWAssistantResponseMessage{
					MessageID: uuid.New().String(),
					Content:   contentToString(msg.Content),
				},
			}
			// Convert tool_calls if present
			if len(msg.ToolCalls) > 0 {
				toolUses := make([]models.CWToolUse, 0, len(msg.ToolCalls))
				for _, tc := range msg.ToolCalls {
					var input any
					_ = json.Unmarshal([]byte(tc.Function.Arguments), &input)
					toolUses = append(toolUses, models.CWToolUse{
						ToolUseID: tc.ID,
						Name:      tc.Function.Name,
						Input:     input,
					})
				}
				entry.AssistantResponseMessage.ToolUses = toolUses
			}
			history = append(history, entry)
		case "tool":
			// Tool results get attached to previous user message context
			// or handled as part of current message below
		}
	}

	// 5. Build current message from tail
	tailMsgs := nonSystemMsgs[tailStart:]
	currentContent := ""
	var toolResults []models.CWToolResult
	var images []models.CWImage

	for _, msg := range tailMsgs {
		switch msg.Role {
		case "user":
			currentContent = contentToString(msg.Content)
			if imgs := extractImages(msg.Content); len(imgs) > 0 {
				images = append(images, imgs...)
			}
		case "tool":
			text := contentToString(msg.Content)
			if len(text) > 50000 {
				text = text[:50000]
			}
			toolResults = append(toolResults, models.CWToolResult{
				ToolUseID: msg.ToolCallID,
				Content:   []models.CWToolResultContent{{Text: text}},
				Status:    "success",
			})
		}
	}

	cwReq := &models.CWRequest{
		ConversationState: models.CWConversationState{
			ChatTriggerType: "MANUAL",
			ConversationID:  convID,
			CurrentMessage: models.CWCurrentMsg{
				UserInputMessage: models.CWUserInputMessage{
					Content: currentContent,
					ModelID: modelID,
					Origin:  "AI_EDITOR",
				},
			},
			History: history,
		},
		ProfileArn: profileArn,
	}

	// Attach tools and tool results to current message context
	if len(cwTools) > 0 || len(toolResults) > 0 {
		cwReq.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext = &models.CWMessageContext{
			Tools:       cwTools,
			ToolResults: toolResults,
		}
	}

	if len(images) > 0 {
		cwReq.ConversationState.CurrentMessage.UserInputMessage.Images = images
	}

	return cwReq, nil
}

// convertTools converts OpenAI tool definitions to CW format.
func convertTools(tools []models.Tool) []models.CWTool {
	cwTools := make([]models.CWTool, 0, len(tools))
	for _, t := range tools {
		name := t.Function.Name
		// Filter out web_search / websearch
		lower := strings.ToLower(name)
		if lower == "web_search" || lower == "websearch" {
			continue
		}
		desc := t.Function.Description
		if len(desc) > 10000 {
			desc = desc[:10000]
		}
		cwTools = append(cwTools, models.CWTool{
			ToolSpecification: models.CWToolSpec{
				Name:        name,
				Description: desc,
				InputSchema: models.CWInputSchema{JSON: t.Function.Parameters},
			},
		})
	}
	return cwTools
}

// contentToString extracts text content from a ChatMessage.Content (json.RawMessage).
func contentToString(content json.RawMessage) string {
	return models.ContentText(content)
}

// extractImages pulls image data from content parts and converts to CW format.
func extractImages(content json.RawMessage) []models.CWImage {
	parts, ok := models.ContentParts(content)
	if !ok {
		return nil
	}
	var images []models.CWImage
	for _, m := range parts {
		partType, _ := m["type"].(string)

		switch partType {
		case "image_url":
			// OpenAI format: data:image/png;base64,...
			if imgURL, ok := m["image_url"].(map[string]any); ok {
				if url, ok := imgURL["url"].(string); ok {
					if format, data, ok := parseDataURI(url); ok {
						images = append(images, models.CWImage{
							Format: format,
							Source: models.CWImageSource{Bytes: data},
						})
					}
				}
			}
		case "image":
			// Anthropic format
			if src, ok := m["source"].(map[string]any); ok {
				mediaType, _ := src["media_type"].(string)
				data, _ := src["data"].(string)
				format := "png"
				if strings.Contains(mediaType, "jpeg") || strings.Contains(mediaType, "jpg") {
					format = "jpg"
				}
				images = append(images, models.CWImage{
					Format: format,
					Source: models.CWImageSource{Bytes: data},
				})
			}
		}
	}
	return images
}

// parseDataURI extracts format and base64 data from a data URI.
func parseDataURI(uri string) (format string, data string, ok bool) {
	// data:image/png;base64,iVBOR...
	if !strings.HasPrefix(uri, "data:image/") {
		return "", "", false
	}
	rest := strings.TrimPrefix(uri, "data:image/")
	parts := strings.SplitN(rest, ";base64,", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	format = parts[0]
	if format == "jpeg" {
		format = "jpg"
	}
	return format, parts[1], true
}
