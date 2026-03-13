package kiro

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
	form.Set("scope", "openid profile offline_access")

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
			resp.Body.Close()
			lastErr = fmt.Errorf("login token refresh status: %d", resp.StatusCode)
			continue
		}

		if resp.StatusCode != 200 {
			resp.Body.Close()
			return nil, fmt.Errorf("login token refresh status: %d", resp.StatusCode)
		}

		var result struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			ExpiresIn    int    `json:"expires_in"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decode login token refresh: %w", err)
		}
		resp.Body.Close()

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

// StartBackgroundRefresh runs a goroutine that periodically refreshes the token.
func (tm *TokenManager) StartBackgroundRefresh(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			if _, err := tm.refresh(); err != nil {
				tm.logger.Error("background token refresh failed", zap.Error(err))
			}
		}
	}()
}
