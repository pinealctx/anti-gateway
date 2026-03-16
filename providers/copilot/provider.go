package copilot

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pinealctx/anti-gateway/core/providers"
	"github.com/pinealctx/anti-gateway/models"
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
	models      []string // dynamically fetched from Copilot API
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
	apiBase   string // from endpoints.api in token response
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
	p.healthy = true // Optimistically set healthy, refreshToken will update based on result
	p.mu.Unlock()

	// Trigger synchronous token refresh (blocking to ensure status is updated)
	if err := p.refreshToken(); err != nil {
		p.logger.Warn("Initial token refresh after SetGithubToken failed",
			zap.String("provider", p.name),
			zap.Error(err),
		)
	}
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
		"username":  p.username,
		"healthy":   p.healthy,
		"has_token": p.copilot != nil && p.copilot.token != "",
		"models":    p.models,
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
	sanitizeMessages(req)

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

	// Debug: log messages BEFORE sanitize
	for mi, msg := range req.Messages {
		if len(msg.ToolCalls) > 0 {
			for ti, tc := range msg.ToolCalls {
				p.logger.Debug("pre-sanitize tool_call",
					zap.Int("msg_idx", mi),
					zap.Int("tc_idx", ti),
					zap.String("tc_id", tc.ID),
					zap.String("func_name", tc.Function.Name),
				)
			}
		}
		if msg.Role == "tool" {
			p.logger.Debug("pre-sanitize tool_response",
				zap.Int("msg_idx", mi),
				zap.String("tool_call_id", msg.ToolCallID),
			)
		}
	}

	sanitizeMessages(req)

	// Debug: log messages AFTER sanitize
	for mi, msg := range req.Messages {
		if len(msg.ToolCalls) > 0 {
			for ti, tc := range msg.ToolCalls {
				p.logger.Debug("post-sanitize tool_call",
					zap.Int("msg_idx", mi),
					zap.Int("tc_idx", ti),
					zap.String("tc_id", tc.ID),
					zap.String("func_name", tc.Function.Name),
					zap.String("arguments", tc.Function.Arguments),
				)
			}
		}
	}

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
				for _, tc := range choice.Delta.ToolCalls {
					p.logger.Debug("response tool_call delta",
						zap.String("tc_id", tc.ID),
						zap.String("func_name", tc.Function.Name),
						zap.String("arguments", tc.Function.Arguments),
						zap.Intp("index", tc.Index),
					)
				}
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

// apiBaseURL returns the API base URL from the token response, or the default.
func (p *Provider) apiBaseURL() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.copilot != nil && p.copilot.apiBase != "" {
		return p.copilot.apiBase
	}
	return copilotBaseURL
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

		httpReq, err := http.NewRequestWithContext(ctx, "POST", p.apiBaseURL()+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}

		setCopilotHeaders(httpReq, token, p.vsCodeVer)

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
	apiBase := strings.TrimRight(tokenResp.Endpoints.API, "/")

	p.mu.Lock()
	p.copilot = &copilotToken{
		token:     tokenResp.Token,
		expiresAt: expiresAt,
		issuedAt:  time.Now(),
		apiBase:   apiBase,
	}
	p.healthy = true
	p.mu.Unlock()

	// Fetch username if not set
	p.mu.RLock()
	username := p.username
	p.mu.RUnlock()

	if username == "" {
		user, err := FetchGithubUser(p.client, githubToken, p.vsCodeVer)
		if err != nil {
			p.logger.Warn("Failed to fetch GitHub user info",
				zap.String("provider", p.name),
				zap.Error(err),
			)
		} else {
			p.mu.Lock()
			p.username = user.Login
			p.mu.Unlock()
			p.logger.Info("Copilot account authenticated",
				zap.String("provider", p.name),
				zap.String("username", user.Login),
				zap.Time("token_expires", expiresAt),
				zap.String("api_base", apiBase),
			)
		}
	} else {
		p.logger.Debug("Copilot token refreshed",
			zap.String("provider", p.name),
			zap.String("username", username),
			zap.Time("expires", expiresAt),
		)
	}

	// Fetch available models from Copilot API (non-blocking, best-effort)
	go p.fetchAndCacheModels()

	return nil
}

// FetchModels queries the Copilot models endpoint.
func (p *Provider) FetchModels(ctx context.Context) ([]string, error) {
	token, isHealthy := p.getToken()
	if !isHealthy || token == "" {
		return nil, fmt.Errorf("provider not healthy")
	}

	httpReq, err := http.NewRequestWithContext(ctx, "GET", p.apiBaseURL()+"/models", nil)
	if err != nil {
		return nil, err
	}
	setCopilotHeaders(httpReq, token, p.vsCodeVer)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("copilot models %d: %s", resp.StatusCode, string(respBytes))
	}

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

// fetchAndCacheModels fetches available models and caches them.
func (p *Provider) fetchAndCacheModels() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	models, err := p.FetchModels(ctx)
	if err != nil {
		p.logger.Warn("Failed to fetch Copilot models",
			zap.String("provider", p.name),
			zap.Error(err),
		)
		return
	}

	p.mu.Lock()
	p.models = models
	p.mu.Unlock()

	p.logger.Info("Copilot models refreshed",
		zap.String("provider", p.name),
		zap.Int("count", len(models)),
		zap.Strings("models", models),
	)
}

// GetModels returns the cached list of available Copilot models.
func (p *Provider) GetModels() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.models
}

// SupportedModels returns externally maintained model IDs merged with
// runtime-fetched Copilot /models results (if available).
func (p *Provider) SupportedModels() []string {
	set := make(map[string]struct{}, len(DefaultSupportedModels)+len(p.models))
	for _, m := range DefaultSupportedModels {
		if m != "" {
			set[m] = struct{}{}
		}
	}
	for _, m := range p.GetModels() {
		if m != "" {
			set[m] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for m := range set {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
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

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.apiBaseURL()+"/embeddings", bytes.NewReader(body))
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

// sanitizeMessages cleans up messages that Copilot API would reject:
// 1. Remove tool_calls with empty function.name
// 2. Ensure tool_call arguments are valid JSON (at least "{}")
// 3. Ensure every tool_call has a matching tool response (and vice versa)
func sanitizeMessages(req *models.ChatCompletionRequest) {
	// Pass 1: clean tool_calls with empty names, fix invalid arguments
	for i := range req.Messages {
		if len(req.Messages[i].ToolCalls) == 0 {
			continue
		}
		clean := req.Messages[i].ToolCalls[:0]
		for _, tc := range req.Messages[i].ToolCalls {
			if tc.Function.Name != "" {
				if tc.Function.Arguments == "" || !json.Valid([]byte(tc.Function.Arguments)) {
					tc.Function.Arguments = "{}"
				}
				clean = append(clean, tc)
			}
		}
		req.Messages[i].ToolCalls = clean
	}

	// Pass 2: build positionally-valid pairs.
	// OpenAI API requires tool responses to immediately follow the assistant
	// message with tool_calls. If a non-tool message (e.g. user) sits between
	// the assistant tool_call and its tool response, the pair is broken.
	pairedIDs := make(map[string]bool)
	for i, msg := range req.Messages {
		if len(msg.ToolCalls) == 0 {
			continue
		}
		// Collect tool_call IDs from this assistant message
		callIDs := make(map[string]bool)
		for _, tc := range msg.ToolCalls {
			if tc.ID != "" {
				callIDs[tc.ID] = true
			}
		}
		// Only count tool responses in the consecutive tool-role block right after
		for j := i + 1; j < len(req.Messages); j++ {
			if req.Messages[j].Role != "tool" {
				break
			}
			if callIDs[req.Messages[j].ToolCallID] {
				pairedIDs[req.Messages[j].ToolCallID] = true
			}
		}
	}

	// Pass 3: strip unmatched tool_calls from assistant messages
	for i := range req.Messages {
		if len(req.Messages[i].ToolCalls) == 0 {
			continue
		}
		clean := req.Messages[i].ToolCalls[:0]
		for _, tc := range req.Messages[i].ToolCalls {
			if pairedIDs[tc.ID] {
				clean = append(clean, tc)
			}
		}
		req.Messages[i].ToolCalls = clean
	}

	// Pass 4: remove orphaned tool-role messages
	clean := req.Messages[:0]
	for _, msg := range req.Messages {
		if msg.Role == "tool" {
			if !pairedIDs[msg.ToolCallID] {
				continue
			}
		}
		clean = append(clean, msg)
	}
	req.Messages = clean
}
