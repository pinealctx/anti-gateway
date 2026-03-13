package openai

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

	"github.com/SilkageNet/anti-gateway/internal/core/providers"
	"github.com/SilkageNet/anti-gateway/internal/models"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	defaultTimeout = 300 * time.Second
	maxRetries     = 3
)

var retryBackoff = []time.Duration{1 * time.Second, 3 * time.Second, 10 * time.Second}

// Provider implements AIProvider for any OpenAI-compatible API endpoint.
// This covers: OpenAI official, Azure OpenAI, OneAPI, NewAPI, vLLM, Ollama, etc.
type Provider struct {
	name         string
	baseURL      string
	apiKey       string
	defaultModel string
	client       *http.Client
	logger       *zap.Logger
}

// Config holds provider initialization parameters.
type Config struct {
	Name         string
	BaseURL      string // e.g. "https://api.openai.com/v1"
	APIKey       string
	DefaultModel string
	Logger       *zap.Logger
}

func NewProvider(cfg Config) *Provider {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
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
	req.Stream = false

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	respBody, err := p.doWithRetry(ctx, "POST", "/chat/completions", body)
	if err != nil {
		return nil, err
	}
	defer respBody.Close()

	var resp models.ChatCompletionResponse
	if err := json.NewDecoder(respBody).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &resp, nil
}

func (p *Provider) StreamCompletion(ctx context.Context, req *models.ChatCompletionRequest, stream chan<- providers.StreamChunk) error {
	defer close(stream)

	if req.Model == "" && p.defaultModel != "" {
		req.Model = p.defaultModel
	}
	req.Stream = true

	body, err := json.Marshal(req)
	if err != nil {
		stream <- providers.StreamChunk{Error: fmt.Errorf("marshal request: %w", err)}
		return err
	}

	respBody, err := p.doWithRetry(ctx, "POST", "/chat/completions", body)
	if err != nil {
		stream <- providers.StreamChunk{Error: err}
		return err
	}
	defer respBody.Close()

	scanner := bufio.NewScanner(respBody)
	scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			return nil
		}

		var chunk models.ChatCompletionChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			p.logger.Warn("skip malformed SSE chunk", zap.Error(err))
			continue
		}

		for _, choice := range chunk.Choices {
			sc := providers.StreamChunk{}

			if choice.Delta.Content != "" {
				sc.Content = choice.Delta.Content
			}
			if len(choice.Delta.ToolCalls) > 0 {
				sc.ToolCalls = choice.Delta.ToolCalls
			}
			if choice.FinishReason != nil && *choice.FinishReason != "" {
				sc.FinishReason = *choice.FinishReason
			}

			if sc.Content != "" || len(sc.ToolCalls) > 0 || sc.FinishReason != "" {
				stream <- sc
			}
		}
	}

	return scanner.Err()
}

func (p *Provider) RefreshToken(_ context.Context) error {
	return nil // API key based, no refresh needed
}

func (p *Provider) IsHealthy(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/models", nil)
	if err != nil {
		return false
	}
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// doWithRetry performs an HTTP request with retries on 5xx errors.
func (p *Provider) doWithRetry(ctx context.Context, method, path string, body []byte) (io.ReadCloser, error) {
	url := p.baseURL + path

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := retryBackoff[attempt-1]
			p.logger.Info("retrying request",
				zap.String("provider", p.name),
				zap.Int("attempt", attempt),
				zap.Duration("backoff", backoff),
			)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		httpReq, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if p.apiKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
		}

		resp, err := p.client.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			continue
		}

		if resp.StatusCode >= 500 {
			respBytes, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("upstream %d: %s", resp.StatusCode, string(respBytes))
			continue
		}

		if resp.StatusCode >= 400 {
			respBytes, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("upstream error %d: %s", resp.StatusCode, string(respBytes))
		}

		return resp.Body, nil
	}

	return nil, fmt.Errorf("all retries exhausted for %s: %w", p.name, lastErr)
}

// FetchModels queries the /models endpoint and returns available model IDs.
func (p *Provider) FetchModels(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	ids := make([]string, len(result.Data))
	for i, m := range result.Data {
		ids[i] = m.ID
	}
	return ids, nil
}

// GenerateRequestID returns a unique request ID for this provider.
func (p *Provider) GenerateRequestID() string {
	return "chatcmpl-" + uuid.New().String()[:8]
}

// CreateEmbedding implements EmbeddingProvider.
func (p *Provider) CreateEmbedding(ctx context.Context, req *models.EmbeddingRequest) (*models.EmbeddingResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request: %w", err)
	}

	respBody, err := p.doWithRetry(ctx, "POST", "/embeddings", body)
	if err != nil {
		return nil, err
	}
	defer respBody.Close()

	var resp models.EmbeddingResponse
	if err := json.NewDecoder(respBody).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	return &resp, nil
}
