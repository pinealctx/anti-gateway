package converter

import (
	"strings"
	"testing"

	"github.com/pinealctx/anti-gateway/internal/models"
)

// ============================================================
// Converter benchmarks
// ============================================================

func BenchmarkResolveModel(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ResolveModel("gpt-4o")
	}
}

func BenchmarkOpenAIToCW_Simple(b *testing.B) {
	req := &models.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []models.ChatMessage{
			{Role: "user", Content: models.RawString("Hello, how are you?")},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		OpenAIToCW(req, "")
	}
}

func BenchmarkOpenAIToCW_LargeHistory(b *testing.B) {
	msgs := make([]models.ChatMessage, 0, 102)
	msgs = append(msgs, models.ChatMessage{Role: "system", Content: models.RawString("You are a helpful assistant.")})
	for i := 0; i < 50; i++ {
		msgs = append(msgs, models.ChatMessage{Role: "user", Content: models.RawString("Tell me about topic " + strings.Repeat("x", 200))})
		msgs = append(msgs, models.ChatMessage{Role: "assistant", Content: models.RawString("Here's info about topic " + strings.Repeat("y", 500))})
	}
	msgs = append(msgs, models.ChatMessage{Role: "user", Content: models.RawString("final question")})
	req := &models.ChatCompletionRequest{Model: "gpt-4", Messages: msgs}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		OpenAIToCW(req, "")
	}
}

func BenchmarkOpenAIToCW_WithTools(b *testing.B) {
	tools := make([]models.Tool, 10)
	for i := range tools {
		tools[i] = models.Tool{
			Type: "function",
			Function: models.ToolFunction{
				Name:        "tool_" + strings.Repeat("a", 10),
				Description: strings.Repeat("d", 500),
				Parameters:  models.MustMarshal(map[string]any{"type": "object"}),
			},
		}
	}
	req := &models.ChatCompletionRequest{
		Model:    "gpt-4",
		Messages: []models.ChatMessage{{Role: "user", Content: models.RawString("call a tool")}},
		Tools:    tools,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		OpenAIToCW(req, "")
	}
}

func BenchmarkAnthropicToOpenAI(b *testing.B) {
	req := &models.AnthropicRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []models.AnthropicMessage{
			{Role: "user", Content: models.MustMarshal([]models.AnthropicContentBlock{{Type: "text", Text: "Hello"}})},
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		AnthropicToOpenAI(req)
	}
}
