package handlers

import (
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pinealctx/anti-gateway/core/continuation"
	"github.com/pinealctx/anti-gateway/core/converter"
	"github.com/pinealctx/anti-gateway/core/providers"
	"github.com/pinealctx/anti-gateway/core/streaming"
	"github.com/pinealctx/anti-gateway/middleware"
	"github.com/pinealctx/anti-gateway/models"
	copilotProvider "github.com/pinealctx/anti-gateway/providers/copilot"
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

func (h *OpenAIHandler) handleNonStream(c *gin.Context, provider providers.AIProvider, req *models.ChatCompletionRequest, _ string) {
	resp, err := provider.ChatCompletion(c.Request.Context(), req)
	if err != nil {
		middleware.ErrorsTotal.WithLabelValues("provider").Inc()
		c.JSON(http.StatusBadGateway, gin.H{
			"error": gin.H{"message": err.Error(), "type": "upstream_error"},
		})
		return
	}
	if resp.Usage != nil {
		middleware.RecordTokenUsage(c, resp.Usage.TotalTokens)
	}
	c.JSON(http.StatusOK, resp)
}

func (h *OpenAIHandler) handleStream(c *gin.Context, provider providers.AIProvider, req *models.ChatCompletionRequest, reqID string) {
	completionID := "chatcmpl-" + reqID
	writer := streaming.NewOpenAISSEWriter(c, req.Model, completionID)

	if err := writer.WriteRoleDelta(); err != nil {
		return
	}

	var fullOutput string
	var hasToolCalls bool
	continueCount := 0

	for continueCount <= continuation.MaxContinuations {
		stream := make(chan providers.StreamChunk, 64)
		go func() {
			if err := provider.StreamCompletion(c.Request.Context(), req, stream); err != nil {
				h.logger.Error("stream completion error", zap.Error(err))
			}
		}()

		truncated := false
		for chunk := range stream {
			if chunk.Error != nil {
				h.logger.Error("stream error", zap.Error(chunk.Error))
				_ = writer.WriteContentDelta("\n\n[Error: " + chunk.Error.Error() + "]")
				_ = writer.WriteFinish("stop")
				return
			}

			if chunk.Content != "" {
				fullOutput += chunk.Content
				if err := writer.WriteContentDelta(chunk.Content); err != nil {
					return
				}
			}

			if len(chunk.ToolCalls) > 0 {
				hasToolCalls = true
				middleware.ToolCallsTotal.Add(float64(len(chunk.ToolCalls)))
				if err := writer.WriteToolCallDelta(chunk.ToolCalls); err != nil {
					return
				}
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
	_ = writer.WriteFinish(finishReason)
}

// Models handles /v1/models — returns available models.
func (h *OpenAIHandler) Models(c *gin.Context) {
	now := time.Now().Unix()
	owners := make(map[string]string)

	addModel := func(id, owner string) {
		if id == "" {
			return
		}
		if old, ok := owners[id]; ok && old != owner {
			owners[id] = "multi"
			return
		}
		owners[id] = owner
	}

	// Kiro externally maintained model list
	for _, id := range converter.KiroSupportedModels {
		addModel(id, "kiro")
		addModel("kiro/"+id, "kiro")
	}

	// Copilot externally maintained list
	for _, id := range copilotProvider.DefaultSupportedModels {
		addModel(id, "copilot")
		addModel("copilot/"+id, "copilot")
	}

	// Merge runtime-fetched models from configured Copilot providers (best-effort)
	for name, p := range h.registry.All() {
		if cp, ok := p.(*copilotProvider.Provider); ok {
			for _, id := range cp.SupportedModels() {
				addModel(id, "copilot")
				addModel(name+"/"+id, "copilot")
			}
		}
	}

	ids := make([]string, 0, len(owners))
	for id := range owners {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	modelList := make([]gin.H, 0, len(ids))
	for _, id := range ids {
		modelList = append(modelList, gin.H{
			"id":       id,
			"object":   "model",
			"created":  now,
			"owned_by": owners[id],
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
