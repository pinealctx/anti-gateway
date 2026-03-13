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

	"github.com/SilkageNet/anti-gateway/internal/core/providers"
	"github.com/SilkageNet/anti-gateway/internal/models"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	providerTimeout = 300 * time.Second
	maxRetries      = 3
)

var retryBackoff = []time.Duration{2 * time.Second, 8 * time.Second, 18 * time.Second}

// Config holds Copilot provider configuration.
type Config struct {
	Name         string
	GithubTokens []string // One or more GitHub OAuth tokens
	Logger       *zap.Logger
}

// Account represents a single GitHub account with Copilot access.
type Account struct {
	GithubToken  string
	CopilotToken *CopilotToken
	IssuedAt     time.Time
	Username     string
	healthy      bool
	mu           sync.RWMutex
}

// Provider implements AIProvider for GitHub Copilot.
type Provider struct {
	name        string
	accounts    []*Account
	accountIdx  int
	mu          sync.Mutex
	client      *http.Client
	vsCodeVer   string
	logger      *zap.Logger
	authManager *AuthManager
	stopCh      chan struct{}
}

// NewProvider creates a new Copilot provider.
func NewProvider(cfg Config) *Provider {
	accounts := make([]*Account, 0, len(cfg.GithubTokens))
	for _, token := range cfg.GithubTokens {
		if token == "" {
			continue
		}
		accounts = append(accounts, &Account{
			GithubToken: token,
			healthy:     true,
		})
	}

	p := &Provider{
		name:        cfg.Name,
		accounts:    accounts,
		client:      &http.Client{Timeout: providerTimeout},
		logger:      cfg.Logger,
		authManager: NewAuthManager(),
		stopCh:      make(chan struct{}),
	}

	// Fetch VSCode version
	p.vsCodeVer = getVSCodeVersion(p.client)
	p.logger.Info("Copilot provider initialized",
		zap.String("name", cfg.Name),
		zap.Int("accounts", len(accounts)),
		zap.String("vscode_version", p.vsCodeVer),
	)

	// Start token refresh loop for all accounts
	for _, acc := range accounts {
		go p.tokenRefreshLoop(acc)
	}

	return p
}

// AddAccount adds a new GitHub token to the account pool at runtime.
func (p *Provider) AddAccount(githubToken string) {
	acc := &Account{
		GithubToken: githubToken,
		healthy:     true,
	}
	p.mu.Lock()
	p.accounts = append(p.accounts, acc)
	p.mu.Unlock()

	go p.tokenRefreshLoop(acc)
	p.logger.Info("Added Copilot account", zap.Int("total", len(p.accounts)))
}

// AuthMgr returns the auth manager for device flow operations.
func (p *Provider) AuthMgr() *AuthManager {
	return p.authManager
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
	p.mu.Lock()
	accounts := make([]*Account, len(p.accounts))
	copy(accounts, p.accounts)
	p.mu.Unlock()

	var lastErr error
	for _, acc := range accounts {
		if err := p.refreshAccountToken(acc); err != nil {
			lastErr = err
			p.logger.Warn("Token refresh failed",
				zap.String("username", acc.Username),
				zap.Error(err),
			)
		}
	}
	return lastErr
}

func (p *Provider) IsHealthy(ctx context.Context) bool {
	_, err := p.pickAccount()
	return err == nil
}

// Stop shuts down the provider's background goroutines.
func (p *Provider) Stop() {
	close(p.stopCh)
}

// AccountInfo represents account status for admin display.
type AccountInfo struct {
	Username    string `json:"username"`
	Healthy     bool   `json:"healthy"`
	HasToken    bool   `json:"has_token"`
	TokenExpiry string `json:"token_expiry,omitempty"`
}

// ListAccounts returns account status information.
func (p *Provider) ListAccounts() []AccountInfo {
	p.mu.Lock()
	accounts := make([]*Account, len(p.accounts))
	copy(accounts, p.accounts)
	p.mu.Unlock()

	result := make([]AccountInfo, len(accounts))
	for i, acc := range accounts {
		acc.mu.RLock()
		info := AccountInfo{
			Username: acc.Username,
			Healthy:  acc.healthy,
			HasToken: acc.CopilotToken != nil && acc.CopilotToken.Token != "",
		}
		if acc.CopilotToken != nil {
			info.TokenExpiry = acc.CopilotToken.ExpiresAt.Format(time.RFC3339)
		}
		acc.mu.RUnlock()
		result[i] = info
	}
	return result
}

// pickAccount selects the next healthy account using round-robin.
func (p *Provider) pickAccount() (*Account, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.accounts) == 0 {
		return nil, fmt.Errorf("no copilot accounts configured")
	}

	// Round-robin with health check
	start := p.accountIdx
	for i := 0; i < len(p.accounts); i++ {
		idx := (start + i) % len(p.accounts)
		acc := p.accounts[idx]
		acc.mu.RLock()
		hasToken := acc.CopilotToken != nil && acc.CopilotToken.Token != ""
		isHealthy := acc.healthy
		acc.mu.RUnlock()

		if hasToken && isHealthy {
			p.accountIdx = (idx + 1) % len(p.accounts)
			return acc, nil
		}
	}

	return nil, fmt.Errorf("no healthy copilot accounts available")
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

		acc, err := p.pickAccount()
		if err != nil {
			lastErr = err
			continue
		}

		acc.mu.RLock()
		token := acc.CopilotToken.Token
		acc.mu.RUnlock()

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
			// Rate limited — mark account as temporarily unhealthy
			_ = resp.Body.Close()
			acc.mu.Lock()
			acc.healthy = false
			acc.mu.Unlock()

			go func(a *Account) {
				time.Sleep(30 * time.Second)
				a.mu.Lock()
				a.healthy = true
				a.mu.Unlock()
			}(acc)

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
func (p *Provider) tokenRefreshLoop(acc *Account) {
	// Initial token fetch
	if err := p.refreshAccountToken(acc); err != nil {
		p.logger.Error("Initial copilot token fetch failed",
			zap.String("token_prefix", acc.GithubToken[:min(8, len(acc.GithubToken))]),
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
			acc.mu.RLock()
			needsRefresh := acc.CopilotToken == nil || acc.CopilotToken.NeedsRefresh(acc.IssuedAt)
			acc.mu.RUnlock()

			if needsRefresh {
				if err := p.refreshAccountToken(acc); err != nil {
					p.logger.Warn("Copilot token refresh failed",
						zap.String("username", acc.Username),
						zap.Error(err),
					)
				}
			}
		}
	}
}

// refreshAccountToken fetches a fresh Copilot JWT for an account.
func (p *Provider) refreshAccountToken(acc *Account) error {
	tokenResp, err := RefreshCopilotToken(p.client, acc.GithubToken, p.vsCodeVer)
	if err != nil {
		acc.mu.Lock()
		acc.healthy = false
		acc.mu.Unlock()
		return err
	}

	expiresAt := time.Unix(tokenResp.ExpiresAt, 0)

	acc.mu.Lock()
	acc.CopilotToken = &CopilotToken{
		Token:     tokenResp.Token,
		ExpiresAt: expiresAt,
	}
	acc.IssuedAt = time.Now()
	acc.healthy = true
	acc.mu.Unlock()

	// Fetch username if not set
	if acc.Username == "" {
		if user, err := FetchGithubUser(p.client, acc.GithubToken, p.vsCodeVer); err == nil {
			acc.mu.Lock()
			acc.Username = user.Login
			acc.mu.Unlock()
			p.logger.Info("Copilot account authenticated",
				zap.String("username", user.Login),
				zap.Time("token_expires", expiresAt),
			)
		}
	} else {
		p.logger.Debug("Copilot token refreshed",
			zap.String("username", acc.Username),
			zap.Time("expires", expiresAt),
		)
	}

	return nil
}

// FetchModels queries the Copilot models endpoint.
func (p *Provider) FetchModels(ctx context.Context) ([]string, error) {
	acc, err := p.pickAccount()
	if err != nil {
		return nil, err
	}

	acc.mu.RLock()
	token := acc.CopilotToken.Token
	acc.mu.RUnlock()

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
	acc, err := p.pickAccount()
	if err != nil {
		return nil, err
	}

	acc.mu.RLock()
	token := acc.CopilotToken.Token
	acc.mu.RUnlock()

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
