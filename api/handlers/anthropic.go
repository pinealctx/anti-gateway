package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pinealctx/anti-gateway/core/continuation"
	"github.com/pinealctx/anti-gateway/core/converter"
	"github.com/pinealctx/anti-gateway/core/providers"
	"github.com/pinealctx/anti-gateway/core/streaming"
	"github.com/pinealctx/anti-gateway/middleware"
	"github.com/pinealctx/anti-gateway/models"
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

// setAnthropicHeaders sets standard Anthropic response headers expected by Claude Code SDK.
func setAnthropicHeaders(c *gin.Context, reqID string) {
	c.Header("anthropic-version", "2023-06-01")
	c.Header("request-id", reqID)
	c.Header("x-request-id", reqID)
}

// estimateInputTokens provides a rough token estimate for message_start usage.
func estimateInputTokens(messages []models.ChatMessage) int {
	totalChars := 0
	for _, msg := range messages {
		totalChars += len(models.ContentText(msg.Content))
	}
	// ~4 chars per token + per-message overhead
	return totalChars/4 + len(messages)*4
}

func (h *AnthropicHandler) handleNonStream(c *gin.Context, provider providers.AIProvider, req *models.ChatCompletionRequest, model, reqID string) {
	setAnthropicHeaders(c, reqID)

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
		if content := models.ContentText(choice.Message.Content); content != "" {
			anthropicResp.Content = append(anthropicResp.Content, models.AnthropicContentBlock{
				Type: "text",
				Text: content,
			})
		}
		// Convert tool_calls to Anthropic tool_use content blocks
		for _, tc := range choice.Message.ToolCalls {
			inputRaw := json.RawMessage(tc.Function.Arguments)
			if !json.Valid(inputRaw) || len(inputRaw) == 0 {
				inputRaw = json.RawMessage(`{}`)
			}
			anthropicResp.Content = append(anthropicResp.Content, models.AnthropicContentBlock{
				Type:  "tool_use",
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: inputRaw,
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

	if resp.Usage != nil {
		middleware.RecordTokenUsage(c, resp.Usage.TotalTokens)
	}

	c.JSON(http.StatusOK, anthropicResp)
}

func (h *AnthropicHandler) handleStream(c *gin.Context, provider providers.AIProvider, req *models.ChatCompletionRequest, model, reqID string) {
	setAnthropicHeaders(c, reqID)

	inputTokens := estimateInputTokens(req.Messages)
	writer := streaming.NewAnthropicSSEWriter(c, model, reqID)

	if err := writer.WriteMessageStart(inputTokens); err != nil {
		return
	}

	var fullOutput string
	textStarted := false
	textClosed := false
	hasToolUse := false
	continueCount := 0
	toolBlockOpen := false // track whether a tool_use content block is open

	for continueCount <= continuation.MaxContinuations {
		stream := make(chan providers.StreamChunk, 64)
		go func() {
			if err := provider.StreamCompletion(c.Request.Context(), req, stream); err != nil {
				if c.Request.Context().Err() != nil {
					h.logger.Debug("stream completion canceled (client disconnected)", zap.Error(err))
				} else {
					h.logger.Error("stream completion error", zap.Error(err))
				}
			}
		}()

		truncated := false
		for chunk := range stream {
			if chunk.Error != nil {
				if c.Request.Context().Err() != nil {
					h.logger.Debug("stream chunk error (client disconnected)", zap.Error(chunk.Error))
					break
				}
				h.logger.Error("stream error", zap.Error(chunk.Error))
				if !textStarted {
					_ = writer.WriteContentBlockStart()
					textStarted = true
				}
				_ = writer.WriteContentDelta("\n\n[Error: " + chunk.Error.Error() + "]")
				break
			}

			if chunk.Content != "" {
				// Close tool block if open before starting text
				if toolBlockOpen {
					_ = writer.WriteContentBlockStop()
					toolBlockOpen = false
				}
				if !textStarted {
					if err := writer.WriteContentBlockStart(); err != nil {
						return
					}
					textStarted = true
				}
				fullOutput += chunk.Content
				if err := writer.WriteContentDelta(chunk.Content); err != nil {
					return
				}
			}

			if len(chunk.ToolCalls) > 0 {
				hasToolUse = true

				// Close text block if open
				if textStarted && !textClosed {
					if err := writer.WriteContentBlockStop(); err != nil {
						return
					}
					textClosed = true
				}

				for _, tc := range chunk.ToolCalls {
					// tc.ID != "" means a new tool_call is starting
					// (subsequent deltas for the same call have empty ID)
					if tc.ID != "" {
						// Close previous tool block if open
						if toolBlockOpen {
							if err := writer.WriteContentBlockStop(); err != nil {
								return
							}
						}
						if err := writer.WriteToolUseBlockStart(tc.ID, tc.Function.Name); err != nil {
							return
						}
						toolBlockOpen = true
						middleware.ToolCallsTotal.Inc()
					}

					// Append arguments delta to current block
					if tc.Function.Arguments != "" {
						if err := writer.WriteToolUseInputDelta(tc.Function.Arguments); err != nil {
							return
						}
					}
				}
			}

			if chunk.FinishReason != "" {
				// Close tool block if open
				if toolBlockOpen {
					_ = writer.WriteContentBlockStop()
					toolBlockOpen = false
				}
				if chunk.FinishReason == "length" {
					truncated = true
				}
			}
		}

		// Close any tool block left open (e.g., stream ended without finish_reason)
		if toolBlockOpen {
			_ = writer.WriteContentBlockStop()
			toolBlockOpen = false
		}

		if truncated && !hasToolUse && continueCount < continuation.MaxContinuations && continuation.ShouldAutoContinue(fullOutput, req.Messages) {
			continueCount++
			middleware.AutoContinueTriggered.Inc()
			h.logger.Info("auto-continuing (anthropic)", zap.Int("round", continueCount))
			req.Messages = continuation.BuildContinuationMessages(req.Messages, fullOutput)
			continue
		}

		break
	}

	// Close text block if still open
	if textStarted && !textClosed {
		_ = writer.WriteContentBlockStop()
	}

	stopReason := "end_turn"
	if hasToolUse {
		stopReason = "tool_use"
	}
	outputTokens := len(fullOutput) / 4
	_ = writer.WriteMessageDelta(stopReason, outputTokens)
	_ = writer.WriteMessageStop()
}

// CountTokens handles /v1/messages/count_tokens.
// Estimates token count from message content using a character-based heuristic
// (~4 chars per token for English text, consistent with BPE tokenizer averages).
func (h *AnthropicHandler) CountTokens(c *gin.Context) {
	var req struct {
		Model    string                    `json:"model"`
		System   json.RawMessage           `json:"system,omitempty"`
		Messages []models.AnthropicMessage `json:"messages"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"type":  "error",
			"error": gin.H{"type": "invalid_request_error", "message": err.Error()},
		})
		return
	}

	totalChars := len(string(req.System))
	for _, msg := range req.Messages {
		totalChars += len(string(msg.Content))
	}

	// ~4 chars per token is a reasonable BPE approximation.
	// Add a small overhead for message framing (role tokens, etc.).
	estimated := totalChars/4 + len(req.Messages)*4

	c.JSON(http.StatusOK, gin.H{
		"input_tokens": estimated,
	})
}
