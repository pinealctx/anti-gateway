package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/pinealctx/anti-gateway/api/routes"
	"github.com/pinealctx/anti-gateway/core/providers"
	"github.com/pinealctx/anti-gateway/models"
	"go.uber.org/zap"
)

// ============================================================
// Mock embedding provider
// ============================================================

type mockEmbeddingProvider struct {
	mockProvider
	embeddingResp *models.EmbeddingResponse
	embeddingErr  error
}

func (m *mockEmbeddingProvider) CreateEmbedding(_ context.Context, req *models.EmbeddingRequest) (*models.EmbeddingResponse, error) {
	if m.embeddingErr != nil {
		return nil, m.embeddingErr
	}
	return m.embeddingResp, nil
}

func setupRouterWithEmbeddings(mock *mockEmbeddingProvider, apiKey string) http.Handler {
	logger, _ := zap.NewDevelopment()
	registry := providers.NewRegistry("kiro")
	registry.Register(mock)
	return routes.SetupRouter(routes.RouterConfig{
		Registry: registry,
		Logger:   logger,
		APIKey:   apiKey,
	})
}

// ============================================================
// Embeddings endpoint
// ============================================================

func TestEmbeddings_Success(t *testing.T) {
	mock := &mockEmbeddingProvider{
		mockProvider: mockProvider{name: "kiro", healthy: true},
		embeddingResp: &models.EmbeddingResponse{
			Object: "list",
			Data: []models.EmbeddingData{
				{Object: "embedding", Index: 0, Embedding: []float32{0.1, 0.2, 0.3}},
			},
			Model: "text-embedding-3-small",
			Usage: models.EmbeddingUsage{PromptTokens: 5, TotalTokens: 5},
		},
	}
	handler := setupRouterWithEmbeddings(mock, "test-key")

	w := doJSON(handler, "POST", "/v1/embeddings",
		map[string]any{"input": "Hello", "model": "text-embedding-3-small"},
		map[string]string{"Authorization": "Bearer test-key"},
	)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp models.EmbeddingResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Data) != 1 {
		t.Errorf("expected 1 embedding, got %d", len(resp.Data))
	}
}

func TestEmbeddings_InvalidBody(t *testing.T) {
	mock := &mockEmbeddingProvider{
		mockProvider: mockProvider{name: "kiro", healthy: true},
	}
	handler := setupRouterWithEmbeddings(mock, "test-key")

	w := doJSON(handler, "POST", "/v1/embeddings",
		nil, // empty body
		map[string]string{"Authorization": "Bearer test-key"},
	)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestEmbeddings_ProviderDoesNotSupportEmbeddings(t *testing.T) {
	// Regular mock doesn't implement EmbeddingProvider
	regular := &mockProvider{name: "kiro", healthy: true}
	handler := setupRouter(regular, "test-key")

	w := doJSON(handler, "POST", "/v1/embeddings",
		map[string]any{"input": "Hello", "model": "text-embedding-3-small"},
		map[string]string{"Authorization": "Bearer test-key"},
	)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (no embedding support)", w.Code)
	}
}

func TestEmbeddings_Unauthorized(t *testing.T) {
	mock := &mockEmbeddingProvider{
		mockProvider: mockProvider{name: "kiro", healthy: true},
	}
	handler := setupRouterWithEmbeddings(mock, "test-key")

	w := doJSON(handler, "POST", "/v1/embeddings",
		map[string]any{"input": "Hello", "model": "test"},
		map[string]string{}, // no auth header
	)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}
