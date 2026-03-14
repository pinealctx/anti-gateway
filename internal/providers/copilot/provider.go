package copilot

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pinealctx/anti-gateway/internal/core/providers"
	"github.com/pinealctx/anti-gateway/internal/models"
	"go.uber.org/zap"
)

const (
	providerTimeout = 300 * time.Second
	maxRetries      = 3
)

var retryBackoff = []time.Duration{2 * time.Second, 8 * time.Second, 18 * time.Second}

// Config holds Copilot provider configuration.
type Config struct {
	Name        string
	GithubToken string // Single GitHub OAuth token (one provider = one account)
	Logger      *zap.Logger
}

// Provider implements AIProvider for GitHub Copilot.
// Each Provider instance corresponds to exactly one GitHub account.
type Provider struct {
	name        string
	githubToken string
	copilot     *copilotToken
	username    string
	healthy     bool
	mu          sync.RWMutex
	client      *http.Client
	vsCodeVer   string
	logger      *zap.Logger
	authManager *AuthManager
	stopCh      chan struct{}
}

type copilotToken struct {
	token     string
	expiresAt time.Time
	issuedAt  time.Time
}

// NewProvider creates a new Copilot provider with a single GitHub token.
func NewProvider(cfg Config) *Provider {
	p := &Provider{
		name:        cfg.Name,
		githubToken: cfg.GithubToken,
		client:      &http.Client{Timeout: providerTimeout},
		logger:      cfg.Logger,
		authManager: NewAuthManager(),
		stopCh:      make(chan struct{}),
		healthy:     true,
	}

	// Fetch VSCode version
	p.vsCodeVer = getVSCodeVersion(p.client)
	p.logger.Info("Copilot provider initialized",
		zap.String("name", cfg.Name),
		zap.String("vscode_version", p.vsCodeVer),
	)

	// Start token refresh loop
	if cfg.GithubToken != "" {
		go p.tokenRefreshLoop()
	}

	return p
}

// AuthMgr returns the auth manager for device flow operations.
func (p *Provider) AuthMgr() *AuthManager {
	return p.authManager
}

// SetGithubToken updates the GitHub token at runtime (e.g., after device flow completion).
func (p *Provider) SetGithubToken(token string) {
	p.mu.Lock()
	p.githubToken = token
	p.healthy = true
	p.mu.Unlock()

	// Trigger immediate token refresh
	go p.refreshToken()
}

// GetUsername returns the authenticated username.
func (p *Provider) GetUsername() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.username
}

// GetTokenInfo returns current token status for admin display.
func (p *Provider) GetTokenInfo() map[string]any {
	p.mu.RLock()
	defer p.mu.RUnlock()

	info := map[string]any{
		"username": p.username,
		"healthy":  p.healthy,
		"has_token": p.copilot != nil && p.copilot.token != "",
	}
	if p.copilot != nil {
		info["token_expires"] = p.copilot.expiresAt.Format(time.RFC3339)
	}
	return info
}

func (p *Provider) Name() string { return p.name }

func (p *Provider) ChatCompletion(ctx context.Context, req *models.ChatCompletionRequest) (*models.ChatCompletionResponse, error) {
	req.Model = toCopilotModel(req.Model)
	req.Stream = false

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	respBody, err := p.doWithRetry(ctx, body)
	if err != nil {
		return nil, err
	}
	defer func() { _ = respBody.Close() }()

	var resp models.ChatCompletionResponse
	if err := json.NewDecoder(respBody).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &resp, nil
}

func (p *Provider) StreamCompletion(ctx context.Context, req *models.ChatCompletionRequest, stream chan<- providers.StreamChunk) error {
	defer close(stream)

	req.Model = toCopilotModel(req.Model)
	req.Stream = true

	body, err := json.Marshal(req)
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

func (p *Provider) RefreshToken(ctx context.Context) error {
	return p.refreshToken()
}

func (p *Provider) IsHealthy(ctx context.Context) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.healthy && p.copilot != nil && p.copilot.token != ""
}

// Stop shuts down the provider's background goroutines.
func (p *Provider) Stop() {
	close(p.stopCh)
}

// getToken returns the current copilot token and its health status.
func (p *Provider) getToken() (string, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.copilot == nil || p.copilot.token == "" {
		return "", false
	}
	return p.copilot.token, p.healthy
}

// doWithRetry performs a chat completion request with retries.
func (p *Provider) doWithRetry(ctx context.Context, body []byte) (io.ReadCloser, error) {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := retryBackoff[attempt-1]
			p.logger.Info("retrying copilot request",
				zap.Int("attempt", attempt),
				zap.Duration("backoff", backoff),
			)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		token, isHealthy := p.getToken()
		if !isHealthy || token == "" {
			lastErr = fmt.Errorf("copilot provider not healthy or no token")
			continue
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST", copilotChatURL, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}

		setCopilotHeaders(httpReq, token, p.vsCodeVer)
		httpReq.Header.Set("X-Request-Id", uuid.New().String())

		resp, err := p.client.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("copilot request failed: %w", err)
			continue
		}

		if resp.StatusCode == 429 {
			// Rate limited — mark as temporarily unhealthy
			_ = resp.Body.Close()
			p.mu.Lock()
			p.healthy = false
			p.mu.Unlock()

			go func() {
				time.Sleep(30 * time.Second)
				p.mu.Lock()
				p.healthy = true
				p.mu.Unlock()
			}()

			lastErr = fmt.Errorf("copilot rate limited (429)")
			continue
		}

		if resp.StatusCode >= 500 {
			respBytes, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("copilot %d: %s", resp.StatusCode, string(respBytes))
			continue
		}

		if resp.StatusCode >= 400 {
			respBytes, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			return nil, fmt.Errorf("copilot error %d: %s", resp.StatusCode, string(respBytes))
		}

		return resp.Body, nil
	}

	return nil, fmt.Errorf("copilot request failed after retries: %w", lastErr)
}

// tokenRefreshLoop runs a background goroutine that keeps the Copilot token fresh.
func (p *Provider) tokenRefreshLoop() {
	// Initial token fetch
	if err := p.refreshToken(); err != nil {
		p.logger.Error("Initial copilot token fetch failed",
			zap.String("provider", p.name),
			zap.Error(err),
		)
	}

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.mu.RLock()
			needsRefresh := p.copilot == nil || p.copilot.needsRefresh()
			p.mu.RUnlock()

			if needsRefresh {
				if err := p.refreshToken(); err != nil {
					p.logger.Warn("Copilot token refresh failed",
						zap.String("provider", p.name),
						zap.String("username", p.username),
						zap.Error(err),
					)
				}
			}
		}
	}
}

// refreshToken fetches a fresh Copilot JWT.
func (p *Provider) refreshToken() error {
	p.mu.RLock()
	githubToken := p.githubToken
	p.mu.RUnlock()

	if githubToken == "" {
		return fmt.Errorf("no github token configured")
	}

	tokenResp, err := RefreshCopilotToken(p.client, githubToken, p.vsCodeVer)
	if err != nil {
		p.mu.Lock()
		p.healthy = false
		p.mu.Unlock()
		return err
	}

	expiresAt := time.Unix(tokenResp.ExpiresAt, 0)

	p.mu.Lock()
	p.copilot = &copilotToken{
		token:     tokenResp.Token,
		expiresAt: expiresAt,
		issuedAt:  time.Now(),
	}
	p.healthy = true
	p.mu.Unlock()

	// Fetch username if not set
	p.mu.RLock()
	username := p.username
	p.mu.RUnlock()

	if username == "" {
		if user, err := FetchGithubUser(p.client, githubToken, p.vsCodeVer); err == nil {
			p.mu.Lock()
			p.username = user.Login
			p.mu.Unlock()
			p.logger.Info("Copilot account authenticated",
				zap.String("provider", p.name),
				zap.String("username", user.Login),
				zap.Time("token_expires", expiresAt),
			)
		}
	} else {
		p.logger.Debug("Copilot token refreshed",
			zap.String("provider", p.name),
			zap.String("username", username),
			zap.Time("expires", expiresAt),
		)
	}

	return nil
}

// FetchModels queries the Copilot models endpoint.
func (p *Provider) FetchModels(ctx context.Context) ([]string, error) {
	token, isHealthy := p.getToken()
	if !isHealthy || token == "" {
		return nil, fmt.Errorf("provider not healthy")
	}

	httpReq, err := http.NewRequestWithContext(ctx, "GET", copilotModelsURL, nil)
	if err != nil {
		return nil, err
	}
	setCopilotHeaders(httpReq, token, p.vsCodeVer)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	modelIDs := make([]string, len(result.Data))
	for i, m := range result.Data {
		modelIDs[i] = m.ID
	}
	return modelIDs, nil
}

// CreateEmbedding implements EmbeddingProvider for Copilot.
func (p *Provider) CreateEmbedding(ctx context.Context, req *models.EmbeddingRequest) (*models.EmbeddingResponse, error) {
	token, isHealthy := p.getToken()
	if !isHealthy || token == "" {
		return nil, fmt.Errorf("provider not healthy")
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", copilotEmbedURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	setCopilotHeaders(httpReq, token, p.vsCodeVer)
	httpReq.Header.Set("X-Request-Id", uuid.New().String())

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("copilot embeddings %d: %s", resp.StatusCode, string(respBytes))
	}

	var embResp models.EmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&embResp); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	return &embResp, nil
}

// needsRefresh checks if the token needs to be refreshed.
func (t *copilotToken) needsRefresh() bool {
	if t == nil {
		return true
	}
	// Refresh if expired or will expire in next 10 minutes
	return time.Now().Add(10 * time.Minute).After(t.expiresAt)
}
