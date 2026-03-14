package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pinealctx/anti-gateway/core/converter"
	"github.com/pinealctx/anti-gateway/core/providers"
	"github.com/pinealctx/anti-gateway/models"
	"go.uber.org/zap"
)

const (
	defaultBaseURL    = "https://api.anthropic.com"
	defaultAPIVersion = "2023-06-01"
	defaultTimeout    = 300 * time.Second
	maxRetries        = 3
)

var retryBackoff = []time.Duration{1 * time.Second, 3 * time.Second, 10 * time.Second}

// Config holds provider initialization parameters.
type Config struct {
	Name         string
	BaseURL      string // defaults to "https://api.anthropic.com"
	APIKey       string
	DefaultModel string
	Logger       *zap.Logger
}

// Provider implements AIProvider for the Anthropic Messages API.
// It accepts OpenAI-format requests (internal to the gateway), converts them to
// Anthropic format, calls the Anthropic API, and converts responses back.
type Provider struct {
	name         string
	baseURL      string
	apiKey       string
	defaultModel string
	client       *http.Client
	logger       *zap.Logger
}

func NewProvider(cfg Config) *Provider {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Provider{
		name:         cfg.Name,
		baseURL:      baseURL,
		apiKey:       cfg.APIKey,
		defaultModel: cfg.DefaultModel,
		client:       &http.Client{Timeout: defaultTimeout},
		logger:       cfg.Logger,
	}
}

func (p *Provider) Name() string { return p.name }

func (p *Provider) ChatCompletion(ctx context.Context, req *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error) {
	if req.Model == "" && p.defaultModel != "" {
		req.Model = p.defaultModel
	}

	// Convert OpenAI → Anthropic
	anthReq, err := converter.OpenAIToAnthropic(req)
	if err != nil {
		return nil, fmt.Errorf("convert to anthropic: %w", err)
	}
	anthReq.Stream = false

	body, err := json.Marshal(anthReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	respBody, err := p.doWithRetry(ctx, body)
	if err != nil {
		return nil, err
	}
	defer func() { _ = respBody.Close() }()

	var anthResp models.AnthropicResponse
	if err := json.NewDecoder(respBody).Decode(&anthResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Convert Anthropic response → OpenAI
	return convertAnthropicResponseToOpenAI(&anthResp, req.Model), nil
}

func (p *Provider) StreamCompletion(ctx context.Context, req *models.ChatCompletionRequest, stream chan<- providers.StreamChunk) error {
	defer close(stream)

	if req.Model == "" && p.defaultModel != "" {
		req.Model = p.defaultModel
	}

	anthReq, err := converter.OpenAIToAnthropic(req)
	if err != nil {
		stream <- providers.StreamChunk{Error: fmt.Errorf("convert to anthropic: %w", err)}
		return err
	}
	anthReq.Stream = true

	body, err := json.Marshal(anthReq)
	if err != nil {
		stream <- providers.StreamChunk{Error: fmt.Errorf("marshal request: %w", err)}
		return err
	}

	respBody, err := p.doWithRetry(ctx, body)
	if err != nil {
		stream <- providers.StreamChunk{Error: err}
		return err
	}
	defer func() { _ = respBody.Close() }()

	scanner := bufio.NewScanner(respBody)
	scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)

	var currentEventType string
	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			currentEventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "" {
			continue
		}

		chunk := p.parseAnthropicSSE(currentEventType, data)
		if chunk != nil {
			stream <- *chunk
		}
		currentEventType = ""
	}

	return scanner.Err()
}

func (p *Provider) RefreshToken(_ context.Context) error {
	return nil // API key based
}

func (p *Provider) IsHealthy(ctx context.Context) bool {
	// Anthropic doesn't have a standard health endpoint;
	// we consider it healthy if we have an API key
	return p.apiKey != ""
}

// doWithRetry performs an HTTP request to the Anthropic API with retries.
func (p *Provider) doWithRetry(ctx context.Context, body []byte) (io.ReadCloser, error) {
	url := p.baseURL + "/v1/messages"

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := retryBackoff[attempt-1]
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("X-API-Key", p.apiKey)
		httpReq.Header.Set("Anthropic-Version", defaultAPIVersion)

		resp, err := p.client.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			continue
		}

		if resp.StatusCode >= 500 || resp.StatusCode == 429 {
			respBytes, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("anthropic %d: %s", resp.StatusCode, string(respBytes))
			continue
		}

		if resp.StatusCode >= 400 {
			respBytes, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			return nil, fmt.Errorf("anthropic error %d: %s", resp.StatusCode, string(respBytes))
		}

		return resp.Body, nil
	}

	return nil, fmt.Errorf("anthropic request failed after retries: %w", lastErr)
}

// parseAnthropicSSE parses a single SSE event from the Anthropic stream.
func (p *Provider) parseAnthropicSSE(eventType, data string) *providers.StreamChunk {
	switch eventType {
	case "content_block_delta":
		var evt struct {
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(data), &evt); err != nil {
			return nil
		}
		if evt.Delta.Type == "text_delta" && evt.Delta.Text != "" {
			return &providers.StreamChunk{Content: evt.Delta.Text}
		}

	case "content_block_start":
		var evt struct {
			ContentBlock struct {
				Type  string `json:"type"`
				ID    string `json:"id"`
				Name  string `json:"name"`
				Input any    `json:"input"`
			} `json:"content_block"`
		}
		if err := json.Unmarshal([]byte(data), &evt); err != nil {
			return nil
		}
		if evt.ContentBlock.Type == "tool_use" {
			inputJSON, _ := json.Marshal(evt.ContentBlock.Input)
			return &providers.StreamChunk{
				ToolCalls: []models.ToolCall{
					{
						ID:   evt.ContentBlock.ID,
						Type: "function",
						Function: models.ToolCallFunction{
							Name:      evt.ContentBlock.Name,
							Arguments: string(inputJSON),
						},
					},
				},
			}
		}

	case "message_delta":
		var evt struct {
			Delta struct {
				StopReason string `json:"stop_reason"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(data), &evt); err != nil {
			return nil
		}
		if evt.Delta.StopReason != "" {
			reason := "stop"
			switch evt.Delta.StopReason {
			case "end_turn":
				reason = "stop"
			case "tool_use":
				reason = "tool_calls"
			case "max_tokens":
				reason = "length"
			}
			return &providers.StreamChunk{FinishReason: reason}
		}
	}

	return nil
}

// convertAnthropicResponseToOpenAI converts an Anthropic non-streaming response to OpenAI format.
func convertAnthropicResponseToOpenAI(resp *models.AnthropicResponse, model string) *models.ChatCompletionResponse {
	var content string
	var toolCalls []models.ToolCall

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			content += block.Text
		case "tool_use":
			inputJSON, _ := json.Marshal(block.Input)
			toolCalls = append(toolCalls, models.ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: models.ToolCallFunction{
					Name:      block.Name,
					Arguments: string(inputJSON),
				},
			})
		}
	}

	finishReason := "stop"
	switch resp.StopReason {
	case "end_turn":
		finishReason = "stop"
	case "tool_use":
		finishReason = "tool_calls"
	case "max_tokens":
		finishReason = "length"
	}

	return &models.ChatCompletionResponse{
		ID:      "chatcmpl-" + uuid.New().String()[:8],
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []models.ChatCompletionChoice{
			{
				Index: 0,
				Message: models.ChatMessage{
					Role:      "assistant",
					Content:   models.RawString(content),
					ToolCalls: toolCalls,
				},
				FinishReason: finishReason,
			},
		},
		Usage: &models.Usage{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}
}
