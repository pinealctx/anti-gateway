package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/SilkageNet/anti-gateway/internal/core/continuation"
	"github.com/SilkageNet/anti-gateway/internal/core/converter"
	"github.com/SilkageNet/anti-gateway/internal/core/providers"
	"github.com/SilkageNet/anti-gateway/internal/core/streaming"
	"github.com/SilkageNet/anti-gateway/internal/middleware"
	"github.com/SilkageNet/anti-gateway/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// AnthropicHandler handles /v1/messages requests.
type AnthropicHandler struct {
	registry *providers.Registry
	logger   *zap.Logger
}

func NewAnthropicHandler(registry *providers.Registry, logger *zap.Logger) *AnthropicHandler {
	return &AnthropicHandler{registry: registry, logger: logger}
}

func (h *AnthropicHandler) Messages(c *gin.Context) {
	var req models.AnthropicRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"type":  "error",
			"error": gin.H{"type": "invalid_request_error", "message": "Invalid request body: " + err.Error()},
		})
		return
	}

	// Convert Anthropic → OpenAI format
	openaiReq, err := converter.AnthropicToOpenAI(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"type":  "error",
			"error": gin.H{"type": "invalid_request_error", "message": err.Error()},
		})
		return
	}

	// Route: model prefix > API Key default provider > weighted selection
	hint := middleware.GetDefaultProvider(c)
	provider, ok := h.registry.ResolveWithHint(openaiReq.Model, hint)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"type":  "error",
			"error": gin.H{"type": "api_error", "message": "No available provider for model: " + req.Model},
		})
		return
	}

	// Strip prefix from model so upstream sees clean model name
	_, cleanModel := providers.ParseModelPrefix(openaiReq.Model)
	openaiReq.Model = cleanModel

	reqID := "msg_" + uuid.New().String()[:8]

	if req.Stream {
		h.handleStream(c, provider, openaiReq, req.Model, reqID)
	} else {
		h.handleNonStream(c, provider, openaiReq, req.Model, reqID)
	}
}

func (h *AnthropicHandler) handleNonStream(c *gin.Context, provider providers.AIProvider, req *models.ChatCompletionRequest, model, reqID string) {
	resp, err := provider.ChatCompletion(c.Request.Context(), req)
	if err != nil {
		middleware.ErrorsTotal.WithLabelValues("provider").Inc()
		c.JSON(http.StatusBadGateway, gin.H{
			"type":  "error",
			"error": gin.H{"type": "api_error", "message": err.Error()},
		})
		return
	}

	// Convert OpenAI response → Anthropic format
	anthropicResp := models.AnthropicResponse{
		ID:    reqID,
		Type:  "message",
		Role:  "assistant",
		Model: model,
		Usage: models.AnthropicUsage{},
	}

	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		if content, ok := choice.Message.Content.(string); ok && content != "" {
			anthropicResp.Content = append(anthropicResp.Content, models.AnthropicContentBlock{
				Type: "text",
				Text: content,
			})
		}
		// Convert tool_calls to Anthropic tool_use content blocks
		for _, tc := range choice.Message.ToolCalls {
			var input any
			if tc.Function.Arguments != "" {
				json.Unmarshal([]byte(tc.Function.Arguments), &input)
			}
			if input == nil {
				input = map[string]any{}
			}
			anthropicResp.Content = append(anthropicResp.Content, models.AnthropicContentBlock{
				Type:  "tool_use",
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: input,
			})
		}
		switch choice.FinishReason {
		case "stop":
			anthropicResp.StopReason = "end_turn"
		case "tool_calls":
			anthropicResp.StopReason = "tool_use"
		case "length":
			anthropicResp.StopReason = "max_tokens"
		default:
			anthropicResp.StopReason = "end_turn"
		}
	}

	c.JSON(http.StatusOK, anthropicResp)
}

func (h *AnthropicHandler) handleStream(c *gin.Context, provider providers.AIProvider, req *models.ChatCompletionRequest, model, reqID string) {
	writer := streaming.NewAnthropicSSEWriter(c, model, reqID)

	writer.WriteMessageStart()
	writer.WriteContentBlockStart()

	var fullOutput string
	hasTextContent := false
	hasToolUse := false
	continueCount := 0

	for continueCount <= continuation.MaxContinuations {
		stream := make(chan providers.StreamChunk, 64)
		go provider.StreamCompletion(c.Request.Context(), req, stream)

		truncated := false
		for chunk := range stream {
			if chunk.Error != nil {
				h.logger.Error("stream error", zap.Error(chunk.Error))
				writer.WriteContentDelta("\n\n[Error: " + chunk.Error.Error() + "]")
				break
			}

			if chunk.Content != "" {
				hasTextContent = true
				fullOutput += chunk.Content
				writer.WriteContentDelta(chunk.Content)
			}

			if len(chunk.ToolCalls) > 0 {
				middleware.ToolCallsTotal.Add(float64(len(chunk.ToolCalls)))
				hasToolUse = true

				// Close text block if we had text content
				if hasTextContent {
					writer.WriteContentBlockStop()
					hasTextContent = false
				}

				for _, tc := range chunk.ToolCalls {
					writer.WriteToolUseBlockStart(tc.ID, tc.Function.Name)
					if tc.Function.Arguments != "" {
						writer.WriteToolUseInputDelta(tc.Function.Arguments)
					}
					writer.WriteContentBlockStop()
				}
			}

			if chunk.FinishReason == "length" {
				truncated = true
			}
		}

		if truncated && continueCount < continuation.MaxContinuations && continuation.ShouldAutoContinue(fullOutput, req.Messages) {
			continueCount++
			middleware.AutoContinueTriggered.Inc()
			h.logger.Info("auto-continuing (anthropic)", zap.Int("round", continueCount))
			req.Messages = continuation.BuildContinuationMessages(req.Messages, fullOutput)
			continue
		}

		break
	}

	// Close any remaining open text block
	if hasTextContent && !hasToolUse {
		writer.WriteContentBlockStop()
	}

	stopReason := "end_turn"
	if hasToolUse {
		stopReason = "tool_use"
	}
	writer.WriteMessageDelta(stopReason, 0)
	writer.WriteMessageStop()
}

// CountTokens handles /v1/messages/count_tokens (stub).
func (h *AnthropicHandler) CountTokens(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"input_tokens": 0,
	})
}
