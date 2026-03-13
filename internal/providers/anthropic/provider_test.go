package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SilkageNet/anti-gateway/internal/core/providers"
	"github.com/SilkageNet/anti-gateway/internal/models"
	"go.uber.org/zap"
)

func testProvider(url string) *Provider {
	logger, _ := zap.NewDevelopment()
	return NewProvider(Config{
		Name:    "test-anthropic",
		BaseURL: url,
		APIKey:  "test-key",
		Logger:  logger,
	})
}

// ============================================================
// Constructor
// ============================================================

func TestNewProvider_Defaults(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	p := NewProvider(Config{Name: "x", APIKey: "k", Logger: logger})
	if p.baseURL != defaultBaseURL {
		t.Errorf("baseURL = %s, want %s", p.baseURL, defaultBaseURL)
	}
	if p.Name() != "x" {
		t.Errorf("Name = %s, want x", p.Name())
	}
}

func TestNewProvider_CustomBaseURL(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	p := NewProvider(Config{Name: "x", BaseURL: "https://custom.api/", APIKey: "k", Logger: logger})
	if p.baseURL != "https://custom.api" {
		t.Errorf("baseURL = %s (should strip trailing slash)", p.baseURL)
	}
}

func TestProvider_IsHealthy(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	p := NewProvider(Config{Name: "x", APIKey: "sk-test", Logger: logger})
	if !p.IsHealthy(context.TODO()) {
		t.Error("should be healthy with API key")
	}

	p2 := NewProvider(Config{Name: "x", APIKey: "", Logger: logger})
	if p2.IsHealthy(context.TODO()) {
		t.Error("should not be healthy without API key")
	}
}

func TestProvider_RefreshToken(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	p := NewProvider(Config{Name: "x", APIKey: "k", Logger: logger})
	if err := p.RefreshToken(context.TODO()); err != nil {
		t.Error("RefreshToken should be no-op")
	}
}

// ============================================================
// ChatCompletion with mock server
// ============================================================

func TestChatCompletion_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers
		if r.Header.Get("X-API-Key") != "test-key" {
			t.Errorf("X-API-Key = %s", r.Header.Get("X-API-Key"))
		}
		if r.Header.Get("Anthropic-Version") != defaultAPIVersion {
			t.Errorf("Anthropic-Version = %s", r.Header.Get("Anthropic-Version"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(models.AnthropicResponse{
			ID:   "msg_test",
			Type: "message",
			Role: "assistant",
			Content: []models.AnthropicContentBlock{
				{Type: "text", Text: "Hello from Anthropic!"},
			},
			StopReason: "end_turn",
			Usage:      models.AnthropicUsage{InputTokens: 10, OutputTokens: 5},
		})
	}))
	defer server.Close()

	p := testProvider(server.URL)
	resp, err := p.ChatCompletion(context.Background(), &models.ChatCompletionRequest{
		Model:    "claude-sonnet-4",
		Messages: []models.ChatMessage{{Role: "user", Content: "Hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("expected choices")
	}
	if resp.Choices[0].Message.Content != "Hello from Anthropic!" {
		t.Errorf("content = %v", resp.Choices[0].Message.Content)
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason = %s", resp.Choices[0].FinishReason)
	}
	if resp.Usage.PromptTokens != 10 || resp.Usage.CompletionTokens != 5 {
		t.Errorf("usage = %+v", resp.Usage)
	}
}

func TestChatCompletion_DefaultModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req models.AnthropicRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Model != "claude-opus-4.6" {
			t.Errorf("model = %s, want claude-opus-4.6", req.Model)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(models.AnthropicResponse{
			Content:    []models.AnthropicContentBlock{{Type: "text", Text: "ok"}},
			StopReason: "end_turn",
		})
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	p := NewProvider(Config{
		Name:         "test",
		BaseURL:      server.URL,
		APIKey:       "k",
		DefaultModel: "claude-opus-4.6",
		Logger:       logger,
	})

	_, err := p.ChatCompletion(context.Background(), &models.ChatCompletionRequest{
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestChatCompletion_4xxError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte("bad request"))
	}))
	defer server.Close()

	p := testProvider(server.URL)
	_, err := p.ChatCompletion(context.Background(), &models.ChatCompletionRequest{
		Model:    "test",
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Error("should error on 400")
	}
}

// ============================================================
// StreamCompletion with mock server
// ============================================================

func TestStreamCompletion_Success(t *testing.T) {
	sseData := "event: content_block_delta\ndata: {\"delta\":{\"type\":\"text_delta\",\"text\":\"Hi\"}}\n\n" +
		"event: message_delta\ndata: {\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sseData))
	}))
	defer server.Close()

	p := testProvider(server.URL)
	stream := make(chan providers.StreamChunk, 10)
	err := p.StreamCompletion(context.Background(), &models.ChatCompletionRequest{
		Model:    "test",
		Stream:   true,
		Messages: []models.ChatMessage{{Role: "user", Content: "hi"}},
	}, stream)
	if err != nil {
		t.Fatal(err)
	}

	var chunks []providers.StreamChunk
	for chunk := range stream {
		chunks = append(chunks, chunk)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].Content != "Hi" {
		t.Errorf("chunk 0 content = %s", chunks[0].Content)
	}
	if chunks[1].FinishReason != "stop" {
		t.Errorf("chunk 1 finish = %s", chunks[1].FinishReason)
	}
}

// ============================================================
// parseAnthropicSSE
// ============================================================

func TestParseSSE_ContentBlockDelta(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	p := &Provider{logger: logger}
	data := `{"delta":{"type":"text_delta","text":"Hello!"}}`
	chunk := p.parseAnthropicSSE("content_block_delta", data)
	if chunk == nil || chunk.Content != "Hello!" {
		t.Errorf("expected content 'Hello!', got %+v", chunk)
	}
}

func TestParseSSE_ContentBlockStart_ToolUse(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	p := &Provider{logger: logger}
	data := `{"content_block":{"type":"tool_use","id":"tool_1","name":"get_weather","input":{}}}`
	chunk := p.parseAnthropicSSE("content_block_start", data)
	if chunk == nil || len(chunk.ToolCalls) == 0 {
		t.Fatal("expected tool calls")
	}
	if chunk.ToolCalls[0].ID != "tool_1" || chunk.ToolCalls[0].Function.Name != "get_weather" {
		t.Errorf("tool call = %+v", chunk.ToolCalls[0])
	}
}

func TestParseSSE_MessageDelta_StopReasons(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	p := &Provider{logger: logger}

	tests := []struct {
		reason string
		want   string
	}{
		{"end_turn", "stop"},
		{"tool_use", "tool_calls"},
		{"max_tokens", "length"},
	}

	for _, tc := range tests {
		data := `{"delta":{"stop_reason":"` + tc.reason + `"}}`
		chunk := p.parseAnthropicSSE("message_delta", data)
		if chunk == nil || chunk.FinishReason != tc.want {
			t.Errorf("stop_reason %s → %v, want %s", tc.reason, chunk, tc.want)
		}
	}
}

func TestParseSSE_UnknownEvent(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	p := &Provider{logger: logger}
	chunk := p.parseAnthropicSSE("ping", `{}`)
	if chunk != nil {
		t.Error("unknown event should return nil")
	}
}

func TestParseSSE_InvalidJSON(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	p := &Provider{logger: logger}
	chunk := p.parseAnthropicSSE("content_block_delta", `{invalid}`)
	if chunk != nil {
		t.Error("invalid JSON should return nil")
	}
}

// ============================================================
// convertAnthropicResponseToOpenAI
// ============================================================

func TestConvertResponse_TextOnly(t *testing.T) {
	resp := &models.AnthropicResponse{
		ID:   "msg_1",
		Role: "assistant",
		Content: []models.AnthropicContentBlock{
			{Type: "text", Text: "Hello!"},
		},
		StopReason: "end_turn",
		Usage:      models.AnthropicUsage{InputTokens: 10, OutputTokens: 5},
	}

	result := convertAnthropicResponseToOpenAI(resp, "claude-sonnet-4")
	if result.Model != "claude-sonnet-4" {
		t.Errorf("model = %s", result.Model)
	}
	if len(result.Choices) != 1 {
		t.Fatal("expected 1 choice")
	}
	if result.Choices[0].Message.Content != "Hello!" {
		t.Errorf("content = %v", result.Choices[0].Message.Content)
	}
	if result.Choices[0].FinishReason != "stop" {
		t.Errorf("finish = %s", result.Choices[0].FinishReason)
	}
	if result.Usage.TotalTokens != 15 {
		t.Errorf("total = %d", result.Usage.TotalTokens)
	}
}

func TestConvertResponse_WithToolUse(t *testing.T) {
	resp := &models.AnthropicResponse{
		Content: []models.AnthropicContentBlock{
			{Type: "text", Text: "Let me check."},
			{
				Type:  "tool_use",
				ID:    "tool_1",
				Name:  "search",
				Input: map[string]any{"q": "test"},
			},
		},
		StopReason: "tool_use",
	}

	result := convertAnthropicResponseToOpenAI(resp, "test")
	if result.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("finish = %s, want tool_calls", result.Choices[0].FinishReason)
	}
	if len(result.Choices[0].Message.ToolCalls) != 1 {
		t.Fatal("expected 1 tool call")
	}
	tc := result.Choices[0].Message.ToolCalls[0]
	if tc.ID != "tool_1" || tc.Function.Name != "search" {
		t.Errorf("tool call = %+v", tc)
	}
}

func TestConvertResponse_MaxTokens(t *testing.T) {
	resp := &models.AnthropicResponse{
		Content:    []models.AnthropicContentBlock{{Type: "text", Text: "partial"}},
		StopReason: "max_tokens",
	}
	result := convertAnthropicResponseToOpenAI(resp, "test")
	if result.Choices[0].FinishReason != "length" {
		t.Errorf("finish = %s, want length", result.Choices[0].FinishReason)
	}
}
