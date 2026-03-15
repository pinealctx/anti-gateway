package kiro

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	kiroSigninURL          = "https://app.kiro.dev/signin"
	kiroTokenExchangeURL   = "https://prod.us-east-1.auth.desktop.kiro.dev/token"
	externalIdPRedirectURI = "kiro://kiro.oauth/callback"
	defaultCallbackPort    = 3128
	loginTimeout           = 5 * time.Minute

	// builderIDScopes are the OAuth2 scopes for AWS Builder ID / IAM Identity Center.
	builderIDScopes = "codewhisperer:completions codewhisperer:analysis codewhisperer:conversations codewhisperer:transformations codewhisperer:taskassist"
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

	// External IdP (Enterprise SSO) — populated after first callback from Kiro
	externalIdP           bool   // true if login_option=external_idp was received
	externalIssuerURL     string // e.g. https://login.microsoftonline.com/{tenant}/v2.0
	externalClientID      string // e.g. e0d7fe97-...
	externalScopes        string // e.g. api://e0d7fe97-.../codewhisperer:conversations ...
	externalState         string // state for the external IdP leg
	externalVerifier      string // PKCE code_verifier for the external IdP leg
	externalAuthEndpoint  string // OIDC authorization_endpoint (discovered or derived)
	externalTokenEndpoint string // OIDC token_endpoint (discovered or derived)

	// Builder ID (AWS IAM Identity Center Device Authorization Flow)
	builderID              bool   // true if login_option=builderid was received
	builderIDOIDCBase      string // e.g. https://oidc.us-east-1.amazonaws.com
	builderIDClientID      string // dynamically registered AWS OIDC client_id
	builderIDClientSecret  string // dynamically registered AWS OIDC client_secret
	builderIDDeviceCode    string // device_code for polling
	builderIDUserCode      string // user_code to display
	builderIDVerifyURI     string // verificationUriComplete for browser
	builderIDInterval      int    // polling interval in seconds
	builderIDTokenEndpoint string // token endpoint for code exchange

	// Populated on completion
	AccessToken    string    `json:"-"`
	RefreshToken   string    `json:"-"`
	ClientID       string    `json:"-"`
	ClientSecret   string    `json:"-"` // non-empty for providers requiring client_secret (e.g. AWS OIDC)
	TokenEndpoint  string    `json:"-"`
	TokenExpiresAt time.Time `json:"-"`
	ProfileArn     string    `json:"-"`

	// Server management
	server *http.Server
	mu     sync.Mutex
	done   chan struct{}
}

// Mu returns the session's mutex for external synchronization.
func (s *KiroLoginSession) Mu() *sync.Mutex {
	return &s.mu
}

// IsExternalIdP reports whether this session used an external IdP or Builder ID login.
// Both require TokenType: EXTERNAL_IDP when calling CodeWhisperer.
func (s *KiroLoginSession) IsExternalIdP() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.externalIdP || s.builderID
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
			_ = s.server.Close()
		}
		delete(am.sessions, id)
	}
}

func (am *KiroAuthManager) startCallbackServer(session *KiroLoginSession) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		am.handleCallback(session, w, r)
	})
	// Session status endpoint — used by the device code page to detect completion.
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		session.mu.Lock()
		st := session.Status
		errStr := session.Error
		session.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": st, "error": errStr})
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
			_ = session.server.Close()
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
	ClientSecret  string `json:"client_secret,omitempty"` // for providers requiring client_secret (e.g. AWS OIDC)
	TokenEndpoint string `json:"token_endpoint"`
	ExpiresAt     string `json:"expires_at"`
	ExpiresIn     int    `json:"expires_in"`
	ProfileArn    string `json:"profileArn"`
}

func (am *KiroAuthManager) handleCallback(session *KiroLoginSession, w http.ResponseWriter, r *http.Request) {
	// Ignore non-OAuth noise requests (favicon.ico, browser prefetch, etc.)
	q := r.URL.Query()
	if q.Get("code") == "" && q.Get("access_token") == "" && q.Get("error") == "" &&
		q.Get("login_option") == "" && r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}

	am.logger.Info("OAuth callback received",
		zap.String("method", r.Method),
		zap.String("path", r.URL.Path),
		zap.String("query_keys", fmt.Sprintf("%v", keysOf(r.URL.Query()))))

	// Check for error from authorization server
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		if desc := r.URL.Query().Get("error_description"); desc != "" {
			errMsg = errMsg + ": " + desc
		}
		am.logger.Error("OAuth callback returned error",
			zap.String("error", errMsg),
			zap.String("full_url", r.URL.String()))
		am.failSession(session, errMsg)
		writeCallbackHTML(w, false, errMsg)
		return
	}

	// External IdP flow: Kiro tells us to redirect to Azure AD / external IdP
	if r.URL.Query().Get("login_option") == "external_idp" {
		am.handleExternalIdPRedirect(session, w, r)
		return
	}

	// Builder ID / AWS IdC (organization) flow: same Device Authorization Flow,
	// only differing in the issuerUrl/startUrl value.
	if lo := r.URL.Query().Get("login_option"); lo == "builderid" || lo == "awsidc" {
		am.handleBuilderIDRedirect(session, w, r)
		return
	}

	// External IdP second callback: user pasted back the kiro:// authorization code
	session.mu.Lock()
	isExternalIdP := session.externalIdP
	session.mu.Unlock()

	if isExternalIdP {
		am.handleExternalIdPCallback(session, w, r)
		return
	}

	// Verify state parameter (Kiro direct flow)
	if state := r.URL.Query().Get("state"); state != "" && state != session.state {
		am.failSession(session, "state mismatch")
		writeCallbackHTML(w, false, "State mismatch")
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

	am.completeSession(session, tokenResult)
	writeCallbackHTML(w, true, "")
}

// handleExternalIdPRedirect processes the first callback from Kiro with login_option=external_idp.
// Instead of a direct 302 to Azure AD (whose registered redirect_uri is kiro://kiro.oauth/callback),
// we serve an HTML page that opens Azure AD in a new tab and provides an input field for the user
// to paste the kiro:// callback URL containing the authorization code.
func (am *KiroAuthManager) handleExternalIdPRedirect(session *KiroLoginSession, w http.ResponseWriter, r *http.Request) {
	issuerURL := r.URL.Query().Get("issuer_url")
	clientID := r.URL.Query().Get("client_id")
	scopes := r.URL.Query().Get("scopes")

	if issuerURL == "" || clientID == "" {
		am.failSession(session, "external_idp callback missing issuer_url or client_id")
		writeCallbackHTML(w, false, "Missing IdP configuration")
		return
	}

	am.logger.Info("External IdP login detected, serving code paste page",
		zap.String("issuer_url", issuerURL),
		zap.String("client_id", clientID),
		zap.String("scopes", scopes))

	// Generate new PKCE parameters for the Azure AD leg
	verifier, err := generateCodeVerifier()
	if err != nil {
		am.failSession(session, "generate PKCE verifier: "+err.Error())
		writeCallbackHTML(w, false, "Internal error")
		return
	}
	challenge := computeCodeChallenge(verifier)
	state := uuid.New().String()

	// Discover OIDC endpoints; fall back to Azure AD URL pattern for Microsoft issuers.
	authEndpoint, tokenEndpoint, discErr := oidcDiscover(issuerURL)
	if discErr != nil {
		am.logger.Warn("OIDC discovery failed, falling back to Azure AD URL pattern",
			zap.String("issuer_url", issuerURL),
			zap.Error(discErr))
		base := strings.TrimSuffix(issuerURL, "/v2.0")
		authEndpoint = base + "/oauth2/v2.0/authorize"
		tokenEndpoint = base + "/oauth2/v2.0/token"
	} else {
		am.logger.Info("OIDC discovery succeeded",
			zap.String("auth_endpoint", authEndpoint),
			zap.String("token_endpoint", tokenEndpoint))
	}

	// Save external IdP state in session
	session.mu.Lock()
	session.externalIdP = true
	session.externalIssuerURL = issuerURL
	session.externalClientID = clientID
	session.externalScopes = scopes
	session.externalState = state
	session.externalVerifier = verifier
	session.externalAuthEndpoint = authEndpoint
	session.externalTokenEndpoint = tokenEndpoint
	session.mu.Unlock()

	params := url.Values{}
	params.Set("client_id", clientID)
	params.Set("response_type", "code")
	params.Set("redirect_uri", externalIdPRedirectURI)
	params.Set("scope", scopes)
	params.Set("state", state)
	params.Set("code_challenge", challenge)
	params.Set("code_challenge_method", "S256")
	params.Set("response_mode", "query")

	if hint := r.URL.Query().Get("login_hint"); hint != "" {
		params.Set("login_hint", hint)
	}

	authURL := authEndpoint + "?" + params.Encode()
	writeExternalIdPPage(w, authURL)
}

// handleExternalIdPCallback processes the second callback from Azure AD with an authorization code.
func (am *KiroAuthManager) handleExternalIdPCallback(session *KiroLoginSession, w http.ResponseWriter, r *http.Request) {
	// Verify state
	session.mu.Lock()
	expectedState := session.externalState
	session.mu.Unlock()

	if state := r.URL.Query().Get("state"); state != expectedState {
		am.failSession(session, "external IdP state mismatch")
		writeCallbackHTML(w, false, "State mismatch")
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		am.failSession(session, "no authorization code from external IdP")
		writeCallbackHTML(w, false, "No authorization code received")
		return
	}

	am.logger.Info("exchanging external IdP authorization code for tokens")

	// Exchange code at the discovered token endpoint
	session.mu.Lock()
	clientID := session.externalClientID
	verifier := session.externalVerifier
	scopes := session.externalScopes
	tokenEndpoint := session.externalTokenEndpoint
	session.mu.Unlock()

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", clientID)
	form.Set("code", code)
	form.Set("redirect_uri", externalIdPRedirectURI)
	form.Set("code_verifier", verifier)
	form.Set("scope", scopes)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.PostForm(tokenEndpoint, form)
	if err != nil {
		am.failSession(session, "external IdP token exchange: "+err.Error())
		writeCallbackHTML(w, false, "Token exchange failed")
		return
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	if resp.StatusCode != http.StatusOK {
		am.logger.Error("external IdP token exchange failed",
			zap.Int("status", resp.StatusCode),
			zap.String("body", string(body)))
		am.failSession(session, fmt.Sprintf("external IdP token exchange failed (%d)", resp.StatusCode))
		writeCallbackHTML(w, false, "Token exchange failed")
		return
	}

	var azResult struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &azResult); err != nil {
		am.failSession(session, "parse external IdP token response: "+err.Error())
		writeCallbackHTML(w, false, "Token parse failed")
		return
	}

	if azResult.AccessToken == "" {
		am.failSession(session, "empty access token from external IdP")
		writeCallbackHTML(w, false, "No access token received")
		return
	}

	am.logger.Info("External IdP token exchange successful",
		zap.Bool("has_refresh", azResult.RefreshToken != ""),
		zap.Int("expires_in", azResult.ExpiresIn))

	tokenResult := &callbackTokenResult{
		AccessToken:   azResult.AccessToken,
		RefreshToken:  azResult.RefreshToken,
		ClientID:      clientID,
		TokenEndpoint: tokenEndpoint,
		ExpiresIn:     azResult.ExpiresIn,
	}

	am.completeSession(session, tokenResult)
	writeCallbackHTML(w, true, "")
}

// completeSession finalizes a login session with the obtained tokens.
func (am *KiroAuthManager) completeSession(session *KiroLoginSession, tokenResult *callbackTokenResult) {
	session.mu.Lock()
	session.AccessToken = tokenResult.AccessToken
	session.RefreshToken = tokenResult.RefreshToken
	session.ClientID = tokenResult.ClientID
	session.ClientSecret = tokenResult.ClientSecret
	session.TokenEndpoint = tokenResult.TokenEndpoint
	session.ProfileArn = tokenResult.ProfileArn
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

	am.logger.Info("Kiro login completed",
		zap.String("session_id", session.ID),
		zap.Bool("has_refresh", tokenResult.RefreshToken != ""),
		zap.Bool("has_client_id", tokenResult.ClientID != ""),
		zap.Bool("has_client_secret", tokenResult.ClientSecret != ""),
		zap.Bool("has_token_endpoint", tokenResult.TokenEndpoint != ""),
		zap.Bool("external_idp", session.externalIdP),
		zap.Bool("builder_id", session.builderID))

	close(session.done)
	go func() { _ = session.server.Close() }()
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
	defer func() { _ = resp.Body.Close() }()

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
	go func() { _ = session.server.Close() }()
}

// handleBuilderIDRedirect processes the first callback from Kiro with login_option=builderid.
// It uses the AWS SSO OIDC Device Authorization Flow:
//  1. Register a client → get clientId + clientSecret
//  2. Start device_authorization → get deviceCode + userCode + verificationUriComplete
//  3. Show the user a page with the verification URL to open
//  4. Poll create_token in the background until the user authorizes
func (am *KiroAuthManager) handleBuilderIDRedirect(session *KiroLoginSession, w http.ResponseWriter, r *http.Request) {
	idcRegion := r.URL.Query().Get("idc_region")
	if idcRegion == "" {
		idcRegion = "us-east-1"
	}
	startURL := r.URL.Query().Get("issuer_url")
	if startURL == "" {
		startURL = "https://view.awsapps.com/start"
	}
	// Clean up the start URL: strip hash fragment and trailing slashes.
	// e.g. "https://d-xxx.awsapps.com/start/#/?tab=accounts" → "https://d-xxx.awsapps.com/start"
	if idx := strings.Index(startURL, "#"); idx >= 0 {
		startURL = startURL[:idx]
	}
	startURL = strings.TrimRight(startURL, "/")
	oidcBase := "https://oidc." + idcRegion + ".amazonaws.com"
	tokenEndpoint := oidcBase + "/token"

	am.logger.Info("Builder ID login detected (device flow)",
		zap.String("idc_region", idcRegion),
		zap.String("oidc_base", oidcBase),
		zap.String("start_url", startURL))

	// Step 1: Register client
	httpClient := &http.Client{Timeout: 15 * time.Second}
	regBody, _ := json.Marshal(map[string]interface{}{
		"clientName": "Kiro IDE",
		"clientType": "public",
		"scopes":     strings.Fields(builderIDScopes),
		"grantTypes": []string{"urn:ietf:params:oauth:grant-type:device_code", "refresh_token"},
		"issuerUrl":  startURL,
	})
	regResp, err := httpClient.Post(oidcBase+"/client/register", "application/json", bytes.NewReader(regBody))
	if err != nil {
		am.failSession(session, "AWS OIDC register: "+err.Error())
		writeCallbackHTML(w, false, "Client registration failed")
		return
	}
	regRespBody, _ := io.ReadAll(io.LimitReader(regResp.Body, 32*1024))
	_ = regResp.Body.Close()
	if regResp.StatusCode != http.StatusOK {
		am.logger.Error("AWS OIDC client registration failed",
			zap.Int("status", regResp.StatusCode),
			zap.String("body", string(regRespBody)))
		am.failSession(session, "AWS OIDC register failed")
		writeCallbackHTML(w, false, "Client registration failed")
		return
	}
	var regResult struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}
	_ = json.Unmarshal(regRespBody, &regResult)
	if regResult.ClientID == "" {
		am.failSession(session, "empty clientId from registration")
		writeCallbackHTML(w, false, "Registration returned empty clientId")
		return
	}
	am.logger.Info("AWS OIDC client registered", zap.String("client_id", regResult.ClientID))

	// Step 2: Start device authorization
	authBody, _ := json.Marshal(map[string]string{
		"clientId":     regResult.ClientID,
		"clientSecret": regResult.ClientSecret,
		"startUrl":     startURL,
	})
	authResp, err := httpClient.Post(oidcBase+"/device_authorization", "application/json", bytes.NewReader(authBody))
	if err != nil {
		am.failSession(session, "device_authorization: "+err.Error())
		writeCallbackHTML(w, false, "Device authorization failed")
		return
	}
	authRespBody, _ := io.ReadAll(io.LimitReader(authResp.Body, 32*1024))
	_ = authResp.Body.Close()
	if authResp.StatusCode != http.StatusOK {
		am.logger.Error("device_authorization failed",
			zap.Int("status", authResp.StatusCode),
			zap.String("body", string(authRespBody)))
		am.failSession(session, "device_authorization failed")
		writeCallbackHTML(w, false, "Device authorization failed")
		return
	}
	var daResult struct {
		DeviceCode              string `json:"deviceCode"`
		UserCode                string `json:"userCode"`
		VerificationURI         string `json:"verificationUri"`
		VerificationURIComplete string `json:"verificationUriComplete"`
		ExpiresIn               int    `json:"expiresIn"`
		Interval                int    `json:"interval"`
	}
	_ = json.Unmarshal(authRespBody, &daResult)
	if daResult.DeviceCode == "" || daResult.UserCode == "" {
		am.failSession(session, "device_authorization missing deviceCode/userCode")
		writeCallbackHTML(w, false, "Device authorization incomplete")
		return
	}
	if daResult.Interval < 1 {
		daResult.Interval = 5
	}

	am.logger.Info("Builder ID device authorization started",
		zap.String("user_code", daResult.UserCode),
		zap.String("verify_url", daResult.VerificationURIComplete),
		zap.Int("interval", daResult.Interval))

	// Save state in session
	session.mu.Lock()
	session.builderID = true
	session.builderIDOIDCBase = oidcBase
	session.builderIDClientID = regResult.ClientID
	session.builderIDClientSecret = regResult.ClientSecret
	session.builderIDDeviceCode = daResult.DeviceCode
	session.builderIDUserCode = daResult.UserCode
	session.builderIDVerifyURI = daResult.VerificationURIComplete
	session.builderIDInterval = daResult.Interval
	session.builderIDTokenEndpoint = tokenEndpoint
	session.mu.Unlock()

	// Step 3: Start background polling for device code completion
	go am.pollBuilderIDDeviceCode(session)

	// Step 4: Show the user a page with the code and verification URL
	writeBuilderIDDevicePage(w, daResult.UserCode, daResult.VerificationURIComplete)
}

// pollBuilderIDDeviceCode polls the AWS SSO OIDC create_token endpoint
// until the user authorizes the device code or the session expires/fails.
func (am *KiroAuthManager) pollBuilderIDDeviceCode(session *KiroLoginSession) {
	session.mu.Lock()
	oidcBase := session.builderIDOIDCBase
	clientID := session.builderIDClientID
	clientSecret := session.builderIDClientSecret
	deviceCode := session.builderIDDeviceCode
	interval := session.builderIDInterval
	tokenEndpoint := session.builderIDTokenEndpoint
	session.mu.Unlock()

	httpClient := &http.Client{Timeout: 30 * time.Second}
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-session.done:
			return
		case <-ticker.C:
		}

		reqBody, _ := json.Marshal(map[string]string{
			"clientId":     clientID,
			"clientSecret": clientSecret,
			"deviceCode":   deviceCode,
			"grantType":    "urn:ietf:params:oauth:grant-type:device_code",
		})
		resp, err := httpClient.Post(oidcBase+"/token", "application/json", bytes.NewReader(reqBody))
		if err != nil {
			am.logger.Warn("Builder ID poll error", zap.Error(err))
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
		_ = resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			// Success!
			var tokenData struct {
				AccessToken  string `json:"accessToken"`
				RefreshToken string `json:"refreshToken"`
				ExpiresIn    int    `json:"expiresIn"`
			}
			if err := json.Unmarshal(body, &tokenData); err != nil {
				am.failSession(session, "parse Builder ID token: "+err.Error())
				return
			}
			am.logger.Info("Builder ID device flow completed",
				zap.Bool("has_refresh", tokenData.RefreshToken != ""),
				zap.Int("expires_in", tokenData.ExpiresIn))

			tokenResult := &callbackTokenResult{
				AccessToken:   tokenData.AccessToken,
				RefreshToken:  tokenData.RefreshToken,
				ClientID:      clientID,
				ClientSecret:  clientSecret,
				TokenEndpoint: tokenEndpoint,
				ExpiresIn:     tokenData.ExpiresIn,
			}
			am.completeSession(session, tokenResult)
			return
		}

		// Check error type
		var errResp struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &errResp)

		switch errResp.Error {
		case "authorization_pending":
			// User hasn't authorized yet, keep polling
			continue
		case "slow_down":
			// Increase interval
			ticker.Reset(time.Duration(interval+5) * time.Second)
			continue
		case "expired_token":
			am.failSession(session, "device code expired")
			return
		default:
			am.logger.Error("Builder ID poll failed",
				zap.Int("status", resp.StatusCode),
				zap.String("body", string(body)))
			am.failSession(session, "Builder ID poll: "+errResp.Error)
			return
		}
	}
}

// writeBuilderIDDevicePage serves an HTML page showing the Builder ID device code
// and a link to the verification URL.
func writeBuilderIDDevicePage(w http.ResponseWriter, userCode, verifyURL string) {
	const tpl = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<title>Builder ID Login</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;background:#0a0a0a;color:#e5e5e5;min-height:100vh;display:flex;justify-content:center;align-items:center}
.card{background:#1a1a1a;border-radius:12px;padding:32px;max-width:480px;width:100%;box-shadow:0 4px 24px rgba(0,0,0,.5);text-align:center}
h2{font-size:20px;margin-bottom:20px;color:#fff}
.code{font-size:36px;font-weight:700;letter-spacing:4px;color:#60a5fa;background:#111;padding:16px 24px;border-radius:8px;margin:20px 0;font-family:'Courier New',monospace}
.step{margin:14px 0;padding:14px;background:#111;border-radius:8px;border-left:3px solid #3b82f6;font-size:14px;line-height:1.6;text-align:left}
.step-num{font-weight:700;color:#3b82f6;margin-right:4px}
.btn-open{background:#3b82f6;color:#fff;display:inline-block;text-decoration:none;padding:10px 20px;border-radius:6px;font-weight:500;font-size:14px}
.btn-open:hover{background:#60a5fa}
.hint{color:#888;font-size:13px;margin-top:16px}
.spinner{margin-top:16px;color:#888;font-size:14px}
</style>
</head>
<body>
<div class="card">
<h2>&#128274; AWS Builder ID Login</h2>
<div class="step">
<span class="step-num">Step 1:</span> 点击下方按钮打开 AWS Builder ID 授权页面。
<br><br>
<a class="btn-open" href="{{.VerifyURL}}" target="_blank" rel="noopener noreferrer">打开 AWS Builder ID &#8594;</a>
</div>
<div class="step">
<span class="step-num">Step 2:</span> 在页面中输入以下验证码：
<div class="code">{{.UserCode}}</div>
</div>
<div class="step">
<span class="step-num">Step 3:</span> 完成 AWS 授权后，此页面会自动关闭。
</div>
<div class="spinner" id="spinner">&#9203; 等待授权中...</div>
<div class="hint" id="hint">授权完成后请检查管理面板确认登录状态</div>
</div>
<script>
(function(){
  var spinner = document.getElementById('spinner');
  var hint = document.getElementById('hint');
  function poll(){
    fetch('/status').then(function(r){return r.json()}).then(function(d){
      if(d.status==='completed'){
        spinner.innerHTML='&#9989; 授权完成！';
        spinner.style.color='#22c55e';
        hint.textContent='页面将在 3 秒后自动关闭...';
        setTimeout(function(){window.close();},3000);
      } else if(d.status==='error'||d.status==='expired'){
        spinner.innerHTML='&#10060; '+(d.error||'登录失败');
        spinner.style.color='#ef4444';
        hint.textContent='请关闭此页面后重试';
      } else {
        setTimeout(poll,3000);
      }
    }).catch(function(){setTimeout(poll,5000);});
  }
  setTimeout(poll,3000);
})();
</script>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := struct {
		UserCode  string
		VerifyURL string
	}{UserCode: userCode, VerifyURL: verifyURL}
	t := template.Must(template.New("builderid").Parse(tpl))
	_ = t.Execute(w, data)
}

// oidcDiscover fetches the OIDC discovery document from {issuer}/.well-known/openid-configuration
// and returns the authorization_endpoint and token_endpoint.
// This supports any standards-compliant IdP (Azure AD, Okta, Google, Ping Identity, AWS OIDC, etc.).
func oidcDiscover(issuerURL string) (authEndpoint, tokenEndpoint string, err error) {
	discoveryURL := strings.TrimRight(issuerURL, "/") + "/.well-known/openid-configuration"
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(discoveryURL)
	if err != nil {
		return "", "", fmt.Errorf("OIDC discovery request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("OIDC discovery status: %d", resp.StatusCode)
	}
	var doc struct {
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint         string `json:"token_endpoint"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&doc); err != nil {
		return "", "", fmt.Errorf("OIDC discovery parse: %w", err)
	}
	if doc.AuthorizationEndpoint == "" || doc.TokenEndpoint == "" {
		return "", "", fmt.Errorf("OIDC discovery missing endpoints")
	}
	return doc.AuthorizationEndpoint, doc.TokenEndpoint, nil
}

// writeExternalIdPPage serves an HTML page that opens the external IdP login in a new tab
// and provides an input field for the user to paste the kiro:// callback URL.
func writeExternalIdPPage(w http.ResponseWriter, authURL string) {
	const tpl = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<title>Enterprise SSO Login</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;background:#0a0a0a;color:#e5e5e5;min-height:100vh;display:flex;justify-content:center;align-items:center}
.card{background:#1a1a1a;border-radius:12px;padding:32px;max-width:560px;width:100%;box-shadow:0 4px 24px rgba(0,0,0,.5)}
h2{font-size:20px;margin-bottom:20px;color:#fff}
.step{margin:14px 0;padding:14px;background:#111;border-radius:8px;border-left:3px solid #3b82f6;font-size:14px;line-height:1.6}
.step-num{font-weight:700;color:#3b82f6;margin-right:4px}
code{background:#222;padding:2px 6px;border-radius:3px;font-size:13px;color:#93c5fd}
input[type="text"]{width:100%;padding:10px 12px;border:1px solid #333;border-radius:6px;font-size:14px;background:#111;color:#e5e5e5;outline:none;transition:border-color .2s}
input[type="text"]:focus{border-color:#3b82f6}
button{padding:10px 20px;border:none;border-radius:6px;font-size:14px;cursor:pointer;font-weight:500;transition:background .2s}
.btn-open{background:#3b82f6;color:#fff;display:inline-block;text-decoration:none;padding:10px 20px;border-radius:6px;font-weight:500}
.btn-open:hover{background:#60a5fa}
.btn-submit{background:#22c55e;color:#fff;margin-top:12px;width:100%}
.btn-submit:hover{background:#4ade80}
.btn-submit:disabled{background:#333;color:#666;cursor:not-allowed}
.error{color:#ef4444;font-size:13px;margin-top:8px;display:none}
.hint{color:#666;font-size:13px;margin-top:6px}
.spinner{display:none;text-align:center;padding:20px;color:#888}
</style>
</head>
<body>
<div class="card">
<h2>&#128274; Enterprise SSO Login</h2>
<div class="step">
<span class="step-num">Step 1:</span> 点击下方按钮打开 Azure AD 登录页面。
<br><br>
<a class="btn-open" id="openBtn" href="#" target="_blank" rel="noopener noreferrer">打开 Azure AD 登录 &#8594;</a>
</div>
<div class="step">
<span class="step-num">Step 2:</span> 为避免被 Kiro 客户端自动拦截，建议在即将跳转的页面先打开 F12（开发者工具）；当页面准备跳转到 <code>kiro://...</code> 时，复制该完整 URL（包含 <code>code</code> 和 <code>state</code> 参数）。
</div>
<div class="step">
<span class="step-num">Step 3:</span> 将 URL 粘贴到下方输入框并提交。
<br><br>
<input type="text" id="callbackUrl" placeholder="kiro://kiro.oauth/callback?code=...&amp;state=..." autocomplete="off">
<div class="hint">URL 应包含 <code>code=</code> 参数</div>
<div class="error" id="error"></div>
<br>
<button class="btn-submit" id="submitBtn" disabled>提交</button>
</div>
<div class="spinner" id="spinner">&#9203; 正在交换令牌...</div>
</div>
<div class="result" id="result" style="display:none;text-align:center;padding:24px"></div>
<input type="hidden" id="authUrl" value="{{.}}">
<script>
(function(){
var authURL=document.getElementById('authUrl').value;
document.getElementById('openBtn').href=authURL;
var input=document.getElementById('callbackUrl');
var btn=document.getElementById('submitBtn');
var errEl=document.getElementById('error');
input.addEventListener('input',function(){
var v=input.value.trim();errEl.style.display='none';
if(!v){btn.disabled=true;return}
try{var u=new URL(v.replace(/^kiro:\/\//,'https://'));
btn.disabled=!u.searchParams.get('code');
if(!u.searchParams.get('code')){errEl.textContent='URL 中未找到 code 参数';errEl.style.display='block'}}
catch(e){btn.disabled=true;errEl.textContent='无效的 URL 格式';errEl.style.display='block'}});
btn.addEventListener('click',function(){
var v=input.value.trim();
try{var u=new URL(v.replace(/^kiro:\/\//,'https://'));
var code=u.searchParams.get('code');var state=u.searchParams.get('state');
if(code){var p=new URLSearchParams();p.set('code',code);if(state)p.set('state',state);
document.querySelector('.card').style.display='none';document.getElementById('spinner').style.display='block';
fetch('/?'+p.toString()).then(function(r){return r.text()}).then(function(html){
document.getElementById('spinner').style.display='none';
var res=document.getElementById('result');res.style.display='block';
if(html.indexOf('&#10004;')>=0){res.innerHTML='<h1 style="font-size:48px">&#10004;</h1><h2 style="color:#22c55e">登录成功</h2><p style="color:#888;margin-top:8px">可以关闭此页面了</p>';
}else{res.innerHTML='<h1 style="font-size:48px">&#10008;</h1><h2 style="color:#ef4444">登录失败</h2><p style="color:#888;margin-top:8px">请查看服务器日志</p>';}
}).catch(function(e){
document.getElementById('spinner').style.display='none';
document.querySelector('.card').style.display='block';
errEl.textContent='请求失败: '+e.message;errEl.style.display='block';
});}}
catch(e){errEl.textContent='解析 URL 失败';errEl.style.display='block'}});
})();
</script>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	t := template.Must(template.New("extidp").Parse(tpl))
	_ = t.Execute(w, authURL)
}

// writeCallbackHTML writes a response page for the browser after the OAuth callback.
func writeCallbackHTML(w http.ResponseWriter, success bool, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if success {
		_, _ = fmt.Fprint(w, `<!DOCTYPE html><html><body style="display:flex;justify-content:center;align-items:center;height:100vh;font-family:system-ui;background:#0a0a0a;color:#fff"><div style="text-align:center"><h1 style="font-size:48px;margin:0">&#10004;</h1><h2>Login Successful</h2><p style="color:#888">You can close this page.</p></div></body></html>`)
	} else {
		_, _ = fmt.Fprintf(w, `<!DOCTYPE html><html><body style="display:flex;justify-content:center;align-items:center;height:100vh;font-family:system-ui;background:#0a0a0a;color:#fff"><div style="text-align:center"><h1 style="font-size:48px;margin:0">&#10008;</h1><h2>Login Failed</h2><p style="color:#888">%s</p></div></body></html>`, errMsg)
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
