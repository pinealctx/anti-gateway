package kiro

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	kiroSigninURL        = "https://app.kiro.dev/signin"
	kiroTokenExchangeURL = "https://prod.us-east-1.auth.desktop.kiro.dev/token"
	defaultCallbackPort  = 3128
	loginTimeout         = 5 * time.Minute
)

// KiroLoginSession tracks a pending PKCE authorization code login flow.
type KiroLoginSession struct {
	ID           string `json:"id"`
	AuthURL      string `json:"auth_url"`
	CallbackPort int    `json:"callback_port"`
	Status       string `json:"status"` // pending, completed, expired, error
	Error        string `json:"error,omitempty"`

	// PKCE internal state
	state        string
	codeVerifier string

	// Populated on completion
	AccessToken    string    `json:"-"`
	RefreshToken   string    `json:"-"`
	ClientID       string    `json:"-"`
	TokenEndpoint  string    `json:"-"`
	TokenExpiresAt time.Time `json:"-"`

	// Server management
	server *http.Server
	mu     sync.Mutex
	done   chan struct{}
}

// Mu returns the session's mutex for external synchronization.
func (s *KiroLoginSession) Mu() *sync.Mutex {
	return &s.mu
}

// KiroAuthManager manages Kiro PKCE login flows.
type KiroAuthManager struct {
	mu       sync.Mutex
	sessions map[string]*KiroLoginSession
	logger   *zap.Logger
}

// NewKiroAuthManager creates a new Kiro auth manager.
func NewKiroAuthManager(logger *zap.Logger) *KiroAuthManager {
	return &KiroAuthManager{
		sessions: make(map[string]*KiroLoginSession),
		logger:   logger,
	}
}

// StartLogin initiates a PKCE authorization code flow.
// callbackPort is the local port for the OAuth callback (default 3128).
func (am *KiroAuthManager) StartLogin(callbackPort int) (*KiroLoginSession, error) {
	if callbackPort <= 0 {
		callbackPort = defaultCallbackPort
	}

	// Generate PKCE parameters
	verifier, err := generateCodeVerifier()
	if err != nil {
		return nil, fmt.Errorf("generate code verifier: %w", err)
	}
	challenge := computeCodeChallenge(verifier)
	state := uuid.New().String()

	redirectURI := fmt.Sprintf("http://localhost:%d", callbackPort)

	params := url.Values{}
	params.Set("state", state)
	params.Set("code_challenge", challenge)
	params.Set("code_challenge_method", "S256")
	params.Set("redirect_uri", redirectURI)
	params.Set("redirect_from", "KiroIDE")

	session := &KiroLoginSession{
		ID:           uuid.New().String(),
		AuthURL:      kiroSigninURL + "?" + params.Encode(),
		CallbackPort: callbackPort,
		Status:       "pending",
		state:        state,
		codeVerifier: verifier,
		done:         make(chan struct{}),
	}

	if err := am.startCallbackServer(session); err != nil {
		return nil, fmt.Errorf("start callback server: %w", err)
	}

	am.mu.Lock()
	am.sessions[session.ID] = session
	am.mu.Unlock()

	am.logger.Info("Kiro PKCE login started",
		zap.String("session_id", session.ID),
		zap.Int("port", callbackPort))

	return session, nil
}

// GetSession returns a login session by ID.
func (am *KiroAuthManager) GetSession(id string) (*KiroLoginSession, bool) {
	am.mu.Lock()
	defer am.mu.Unlock()
	s, ok := am.sessions[id]
	return s, ok
}

// RemoveSession removes a completed/expired session and shuts down its callback server.
func (am *KiroAuthManager) RemoveSession(id string) {
	am.mu.Lock()
	defer am.mu.Unlock()
	if s, ok := am.sessions[id]; ok {
		select {
		case <-s.done:
		default:
			close(s.done)
		}
		if s.server != nil {
			s.server.Close()
		}
		delete(am.sessions, id)
	}
}

func (am *KiroAuthManager) startCallbackServer(session *KiroLoginSession) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		am.handleCallback(session, w, r)
	})

	addr := fmt.Sprintf("127.0.0.1:%d", session.CallbackPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	session.server = &http.Server{Handler: mux}

	go func() {
		if err := session.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			am.logger.Error("callback server error", zap.Error(err))
		}
	}()

	// Auto-expire session after timeout
	go func() {
		timer := time.NewTimer(loginTimeout)
		defer timer.Stop()
		select {
		case <-timer.C:
			session.mu.Lock()
			if session.Status == "pending" {
				session.Status = "expired"
				session.Error = "login timeout"
			}
			session.mu.Unlock()
			session.server.Close()
		case <-session.done:
		}
	}()

	return nil
}

// callbackTokenResult holds tokens extracted from the OAuth callback.
type callbackTokenResult struct {
	AccessToken   string `json:"access_token"`
	RefreshToken  string `json:"refresh_token"`
	ClientID      string `json:"client_id"`
	TokenEndpoint string `json:"token_endpoint"`
	ExpiresAt     string `json:"expires_at"`
	ExpiresIn     int    `json:"expires_in"`
}

func (am *KiroAuthManager) handleCallback(session *KiroLoginSession, w http.ResponseWriter, r *http.Request) {
	am.logger.Info("OAuth callback received",
		zap.String("method", r.Method),
		zap.String("path", r.URL.Path),
		zap.String("query_keys", fmt.Sprintf("%v", keysOf(r.URL.Query()))))

	// Verify state parameter
	if state := r.URL.Query().Get("state"); state != "" && state != session.state {
		am.failSession(session, "state mismatch")
		writeCallbackHTML(w, false, "State mismatch")
		return
	}

	// Check for error from authorization server
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		if desc := r.URL.Query().Get("error_description"); desc != "" {
			errMsg = errMsg + ": " + desc
		}
		am.failSession(session, errMsg)
		writeCallbackHTML(w, false, errMsg)
		return
	}

	var tokenResult *callbackTokenResult

	// Strategy 1: Direct token return in query params
	if at := r.URL.Query().Get("access_token"); at != "" {
		tokenResult = &callbackTokenResult{
			AccessToken:   at,
			RefreshToken:  r.URL.Query().Get("refresh_token"),
			ClientID:      r.URL.Query().Get("client_id"),
			TokenEndpoint: r.URL.Query().Get("token_endpoint"),
			ExpiresAt:     r.URL.Query().Get("expires_at"),
		}
	}

	// Strategy 2: POST body with JSON tokens
	if tokenResult == nil && r.Method == http.MethodPost {
		body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
		if err == nil && len(body) > 0 {
			var t callbackTokenResult
			if json.Unmarshal(body, &t) == nil && t.AccessToken != "" {
				tokenResult = &t
			}
		}
	}

	// Strategy 3: Authorization code → exchange for tokens
	if tokenResult == nil {
		if code := r.URL.Query().Get("code"); code != "" {
			am.logger.Info("exchanging authorization code for tokens")
			result, err := am.exchangeCode(session, code)
			if err != nil {
				am.logger.Error("code exchange failed", zap.Error(err))
				am.failSession(session, "code exchange failed: "+err.Error())
				writeCallbackHTML(w, false, "Code exchange failed")
				return
			}
			tokenResult = result
		}
	}

	if tokenResult == nil || tokenResult.AccessToken == "" {
		am.failSession(session, "no token in callback")
		writeCallbackHTML(w, false, "No token received")
		return
	}

	// Success — populate session
	session.mu.Lock()
	session.AccessToken = tokenResult.AccessToken
	session.RefreshToken = tokenResult.RefreshToken
	session.ClientID = tokenResult.ClientID
	session.TokenEndpoint = tokenResult.TokenEndpoint
	if tokenResult.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, tokenResult.ExpiresAt); err == nil {
			session.TokenExpiresAt = t
		}
	}
	if session.TokenExpiresAt.IsZero() {
		if tokenResult.ExpiresIn > 0 {
			session.TokenExpiresAt = time.Now().Add(time.Duration(tokenResult.ExpiresIn) * time.Second)
		} else {
			session.TokenExpiresAt = time.Now().Add(1 * time.Hour)
		}
	}
	session.Status = "completed"
	session.mu.Unlock()

	am.logger.Info("Kiro PKCE login completed",
		zap.String("session_id", session.ID),
		zap.Bool("has_refresh", tokenResult.RefreshToken != ""),
		zap.Bool("has_client_id", tokenResult.ClientID != ""),
		zap.Bool("has_token_endpoint", tokenResult.TokenEndpoint != ""))

	close(session.done)
	go session.server.Close()

	writeCallbackHTML(w, true, "")
}

// exchangeCode exchanges an authorization code for tokens via the Kiro token endpoint.
func (am *KiroAuthManager) exchangeCode(session *KiroLoginSession, code string) (*callbackTokenResult, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("code_verifier", session.codeVerifier)
	form.Set("redirect_uri", fmt.Sprintf("http://localhost:%d", session.CallbackPort))

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.PostForm(kiroTokenExchangeURL, form)
	if err != nil {
		return nil, fmt.Errorf("exchange request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("exchange failed (%d): %s", resp.StatusCode, string(body))
	}

	var result callbackTokenResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse exchange response: %w", err)
	}

	return &result, nil
}

func (am *KiroAuthManager) failSession(session *KiroLoginSession, errMsg string) {
	session.mu.Lock()
	session.Status = "error"
	session.Error = errMsg
	session.mu.Unlock()

	select {
	case <-session.done:
	default:
		close(session.done)
	}
	go session.server.Close()
}

// writeCallbackHTML writes a response page for the browser after the OAuth callback.
func writeCallbackHTML(w http.ResponseWriter, success bool, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if success {
		fmt.Fprint(w, `<!DOCTYPE html><html><body style="display:flex;justify-content:center;align-items:center;height:100vh;font-family:system-ui;background:#0a0a0a;color:#fff"><div style="text-align:center"><h1 style="font-size:48px;margin:0">&#10004;</h1><h2>Login Successful</h2><p style="color:#888">You can close this page.</p></div></body></html>`)
	} else {
		fmt.Fprintf(w, `<!DOCTYPE html><html><body style="display:flex;justify-content:center;align-items:center;height:100vh;font-family:system-ui;background:#0a0a0a;color:#fff"><div style="text-align:center"><h1 style="font-size:48px;margin:0">&#10008;</h1><h2>Login Failed</h2><p style="color:#888">%s</p></div></body></html>`, errMsg)
	}
}

// PKCE helpers

func generateCodeVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func computeCodeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

func keysOf(vals url.Values) []string {
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	return keys
}
