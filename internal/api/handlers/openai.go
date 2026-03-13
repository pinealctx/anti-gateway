package handlers

import (
	"net/http"
	"time"

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

// OpenAIHandler handles /v1/chat/completions requests.
type OpenAIHandler struct {
	registry *providers.Registry
	logger   *zap.Logger
}

func NewOpenAIHandler(registry *providers.Registry, logger *zap.Logger) *OpenAIHandler {
	return &OpenAIHandler{registry: registry, logger: logger}
}

func (h *OpenAIHandler) ChatCompletions(c *gin.Context) {
	var req models.ChatCompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "Invalid request body: " + err.Error(), "type": "invalid_request_error"},
		})
		return
	}

	// Route: model prefix > API Key default provider > weighted selection
	hint := middleware.GetDefaultProvider(c)
	provider, ok := h.registry.ResolveWithHint(req.Model, hint)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{"message": "No available provider for model: " + req.Model, "type": "server_error"},
		})
		return
	}

	// Strip prefix from model so upstream sees clean model name
	_, cleanModel := providers.ParseModelPrefix(req.Model)
	req.Model = cleanModel

	reqID := uuid.New().String()[:8]

	if req.Stream {
		h.handleStream(c, provider, &req, reqID)
	} else {
		h.handleNonStream(c, provider, &req, reqID)
	}
}

func (h *OpenAIHandler) handleNonStream(c *gin.Context, provider providers.AIProvider, req *models.ChatCompletionRequest, reqID string) {
	resp, err := provider.ChatCompletion(c.Request.Context(), req)
	if err != nil {
		middleware.ErrorsTotal.WithLabelValues("provider").Inc()
		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{"message": err.Error(), "type": "upstream_error"},
		})
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *OpenAIHandler) handleStream(c *gin.Context, provider providers.AIProvider, req *models.ChatCompletionRequest, reqID string) {
	completionID := "chatcmpl-" + reqID
	writer := streaming.NewOpenAISSEWriter(c, req.Model, completionID)

	writer.WriteRoleDelta()

	var fullOutput string
	var hasToolCalls bool
	continueCount := 0

	for continueCount <= continuation.MaxContinuations {
		stream := make(chan providers.StreamChunk, 64)
		go provider.StreamCompletion(c.Request.Context(), req, stream)

		truncated := false
		for chunk := range stream {
			if chunk.Error != nil {
				h.logger.Error("stream error", zap.Error(chunk.Error))
				writer.WriteContentDelta("\n\n[Error: " + chunk.Error.Error() + "]")
				writer.WriteFinish("stop")
				return
			}

			if chunk.Content != "" {
				fullOutput += chunk.Content
				writer.WriteContentDelta(chunk.Content)
			}

			if len(chunk.ToolCalls) > 0 {
				hasToolCalls = true
				middleware.ToolCallsTotal.Add(float64(len(chunk.ToolCalls)))
				writer.WriteToolCallDelta(chunk.ToolCalls)
			}

			if chunk.FinishReason == "length" {
				truncated = true
			}
		}

		// Check if we should auto-continue
		if truncated && continueCount < continuation.MaxContinuations && continuation.ShouldAutoContinue(fullOutput, req.Messages) {
			continueCount++
			middleware.AutoContinueTriggered.Inc()
			h.logger.Info("auto-continuing", zap.Int("round", continueCount))
			req.Messages = continuation.BuildContinuationMessages(req.Messages, fullOutput)
			continue
		}

		break
	}

	finishReason := "stop"
	if hasToolCalls {
		finishReason = "tool_calls"
	}
	writer.WriteFinish(finishReason)
}

// Models handles /v1/models — returns available models.
func (h *OpenAIHandler) Models(c *gin.Context) {
	modelList := []gin.H{}
	for id := range converter.ModelMap {
		modelList = append(modelList, gin.H{
			"id":       id,
			"object":   "model",
			"created":  time.Now().Unix(),
			"owned_by": "anthropic",
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   modelList,
	})
}

// Embeddings handles POST /v1/embeddings.
func (h *OpenAIHandler) Embeddings(c *gin.Context) {
	var req models.EmbeddingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "Invalid request body: " + err.Error(), "type": "invalid_request_error"},
		})
		return
	}

	hint := middleware.GetDefaultProvider(c)
	provider, ok := h.registry.ResolveWithHint(req.Model, hint)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{"message": "No available provider for model: " + req.Model, "type": "server_error"},
		})
		return
	}

	// Check if provider supports embeddings
	ep, ok := provider.(providers.EmbeddingProvider)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "Provider does not support embeddings", "type": "invalid_request_error"},
		})
		return
	}

	// Strip prefix
	_, cleanModel := providers.ParseModelPrefix(req.Model)
	req.Model = cleanModel

	resp, err := ep.CreateEmbedding(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{"message": err.Error(), "type": "upstream_error"},
		})
		return
	}

	c.JSON(http.StatusOK, resp)
}
