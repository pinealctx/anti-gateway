package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/SilkageNet/anti-gateway/internal/core/providers"
	"github.com/SilkageNet/anti-gateway/internal/models"
	"go.uber.org/zap"
)

func testLogger() *zap.Logger {
	l, _ := zap.NewDevelopment()
	return l
}

// ============================================================
// ChatCompletion (non-streaming)
// ============================================================

func TestChatCompletion_Success(t *testing.T) {
	resp := models.ChatCompletionResponse{
		ID:    "chatcmpl-123",
		Model: "gpt-4",
		Choices: []models.ChatCompletionChoice{
			{Index: 0, Message: models.ChatMessage{Role: "assistant", Content: "Hello!"}, FinishReason: "stop"},
		},
		Usage: &models.Usage{PromptTokens: 5, CompletionTokens: 2, TotalTokens: 7},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing auth header")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("missing content-type")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p := NewProvider(Config{
		Name:    "test",
		BaseURL: srv.URL,
		APIKey:  "test-key",
		Logger:  testLogger(),
	})

	got, err := p.ChatCompletion(context.Background(), &models.ChatCompletionRequest{
		Model:    "gpt-4",
		Messages: []models.ChatMessage{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "chatcmpl-123" {
		t.Errorf("id = %q, want chatcmpl-123", got.ID)
	}
	if len(got.Choices) != 1 || got.Choices[0].Message.Content != "Hello!" {
		t.Error("unexpected response content")
	}
}

func TestChatCompletion_DefaultModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req models.ChatCompletionRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Model != "gpt-4o" {
			t.Errorf("model = %q, want gpt-4o", req.Model)
		}
		json.NewEncoder(w).Encode(models.ChatCompletionResponse{ID: "ok"})
	}))
	defer srv.Close()

	p := NewProvider(Config{
		Name:         "test",
		BaseURL:      srv.URL,
		DefaultModel: "gpt-4o",
		Logger:       testLogger(),
	})

	_, err := p.ChatCompletion(context.Background(), &models.ChatCompletionRequest{
		Messages: []models.ChatMessage{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestChatCompletion_ClientError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"error": "bad request"}`))
	}))
	defer srv.Close()

	p := NewProvider(Config{Name: "test", BaseURL: srv.URL, Logger: testLogger()})
	_, err := p.ChatCompletion(context.Background(), &models.ChatCompletionRequest{
		Model: "gpt-4", Messages: []models.ChatMessage{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error on 400")
	}
}

func TestChatCompletion_ServerErrorRetries(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts <= 2 {
			w.WriteHeader(500)
			w.Write([]byte("internal error"))
			return
		}
		json.NewEncoder(w).Encode(models.ChatCompletionResponse{ID: "retry-ok"})
	}))
	defer srv.Close()

	// Override retry backoffs to speed up test
	origBackoff := retryBackoff
	retryBackoff = []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond}
	defer func() { retryBackoff = origBackoff }()

	p := NewProvider(Config{Name: "test", BaseURL: srv.URL, Logger: testLogger()})
	got, err := p.ChatCompletion(context.Background(), &models.ChatCompletionRequest{
		Model: "gpt-4", Messages: []models.ChatMessage{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("should succeed after retries: %v", err)
	}
	if got.ID != "retry-ok" {
		t.Error("wrong response after retry")
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

// ============================================================
// StreamCompletion
// ============================================================

func TestStreamCompletion_Success(t *testing.T) {
	finish := "stop"
	chunks := []models.ChatCompletionChunk{
		{ID: "c1", Choices: []models.ChatCompletionChunkChoice{
			{Index: 0, Delta: models.ChatCompletionDelta{Content: "Hello"}},
		}},
		{ID: "c2", Choices: []models.ChatCompletionChunkChoice{
			{Index: 0, Delta: models.ChatCompletionDelta{Content: " world"}, FinishReason: &finish},
		}},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for _, c := range chunks {
			data, _ := json.Marshal(c)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	p := NewProvider(Config{Name: "test", BaseURL: srv.URL, Logger: testLogger()})
	stream := make(chan providers.StreamChunk, 10)

	err := p.StreamCompletion(context.Background(), &models.ChatCompletionRequest{
		Model: "gpt-4", Messages: []models.ChatMessage{{Role: "user", Content: "Hi"}},
	}, stream)
	if err != nil {
		t.Fatalf("stream error: %v", err)
	}

	var contents []string
	var gotFinish string
	for sc := range stream {
		if sc.Error != nil {
			t.Fatalf("chunk error: %v", sc.Error)
		}
		if sc.Content != "" {
			contents = append(contents, sc.Content)
		}
		if sc.FinishReason != "" {
			gotFinish = sc.FinishReason
		}
	}

	if len(contents) != 2 || contents[0] != "Hello" || contents[1] != " world" {
		t.Errorf("contents = %v", contents)
	}
	if gotFinish != "stop" {
		t.Errorf("finish = %q, want stop", gotFinish)
	}
}

func TestStreamCompletion_ToolCalls(t *testing.T) {
	chunk := models.ChatCompletionChunk{
		ID: "c1",
		Choices: []models.ChatCompletionChunkChoice{
			{Index: 0, Delta: models.ChatCompletionDelta{
				ToolCalls: []models.ToolCall{
					{ID: "call_1", Type: "function", Function: models.ToolCallFunction{Name: "get_weather", Arguments: `{"city":"NYC"}`}},
				},
			}},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", data)
	}))
	defer srv.Close()

	p := NewProvider(Config{Name: "test", BaseURL: srv.URL, Logger: testLogger()})
	stream := make(chan providers.StreamChunk, 10)

	p.StreamCompletion(context.Background(), &models.ChatCompletionRequest{
		Model: "gpt-4", Messages: []models.ChatMessage{{Role: "user", Content: "weather"}},
	}, stream)

	var gotTools bool
	for sc := range stream {
		if len(sc.ToolCalls) > 0 {
			gotTools = true
			if sc.ToolCalls[0].Function.Name != "get_weather" {
				t.Error("wrong tool name")
			}
		}
	}
	if !gotTools {
		t.Error("expected tool calls in stream")
	}
}

func TestStreamCompletion_MalformedChunk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {invalid json}\n\n")
		chunk := models.ChatCompletionChunk{
			ID:      "ok",
			Choices: []models.ChatCompletionChunkChoice{{Index: 0, Delta: models.ChatCompletionDelta{Content: "ok"}}},
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", data)
	}))
	defer srv.Close()

	p := NewProvider(Config{Name: "test", BaseURL: srv.URL, Logger: testLogger()})
	stream := make(chan providers.StreamChunk, 10)

	err := p.StreamCompletion(context.Background(), &models.ChatCompletionRequest{
		Model: "gpt-4", Messages: []models.ChatMessage{{Role: "user", Content: "Hi"}},
	}, stream)
	if err != nil {
		t.Fatal("malformed chunk should be skipped, not error")
	}

	var gotContent bool
	for sc := range stream {
		if sc.Content == "ok" {
			gotContent = true
		}
	}
	if !gotContent {
		t.Error("should still receive valid chunks after malformed one")
	}
}

// ============================================================
// IsHealthy
// ============================================================

func TestIsHealthy_Up(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("health check hit %q, want /models", r.URL.Path)
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	p := NewProvider(Config{Name: "test", BaseURL: srv.URL, Logger: testLogger()})
	if !p.IsHealthy(context.Background()) {
		t.Error("should be healthy")
	}
}

func TestIsHealthy_Down(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	p := NewProvider(Config{Name: "test", BaseURL: srv.URL, Logger: testLogger()})
	if p.IsHealthy(context.Background()) {
		t.Error("should be unhealthy on 500")
	}
}

func TestIsHealthy_Unreachable(t *testing.T) {
	p := NewProvider(Config{Name: "test", BaseURL: "http://127.0.0.1:1", Logger: testLogger()})
	if p.IsHealthy(context.Background()) {
		t.Error("should be unhealthy when unreachable")
	}
}

// ============================================================
// FetchModels
// ============================================================

func TestFetchModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"data":[{"id":"gpt-4"},{"id":"gpt-3.5-turbo"}]}`))
	}))
	defer srv.Close()

	p := NewProvider(Config{Name: "test", BaseURL: srv.URL, Logger: testLogger()})
	ids, err := p.FetchModels(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 2 || ids[0] != "gpt-4" {
		t.Errorf("models = %v", ids)
	}
}

// ============================================================
// RefreshToken (no-op)
// ============================================================

func TestRefreshToken_NoOp(t *testing.T) {
	p := NewProvider(Config{Name: "test", BaseURL: "http://noop", Logger: testLogger()})
	if err := p.RefreshToken(context.Background()); err != nil {
		t.Errorf("RefreshToken should be no-op, got: %v", err)
	}
}

// ============================================================
// Name
// ============================================================

func TestName(t *testing.T) {
	p := NewProvider(Config{Name: "my-openai", BaseURL: "http://x", Logger: testLogger()})
	if p.Name() != "my-openai" {
		t.Errorf("name = %q", p.Name())
	}
}
