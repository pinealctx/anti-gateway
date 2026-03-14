package kiro

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	tokenRefreshBefore = 5 * time.Minute
)

// TokenInfo holds the current access token and metadata.
type TokenInfo struct {
	AccessToken   string
	ExpiresAt     time.Time
	IsExternalIdP bool
}

// LoginToken holds credentials obtained from the built-in PKCE login flow.
type LoginToken struct {
	AccessToken   string
	RefreshToken  string
	ClientID      string
	TokenEndpoint string // e.g. https://login.microsoftonline.com/{tenant}/oauth2/v2.0/token
	ExpiresAt     time.Time
	IsExternalIdP bool
	RefreshScope  string // extracted from JWT aud+scp at login time
	ProfileArn    string // CW profile ARN, persisted alongside token
}

// TokenManager manages Kiro tokens obtained from the built-in PKCE login flow.
type TokenManager struct {
	mu         sync.RWMutex
	current    *TokenInfo
	logger     *zap.Logger
	client     *http.Client
	loginToken *LoginToken
}

func NewTokenManager(logger *zap.Logger) *TokenManager {
	return &TokenManager{
		logger: logger,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// SetLoginToken injects a token obtained from the built-in PKCE login flow.
func (tm *TokenManager) SetLoginToken(lt *LoginToken) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	if lt.RefreshScope == "" {
		lt.RefreshScope = extractRefreshScope(lt.AccessToken)
		if lt.RefreshScope != "" {
			tm.logger.Info("extracted refresh scope from JWT", zap.String("scope", lt.RefreshScope))
		}
	}
	tm.loginToken = lt
	// Immediately set as current token
	tm.current = &TokenInfo{
		AccessToken:   lt.AccessToken,
		ExpiresAt:     lt.ExpiresAt,
		IsExternalIdP: lt.IsExternalIdP,
	}
	tm.logger.Info("login token set", zap.Time("expires_at", lt.ExpiresAt))
}

// HasToken returns true if any token source is available.
func (tm *TokenManager) HasToken() bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.current != nil || tm.loginToken != nil
}

// GetToken returns the current valid token, refreshing if necessary.
func (tm *TokenManager) GetToken() (*TokenInfo, error) {
	tm.mu.RLock()
	if tm.current != nil && time.Now().Before(tm.current.ExpiresAt.Add(-tokenRefreshBefore)) {
		token := tm.current
		tm.mu.RUnlock()
		return token, nil
	}
	tm.mu.RUnlock()

	return tm.refresh()
}

// ForceRefresh forces a token refresh regardless of expiry.
func (tm *TokenManager) ForceRefresh() (*TokenInfo, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.loginToken == nil {
		return nil, fmt.Errorf("no login token available")
	}

	token, err := tm.tryLoginTokenRefresh()
	if err != nil {
		return nil, err
	}
	tm.current = token
	tm.logger.Info("token force-refreshed", zap.Time("expires_at", token.ExpiresAt))
	return token, nil
}

func (tm *TokenManager) refresh() (*TokenInfo, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Double-check after acquiring write lock
	if tm.current != nil && time.Now().Before(tm.current.ExpiresAt.Add(-tokenRefreshBefore)) {
		return tm.current, nil
	}

	// Priority 1: Built-in PKCE login token
	if tm.loginToken != nil {
		token, err := tm.tryLoginTokenRefresh()
		if err == nil {
			tm.current = token
			tm.logger.Info("token refreshed (login)", zap.Time("expires_at", token.ExpiresAt))
			return token, nil
		}
		tm.logger.Warn("login token refresh failed", zap.Error(err))
		// If login token has a still-valid access token, use it
		if tm.current != nil && time.Now().Before(tm.current.ExpiresAt) {
			return tm.current, nil
		}
	}

	// If we have a still-valid old token, keep using it
	if tm.current != nil && time.Now().Before(tm.current.ExpiresAt) {
		tm.logger.Warn("refresh failed but existing token still valid")
		return tm.current, nil
	}

	return nil, fmt.Errorf("all token refresh strategies failed")
}

// tryLoginTokenRefresh refreshes the token using credentials from the PKCE login flow.
// Uses Microsoft OAuth2 form-urlencoded POST when TokenEndpoint is set.
func (tm *TokenManager) tryLoginTokenRefresh() (*TokenInfo, error) {
	lt := tm.loginToken
	if lt.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh token available")
	}
	if lt.TokenEndpoint == "" {
		return nil, fmt.Errorf("no token endpoint for login token refresh")
	}

	form := url.Values{}
	form.Set("client_id", lt.ClientID)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", lt.RefreshToken)
	if lt.RefreshScope != "" {
		form.Set("scope", lt.RefreshScope)
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(retryBackoff[attempt-1])
		}

		resp, err := tm.client.PostForm(lt.TokenEndpoint, form)
		if err != nil {
			lastErr = fmt.Errorf("login token refresh request: %w", err)
			continue
		}

		if resp.StatusCode >= 500 {
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("login token refresh status: %d", resp.StatusCode)
			continue
		}

		if resp.StatusCode != 200 {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("login token refresh status: %d", resp.StatusCode)
		}

		var result struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			ExpiresIn    int    `json:"expires_in"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("decode login token refresh: %w", err)
		}
		_ = resp.Body.Close()

		// Update stored refresh token if a new one was issued
		if result.RefreshToken != "" {
			lt.RefreshToken = result.RefreshToken
		}

		lt.AccessToken = result.AccessToken
		lt.ExpiresAt = time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)

		return &TokenInfo{
			AccessToken:   result.AccessToken,
			ExpiresAt:     lt.ExpiresAt,
			IsExternalIdP: lt.IsExternalIdP,
		}, nil
	}

	return nil, fmt.Errorf("login token refresh failed after %d retries: %w", maxRetries, lastErr)
}

// extractRefreshScope parses the JWT access_token to build the correct OAuth2 scope
// for token refresh. Azure AD requires resource-specific scopes (e.g.
// api://app-id/permission) rather than generic OIDC scopes (openid profile),
// otherwise the refreshed token's audience becomes client_id itself.
// Returns empty string if extraction fails (caller should omit scope param).
func extractRefreshScope(accessToken string) string {
	parts := strings.SplitN(accessToken, ".", 3)
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Aud json.RawMessage `json:"aud"`
		Scp string          `json:"scp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}

	// aud can be a string or an array of strings
	var aud string
	if err := json.Unmarshal(claims.Aud, &aud); err != nil {
		var auds []string
		if err := json.Unmarshal(claims.Aud, &auds); err != nil || len(auds) == 0 {
			return ""
		}
		aud = auds[0]
	}

	if aud == "" || claims.Scp == "" {
		return ""
	}

	// Build: {aud}/{scope1} {aud}/{scope2} ... offline_access
	scpItems := strings.Fields(claims.Scp)
	result := make([]string, 0, len(scpItems)+1)
	for _, s := range scpItems {
		result = append(result, aud+"/"+s)
	}
	result = append(result, "offline_access")
	return strings.Join(result, " ")
}

// StartBackgroundRefresh runs a goroutine that periodically refreshes the token.
func (tm *TokenManager) StartBackgroundRefresh(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			// Skip refresh if no token source is configured yet (no one has logged in)
			if !tm.HasToken() {
				continue
			}
			if _, err := tm.refresh(); err != nil {
				tm.logger.Error("background token refresh failed", zap.Error(err))
			}
		}
	}()
}

// StartBackgroundRefreshWithStop runs a goroutine that periodically refreshes the token,
// and stops when the provided channel is closed.
func (tm *TokenManager) StartBackgroundRefreshWithStop(interval time.Duration, stopCh <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				// Skip refresh if no token source is configured yet (no one has logged in)
				if !tm.HasToken() {
					continue
				}
				if _, err := tm.refresh(); err != nil {
					tm.logger.Error("background token refresh failed", zap.Error(err))
				}
			}
		}
	}()
}
