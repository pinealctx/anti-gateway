package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pinealctx/anti-gateway/internal/api/routes"
	"github.com/pinealctx/anti-gateway/internal/core/providers"
	"github.com/pinealctx/anti-gateway/internal/models"
	"go.uber.org/zap"
)

// ============================================================
// Mock provider
// ============================================================

type mockProvider struct {
	name        string
	response    *models.ChatCompletionResponse
	chunks      []providers.StreamChunk
	err         error
	healthy     bool
	lastRequest *models.ChatCompletionRequest
}

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) ChatCompletion(_ context.Context, req *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error) {
	m.lastRequest = req
	if m.err != nil {
		return nil, m.err
	}
	return m.response, nil
}
func (m *mockProvider) StreamCompletion(_ context.Context, req *models.ChatCompletionRequest, stream chan<- providers.StreamChunk) error {
	defer close(stream)
	m.lastRequest = req
	for _, c := range m.chunks {
		stream <- c
	}
	return nil
}
func (m *mockProvider) RefreshToken(_ context.Context) error { return nil }
func (m *mockProvider) IsHealthy(_ context.Context) bool     { return m.healthy }

// ============================================================
// Helper to create a test router with mock provider
// ============================================================

func setupRouter(mock *mockProvider, apiKey string) http.Handler {
	logger, _ := zap.NewDevelopment()
	registry := providers.NewRegistry("kiro")
	registry.Register(mock)
	return routes.SetupRouter(routes.RouterConfig{
		Registry: registry,
		Logger:   logger,
		APIKey:   apiKey,
	})
}

func doJSON(handler http.Handler, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

// ============================================================
// Root & Health
// ============================================================

func TestRoot_ReturnsServiceInfo(t *testing.T) {
	mock := &mockProvider{name: "kiro", healthy: true}
	router := setupRouter(mock, "")

	w := doJSON(router, "GET", "/", nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["name"] != "AntiGateway" {
		t.Errorf("name = %v", body["name"])
	}
}

func TestHealth_ReturnsOK(t *testing.T) {
	mock := &mockProvider{name: "kiro", healthy: true}
	router := setupRouter(mock, "")

	w := doJSON(router, "GET", "/health", nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["status"] != "ok" {
		t.Errorf("status = %v", body["status"])
	}
}

// ============================================================
// Auth middleware
// ============================================================

func TestAuth_MissingKey_Returns401(t *testing.T) {
	mock := &mockProvider{name: "kiro", healthy: true}
	router := setupRouter(mock, "test-key")

	w := doJSON(router, "POST", "/v1/chat/completions", map[string]any{
		"model":    "gpt-4",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	}, nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestAuth_InvalidKey_Returns401(t *testing.T) {
	mock := &mockProvider{name: "kiro", healthy: true}
	router := setupRouter(mock, "test-key")

	w := doJSON(router, "POST", "/v1/chat/completions", map[string]string{}, map[string]string{
		"Authorization": "Bearer wrong-key",
	})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestAuth_ValidBearer_Passes(t *testing.T) {
	mock := &mockProvider{
		name:    "kiro",
		healthy: true,
		response: &models.ChatCompletionResponse{
			Choices: []models.ChatCompletionChoice{{
				Message:      models.ChatMessage{Role: "assistant", Content: models.RawString("hello")},
				FinishReason: "stop",
			}},
		},
	}
	router := setupRouter(mock, "test-key")

	w := doJSON(router, "POST", "/v1/chat/completions", map[string]any{
		"model":    "gpt-4",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	}, map[string]string{"Authorization": "Bearer test-key"})
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
}

func TestAuth_XAPIKey_Passes(t *testing.T) {
	mock := &mockProvider{
		name:    "kiro",
		healthy: true,
		response: &models.ChatCompletionResponse{
			Choices: []models.ChatCompletionChoice{{
				Message:      models.ChatMessage{Role: "assistant", Content: models.RawString("hello")},
				FinishReason: "stop",
			}},
		},
	}
	router := setupRouter(mock, "test-key")

	w := doJSON(router, "POST", "/v1/chat/completions", map[string]any{
		"model":    "gpt-4",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	}, map[string]string{"x-api-key": "test-key"})
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestAuth_NoKeyConfigured_PassesAll(t *testing.T) {
	mock := &mockProvider{
		name:    "kiro",
		healthy: true,
		response: &models.ChatCompletionResponse{
			Choices: []models.ChatCompletionChoice{{
				Message:      models.ChatMessage{Role: "assistant", Content: models.RawString("ok")},
				FinishReason: "stop",
			}},
		},
	}
	router := setupRouter(mock, "")

	w := doJSON(router, "POST", "/v1/chat/completions", map[string]any{
		"model":    "gpt-4",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	}, nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (no auth required)", w.Code)
	}
}

// ============================================================
// OpenAI endpoints
// ============================================================

func TestOpenAI_ChatCompletions_NonStream(t *testing.T) {
	mock := &mockProvider{
		name: "kiro",
		response: &models.ChatCompletionResponse{
			ID:    "test-123",
			Model: "claude-opus-4.6",
			Choices: []models.ChatCompletionChoice{{
				Index:        0,
				Message:      models.ChatMessage{Role: "assistant", Content: models.RawString("Hello there!")},
				FinishReason: "stop",
			}},
		},
	}
	router := setupRouter(mock, "")

	w := doJSON(router, "POST", "/v1/chat/completions", map[string]any{
		"model":    "claude-sonnet-4-20250514",
		"messages": []map[string]string{{"role": "user", "content": "say hi"}},
	}, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}

	var resp models.ChatCompletionResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Choices) != 1 {
		t.Fatalf("choices = %d", len(resp.Choices))
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason = %q", resp.Choices[0].FinishReason)
	}
}

func TestOpenAI_ChatCompletions_InvalidBody(t *testing.T) {
	mock := &mockProvider{name: "kiro"}
	router := setupRouter(mock, "")

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestOpenAI_ChatCompletions_Stream_SSE(t *testing.T) {
	mock := &mockProvider{
		name: "kiro",
		chunks: []providers.StreamChunk{
			{Content: "Hello "},
			{Content: "world!"},
			{FinishReason: "stop"},
		},
	}
	router := setupRouter(mock, "")

	w := doJSON(router, "POST", "/v1/chat/completions", map[string]any{
		"model":    "gpt-4",
		"stream":   true,
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	}, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, "data:") {
		t.Errorf("expected SSE data: lines, got:\n%s", body)
	}
	if !strings.Contains(body, "[DONE]") {
		t.Errorf("expected [DONE] marker")
	}
}

func TestOpenAI_Models_ReturnsList(t *testing.T) {
	mock := &mockProvider{name: "kiro"}
	router := setupRouter(mock, "")

	w := doJSON(router, "GET", "/v1/models", nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["object"] != "list" {
		t.Errorf("object = %v", body["object"])
	}
	data, ok := body["data"].([]any)
	if !ok || len(data) == 0 {
		t.Errorf("models list should not be empty")
	}
}

// ============================================================
// Anthropic endpoints
// ============================================================

func TestAnthropic_Messages_NonStream(t *testing.T) {
	mock := &mockProvider{
		name: "kiro",
		response: &models.ChatCompletionResponse{
			Choices: []models.ChatCompletionChoice{{
				Message:      models.ChatMessage{Role: "assistant", Content: models.RawString("Hi from Claude")},
				FinishReason: "stop",
			}},
		},
	}
	router := setupRouter(mock, "")

	w := doJSON(router, "POST", "/v1/messages", map[string]any{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 1024,
		"messages":   []map[string]string{{"role": "user", "content": "hello"}},
	}, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}

	var resp models.AnthropicResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Type != "message" {
		t.Errorf("type = %q, want message", resp.Type)
	}
	if resp.Role != "assistant" {
		t.Errorf("role = %q", resp.Role)
	}
	if len(resp.Content) == 0 {
		t.Fatal("expected content blocks")
	}
	if resp.Content[0].Type != "text" {
		t.Errorf("content[0].type = %q", resp.Content[0].Type)
	}
}

func TestAnthropic_Messages_InvalidBody(t *testing.T) {
	mock := &mockProvider{name: "kiro"}
	router := setupRouter(mock, "")

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestAnthropic_Messages_Stream_SSE(t *testing.T) {
	mock := &mockProvider{
		name: "kiro",
		chunks: []providers.StreamChunk{
			{Content: "stream "},
			{Content: "reply"},
			{FinishReason: "stop"},
		},
	}
	router := setupRouter(mock, "")

	w := doJSON(router, "POST", "/v1/messages", map[string]any{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 1024,
		"stream":     true,
		"messages":   []map[string]string{{"role": "user", "content": "hi"}},
	}, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, "event:") {
		t.Errorf("expected Anthropic SSE event: lines, got:\n%s", body)
	}
}

func TestAnthropic_Messages_WithToolCalls(t *testing.T) {
	mock := &mockProvider{
		name: "kiro",
		response: &models.ChatCompletionResponse{
			Choices: []models.ChatCompletionChoice{{
				Message: models.ChatMessage{
					Role: "assistant",
					ToolCalls: []models.ToolCall{{
						ID:   "call_1",
						Type: "function",
						Function: models.ToolCallFunction{
							Name:      "get_weather",
							Arguments: `{"city":"Tokyo"}`,
						},
					}},
				},
				FinishReason: "tool_calls",
			}},
		},
	}
	router := setupRouter(mock, "")

	w := doJSON(router, "POST", "/v1/messages", map[string]any{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 1024,
		"messages":   []map[string]string{{"role": "user", "content": "weather in Tokyo?"}},
	}, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}

	var resp models.AnthropicResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.StopReason != "tool_use" {
		t.Errorf("stop_reason = %q, want tool_use", resp.StopReason)
	}
	foundToolUse := false
	for _, block := range resp.Content {
		if block.Type == "tool_use" {
			foundToolUse = true
			if block.Name != "get_weather" {
				t.Errorf("tool name = %q", block.Name)
			}
		}
	}
	if !foundToolUse {
		t.Error("expected tool_use content block")
	}
}

// ============================================================
// Anthropic CountTokens
// ============================================================

func TestAnthropic_CountTokens(t *testing.T) {
	mock := &mockProvider{name: "kiro"}
	router := setupRouter(mock, "")

	w := doJSON(router, "POST", "/v1/messages/count_tokens", map[string]any{
		"model":    "claude-sonnet-4-20250514",
		"messages": []map[string]string{{"role": "user", "content": "hello world"}},
	}, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", w.Code, w.Body.String())
	}
}
