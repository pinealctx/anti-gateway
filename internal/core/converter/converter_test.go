package converter

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/SilkageNet/anti-gateway/internal/models"
)

// ============================================================
// ResolveModel
// ============================================================

func TestResolveModel_ExactMatch(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"claude-opus-4.6", "claude-opus-4.6"},
		{"claude-opus-4-6", "claude-opus-4.6"},
		{"claude-sonnet-4.6", "claude-sonnet-4.6"},
		{"claude-opus-4.5", "claude-opus-4.5"},
		{"claude-sonnet-4-5", "claude-sonnet-4.5"},
	}
	for _, tc := range tests {
		if got := ResolveModel(tc.input); got != tc.want {
			t.Errorf("ResolveModel(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestResolveModel_AliasToDefault(t *testing.T) {
	aliases := []string{
		"gpt-4o", "gpt-4", "gpt-3.5-turbo", "auto",
		"claude-sonnet-4-20250514", "claude-haiku-4.5",
		"deepseek-3.2", "kimi-k2.5", "glm-4.7",
	}
	for _, alias := range aliases {
		got := ResolveModel(alias)
		if got != "claude-opus-4.6" {
			t.Errorf("ResolveModel(%q) = %q, want claude-opus-4.6", alias, got)
		}
	}
}

func TestResolveModel_UnknownFallsToDefault(t *testing.T) {
	if got := ResolveModel("totally-unknown-model"); got != DefaultModel {
		t.Errorf("ResolveModel(unknown) = %q, want %q", got, DefaultModel)
	}
	if got := ResolveModel(""); got != DefaultModel {
		t.Errorf("ResolveModel(\"\") = %q, want %q", got, DefaultModel)
	}
}

// ============================================================
// OpenAIToCW - basic
// ============================================================

func TestOpenAIToCW_SimpleUserMessage(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model: "claude-opus-4.6",
		Messages: []models.ChatMessage{
			{Role: "user", Content: models.RawString("Hello")},
		},
	}
	cw, err := OpenAIToCW(req, "arn:test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cw.ProfileArn != "arn:test" {
		t.Errorf("ProfileArn = %q, want arn:test", cw.ProfileArn)
	}
	if cw.ConversationState.ConversationID == "" {
		t.Error("ConversationID should not be empty")
	}
	// Should have 2 history entries (system injection pair) + current message
	if len(cw.ConversationState.History) < 2 {
		t.Fatalf("expected at least 2 history entries (system pair), got %d", len(cw.ConversationState.History))
	}
	// Current message should be "Hello"
	if cw.ConversationState.CurrentMessage.UserInputMessage.Content != "Hello" {
		t.Errorf("current content = %q, want Hello", cw.ConversationState.CurrentMessage.UserInputMessage.Content)
	}
}

func TestOpenAIToCW_SystemExtracted(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model: "claude-opus-4.6",
		Messages: []models.ChatMessage{
			{Role: "system", Content: models.RawString("You are a helpful coder.")},
			{Role: "user", Content: models.RawString("Write Go")},
		},
	}
	cw, err := OpenAIToCW(req, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// First history entry should contain the anti-injection prefix AND user system
	firstEntry := cw.ConversationState.History[0]
	if firstEntry.UserInputMessage == nil {
		t.Fatal("first history entry should be a UserInputMessage")
	}
	if !strings.Contains(firstEntry.UserInputMessage.Content, "You are a helpful coder.") {
		t.Error("system prompt not included in first history entry")
	}
	if !strings.Contains(firstEntry.UserInputMessage.Content, "Claude") {
		t.Error("anti-injection prompt should mention Claude")
	}
}

func TestOpenAIToCW_NoNonSystemMessages_Error(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model: "claude-opus-4.6",
		Messages: []models.ChatMessage{
			{Role: "system", Content: models.RawString("system only")},
		},
	}
	_, err := OpenAIToCW(req, "")
	if err == nil {
		t.Error("expected error for no non-system messages")
	}
}

func TestOpenAIToCW_ToolsConverted(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model: "claude-opus-4.6",
		Messages: []models.ChatMessage{
			{Role: "user", Content: models.RawString("use tool")},
		},
		Tools: []models.Tool{
			{Type: "function", Function: models.ToolFunction{Name: "get_weather", Description: "Get weather", Parameters: models.MustMarshal(map[string]any{"type": "object"})}},
			{Type: "function", Function: models.ToolFunction{Name: "web_search", Description: "Search web"}},
		},
	}
	cw, err := OpenAIToCW(req, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ctx := cw.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext
	if ctx == nil {
		t.Fatal("expected UserInputMessageContext with tools")
	}
	// web_search should be filtered out
	if len(ctx.Tools) != 1 {
		t.Fatalf("expected 1 tool (web_search filtered), got %d", len(ctx.Tools))
	}
	if ctx.Tools[0].ToolSpecification.Name != "get_weather" {
		t.Errorf("tool name = %q, want get_weather", ctx.Tools[0].ToolSpecification.Name)
	}
}

func TestOpenAIToCW_ToolResultTruncated(t *testing.T) {
	longContent := strings.Repeat("x", 60000)
	req := &models.ChatCompletionRequest{
		Model: "claude-opus-4.6",
		Messages: []models.ChatMessage{
			{Role: "user", Content: models.RawString("call tool")},
			{Role: "assistant", Content: models.RawString("ok"), ToolCalls: []models.ToolCall{{ID: "tc1", Type: "function", Function: models.ToolCallFunction{Name: "test", Arguments: "{}"}}}},
			{Role: "tool", Content: models.RawString(longContent), ToolCallID: "tc1"},
			{Role: "user", Content: models.RawString("continue")},
		},
	}
	cw, err := OpenAIToCW(req, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Tool result should be in the current message context, truncated to 50000
	ctx := cw.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext
	if ctx == nil || len(ctx.ToolResults) == 0 {
		t.Fatal("expected tool results")
	}
	resultText := ctx.ToolResults[0].Content[0].Text
	if len(resultText) > 50000 {
		t.Errorf("tool result should be truncated to 50000, got %d", len(resultText))
	}
}

func TestOpenAIToCW_MultiTurnHistory(t *testing.T) {
	req := &models.ChatCompletionRequest{
		Model: "claude-opus-4.6",
		Messages: []models.ChatMessage{
			{Role: "user", Content: models.RawString("first")},
			{Role: "assistant", Content: models.RawString("response1")},
			{Role: "user", Content: models.RawString("second")},
			{Role: "assistant", Content: models.RawString("response2")},
			{Role: "user", Content: models.RawString("third")},
		},
	}
	cw, err := OpenAIToCW(req, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 2 (system pair) + 4 (user+assistant x2) = 6 history entries
	// Current message = "third"
	if cw.ConversationState.CurrentMessage.UserInputMessage.Content != "third" {
		t.Errorf("current = %q, want third", cw.ConversationState.CurrentMessage.UserInputMessage.Content)
	}
	// Should have at least 6 history entries
	if len(cw.ConversationState.History) < 6 {
		t.Errorf("expected at least 6 history entries, got %d", len(cw.ConversationState.History))
	}
}

// ============================================================
// contentToString
// ============================================================

func TestContentToString_String(t *testing.T) {
	if got := contentToString(models.RawString("hello")); got != "hello" {
		t.Errorf("contentToString(string) = %q, want hello", got)
	}
}

func TestContentToString_Nil(t *testing.T) {
	if got := contentToString(nil); got != "" {
		t.Errorf("contentToString(nil) = %q, want empty", got)
	}
}

func TestContentToString_ContentParts(t *testing.T) {
	parts := models.MustMarshal([]any{
		map[string]any{"type": "text", "text": "part1"},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:..."}},
		map[string]any{"type": "text", "text": "part2"},
	})
	got := contentToString(parts)
	if got != "part1\npart2" {
		t.Errorf("contentToString(parts) = %q, want part1\\npart2", got)
	}
}

// ============================================================
// parseDataURI
// ============================================================

func TestParseDataURI_PNG(t *testing.T) {
	format, data, ok := parseDataURI("data:image/png;base64,iVBORw0KGgo=")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if format != "png" {
		t.Errorf("format = %q, want png", format)
	}
	if data != "iVBORw0KGgo=" {
		t.Errorf("data = %q, want iVBORw0KGgo=", data)
	}
}

func TestParseDataURI_JPEG(t *testing.T) {
	format, _, ok := parseDataURI("data:image/jpeg;base64,/9j/4AAQ=")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if format != "jpg" {
		t.Errorf("format = %q, want jpg (auto-normalized from jpeg)", format)
	}
}

func TestParseDataURI_Invalid(t *testing.T) {
	tests := []string{
		"https://example.com/image.png",
		"data:text/plain;base64,abc",
		"data:image/png;abc", // no ;base64, separator
		"",
	}
	for _, uri := range tests {
		if _, _, ok := parseDataURI(uri); ok {
			t.Errorf("parseDataURI(%q) should return ok=false", uri)
		}
	}
}

// ============================================================
// convertTools
// ============================================================

func TestConvertTools_FiltersWebSearch(t *testing.T) {
	tools := []models.Tool{
		{Function: models.ToolFunction{Name: "good_tool", Description: "ok"}},
		{Function: models.ToolFunction{Name: "web_search", Description: "blocked"}},
		{Function: models.ToolFunction{Name: "WebSearch", Description: "also blocked"}},
		{Function: models.ToolFunction{Name: "another", Description: "ok"}},
	}
	got := convertTools(tools)
	if len(got) != 2 {
		t.Fatalf("expected 2 tools after filter, got %d", len(got))
	}
	names := []string{got[0].ToolSpecification.Name, got[1].ToolSpecification.Name}
	if names[0] != "good_tool" || names[1] != "another" {
		t.Errorf("unexpected tool names: %v", names)
	}
}

func TestConvertTools_TruncatesDescription(t *testing.T) {
	longDesc := strings.Repeat("d", 20000)
	tools := []models.Tool{
		{Function: models.ToolFunction{Name: "t", Description: longDesc}},
	}
	got := convertTools(tools)
	if len(got[0].ToolSpecification.Description) > 10000 {
		t.Error("description should be truncated to 10000")
	}
}

// ============================================================
// AnthropicToOpenAI
// ============================================================

func TestAnthropicToOpenAI_SimpleText(t *testing.T) {
	req := &models.AnthropicRequest{
		Model:  "claude-opus-4.6",
		System: models.RawString("Be helpful"),
		Messages: []models.AnthropicMessage{
			{Role: "user", Content: models.RawString("Hello")},
		},
	}
	got, err := AnthropicToOpenAI(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Model != "claude-opus-4.6" {
		t.Errorf("model = %q", got.Model)
	}
	// Should have system + user = 2 messages
	if len(got.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got.Messages))
	}
	if got.Messages[0].Role != "system" {
		t.Error("first message should be system")
	}
	if got.Messages[1].Role != "user" {
		t.Error("second message should be user")
	}
}

func TestAnthropicToOpenAI_SystemBlocks(t *testing.T) {
	// System as array of blocks
	systemBlocks := models.MustMarshal([]any{
		map[string]any{"type": "text", "text": "Part 1"},
		map[string]any{"type": "text", "text": "Part 2"},
	})
	req := &models.AnthropicRequest{
		Model:  "claude-opus-4.6",
		System: systemBlocks,
		Messages: []models.AnthropicMessage{
			{Role: "user", Content: models.RawString("Hi")},
		},
	}
	got, err := AnthropicToOpenAI(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sysContent := models.ContentText(got.Messages[0].Content)
	if !strings.Contains(sysContent, "Part 1") || !strings.Contains(sysContent, "Part 2") {
		t.Errorf("system should contain both parts, got %q", sysContent)
	}
}

func TestAnthropicToOpenAI_ToolChoice(t *testing.T) {
	tests := []struct {
		input json.RawMessage
		want  string
	}{
		{models.RawString("auto"), "auto"},
		{models.RawString("any"), "required"},
		{models.RawString("none"), "none"},
		{nil, ""},
	}
	for _, tc := range tests {
		got := convertToolChoice(tc.input)
		if tc.want == "" {
			if got != nil {
				t.Errorf("convertToolChoice(%v) = %s, want nil", tc.input, string(got))
			}
		} else {
			var s string
			json.Unmarshal(got, &s)
			if s != tc.want {
				t.Errorf("convertToolChoice(%s) = %s, want %s", string(tc.input), string(got), tc.want)
			}
		}
	}
}

func TestAnthropicToOpenAI_ToolChoiceSpecific(t *testing.T) {
	input := models.MustMarshal(map[string]any{"type": "tool", "name": "my_func"})
	got := convertToolChoice(input)
	var m map[string]any
	json.Unmarshal(got, &m)
	if m["type"] != "function" {
		t.Errorf("type = %v, want function", m["type"])
	}
	fn, _ := m["function"].(map[string]any)
	if fn["name"] != "my_func" {
		t.Errorf("function name = %v, want my_func", fn["name"])
	}
}

func TestAnthropicToOpenAI_AssistantWithToolCalls(t *testing.T) {
	// Assistant message with tool_use blocks
	blocks := models.MustMarshal([]any{
		map[string]any{"type": "text", "text": "Let me search"},
		map[string]any{
			"type":  "tool_use",
			"id":    "tu_1",
			"name":  "get_weather",
			"input": map[string]any{"city": "Tokyo"},
		},
	})
	req := &models.AnthropicRequest{
		Model: "claude-opus-4.6",
		Messages: []models.AnthropicMessage{
			{Role: "user", Content: models.RawString("Weather in Tokyo")},
			{Role: "assistant", Content: blocks},
		},
	}
	got, err := AnthropicToOpenAI(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assistantMsg := got.Messages[1]
	if assistantMsg.Role != "assistant" {
		t.Fatalf("expected assistant, got %q", assistantMsg.Role)
	}
	if len(assistantMsg.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(assistantMsg.ToolCalls))
	}
	tc := assistantMsg.ToolCalls[0]
	if tc.ID != "tu_1" || tc.Function.Name != "get_weather" {
		t.Errorf("tool call = %+v", tc)
	}
	var args map[string]any
	json.Unmarshal([]byte(tc.Function.Arguments), &args)
	if args["city"] != "Tokyo" {
		t.Errorf("arguments parsed = %v", args)
	}
}

func TestAnthropicToOpenAI_ToolResult(t *testing.T) {
	// User message with tool_result block
	resultBlocks := models.MustMarshal([]any{
		map[string]any{
			"type":        "tool_result",
			"tool_use_id": "tu_1",
			"content":     "25°C, sunny",
		},
	})
	req := &models.AnthropicRequest{
		Model: "claude-opus-4.6",
		Messages: []models.AnthropicMessage{
			{Role: "user", Content: resultBlocks},
		},
	}
	got, err := AnthropicToOpenAI(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should produce a tool message
	if len(got.Messages) < 1 {
		t.Fatal("expected at least 1 message")
	}
	toolMsg := got.Messages[0]
	if toolMsg.Role != "tool" {
		t.Errorf("expected role=tool, got %q", toolMsg.Role)
	}
	if toolMsg.ToolCallID != "tu_1" {
		t.Errorf("tool_call_id = %q, want tu_1", toolMsg.ToolCallID)
	}
}

func TestAnthropicToOpenAI_ToolsConverted(t *testing.T) {
	req := &models.AnthropicRequest{
		Model: "claude-opus-4.6",
		Messages: []models.AnthropicMessage{
			{Role: "user", Content: models.RawString("hi")},
		},
		Tools: []models.AnthropicTool{
			{Name: "calc", Description: "Calculator", InputSchema: models.MustMarshal(map[string]any{"type": "object"})},
		},
	}
	got, err := AnthropicToOpenAI(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(got.Tools))
	}
	if got.Tools[0].Function.Name != "calc" {
		t.Errorf("tool name = %q", got.Tools[0].Function.Name)
	}
}
