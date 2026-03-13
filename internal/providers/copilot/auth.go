package copilot

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	githubClientID = "Iv1.b507a08c87ecfe98"
	githubScope    = "read:user"

	githubDeviceURL = "https://github.com/login/device/code"
	githubTokenURL  = "https://github.com/login/oauth/access_token"
	githubUserURL   = "https://api.github.com/user"
	copilotTokenURL = "https://api.github.com/copilot_internal/v2/token"
)

// DeviceFlowResponse is returned when initiating a device code flow.
type DeviceFlowResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// AuthSession tracks a pending device flow authorization.
type AuthSession struct {
	ID              string    `json:"id"`
	DeviceCode      string    `json:"device_code"`
	UserCode        string    `json:"user_code"`
	VerificationURI string    `json:"verification_uri"`
	ExpiresAt       time.Time `json:"expires_at"`
	Interval        int       `json:"interval"`
	Status          string    `json:"status"` // pending, completed, expired, error
	AccessToken     string    `json:"access_token,omitempty"`
	Error           string    `json:"error,omitempty"`
	mu              sync.Mutex
	stopCh          chan struct{}
}

// Mu returns the session's mutex for external synchronization.
func (s *AuthSession) Mu() *sync.Mutex {
	return &s.mu
}

// copilotTokenResponse is the JWT token response from GitHub.
type copilotTokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}

// GithubUser represents basic GitHub user info.
type GithubUser struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
}

// AuthManager manages device flow sessions.
type AuthManager struct {
	mu       sync.Mutex
	sessions map[string]*AuthSession
	client   *http.Client
}

// NewAuthManager creates a new auth manager.
func NewAuthManager() *AuthManager {
	return &AuthManager{
		sessions: make(map[string]*AuthSession),
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// StartDeviceFlow initiates a GitHub device code flow.
func (am *AuthManager) StartDeviceFlow() (*AuthSession, error) {
	form := url.Values{
		"client_id": {githubClientID},
		"scope":     {githubScope},
	}

	req, err := http.NewRequest("POST", githubDeviceURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := am.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("device code request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("device code request failed (%d): %s", resp.StatusCode, body)
	}

	var dfResp DeviceFlowResponse
	if err := json.NewDecoder(resp.Body).Decode(&dfResp); err != nil {
		return nil, fmt.Errorf("decode device code response: %w", err)
	}

	if dfResp.Interval < 5 {
		dfResp.Interval = 5
	}

	session := &AuthSession{
		ID:              uuid.New().String(),
		DeviceCode:      dfResp.DeviceCode,
		UserCode:        dfResp.UserCode,
		VerificationURI: dfResp.VerificationURI,
		ExpiresAt:       time.Now().Add(time.Duration(dfResp.ExpiresIn) * time.Second),
		Interval:        dfResp.Interval,
		Status:          "pending",
		stopCh:          make(chan struct{}),
	}

	am.mu.Lock()
	am.sessions[session.ID] = session
	am.mu.Unlock()

	go am.pollForToken(session)

	return session, nil
}

// GetSession returns a session by ID.
func (am *AuthManager) GetSession(id string) (*AuthSession, bool) {
	am.mu.Lock()
	defer am.mu.Unlock()
	s, ok := am.sessions[id]
	return s, ok
}

// RemoveSession removes a completed/expired session.
func (am *AuthManager) RemoveSession(id string) {
	am.mu.Lock()
	defer am.mu.Unlock()
	if s, ok := am.sessions[id]; ok {
		close(s.stopCh)
		delete(am.sessions, id)
	}
}

// pollForToken polls GitHub for a token until authorized or expired.
func (am *AuthManager) pollForToken(session *AuthSession) {
	interval := time.Duration(session.Interval) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-session.stopCh:
			return
		case <-ticker.C:
			if time.Now().After(session.ExpiresAt) {
				session.mu.Lock()
				session.Status = "expired"
				session.Error = "device code expired"
				session.mu.Unlock()
				return
			}

			token, err := am.exchangeDeviceCode(session.DeviceCode)
			if err != nil {
				if err.Error() == "authorization_pending" {
					continue
				}
				if strings.HasPrefix(err.Error(), "slow_down") {
					// Increase polling interval
					interval += 5 * time.Second
					ticker.Reset(interval)
					continue
				}
				session.mu.Lock()
				session.Status = "error"
				session.Error = err.Error()
				session.mu.Unlock()
				return
			}

			session.mu.Lock()
			session.AccessToken = token
			session.Status = "completed"
			session.mu.Unlock()
			return
		}
	}
}

// exchangeDeviceCode exchanges a device code for an access token.
func (am *AuthManager) exchangeDeviceCode(deviceCode string) (string, error) {
	form := url.Values{
		"client_id":   {githubClientID},
		"device_code": {deviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}

	req, err := http.NewRequest("POST", githubTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := am.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Error       string `json:"error"`
		Interval    int    `json:"interval"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}

	if result.Error != "" {
		if result.Error == "slow_down" {
			return "", fmt.Errorf("slow_down:%d", result.Interval)
		}
		return "", fmt.Errorf("%s", result.Error)
	}

	if result.AccessToken == "" {
		return "", fmt.Errorf("empty access token")
	}

	return result.AccessToken, nil
}

// RefreshCopilotToken exchanges a GitHub token for a Copilot JWT.
func RefreshCopilotToken(client *http.Client, githubToken, vsCodeVersion string) (*copilotTokenResponse, error) {
	req, err := http.NewRequest("GET", copilotTokenURL, nil)
	if err != nil {
		return nil, err
	}

	setGithubHeaders(req, githubToken, vsCodeVersion)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("copilot token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("copilot token request failed (%d): %s", resp.StatusCode, body)
	}

	var tokenResp copilotTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("decode copilot token: %w", err)
	}

	return &tokenResp, nil
}

// FetchGithubUser fetches user info from GitHub.
func FetchGithubUser(client *http.Client, githubToken, vsCodeVersion string) (*GithubUser, error) {
	req, err := http.NewRequest("GET", githubUserURL, nil)
	if err != nil {
		return nil, err
	}

	setGithubHeaders(req, githubToken, vsCodeVersion)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github user request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github user request failed (%d)", resp.StatusCode)
	}

	var user GithubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, err
	}

	return &user, nil
}

func setGithubHeaders(req *http.Request, githubToken, vsCodeVersion string) {
	req.Header.Set("Authorization", "token "+githubToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Editor-Version", "vscode/"+vsCodeVersion)
	req.Header.Set("Editor-Plugin-Version", "copilot-chat/"+copilotVersion)
	req.Header.Set("User-Agent", "GitHubCopilotChat/"+copilotVersion)
	req.Header.Set("X-GitHub-API-Version", githubAPIVersion)
}
