package copilot

import (
	"net/http"
	"testing"
	"time"

	"go.uber.org/zap"
)

// ============================================================
// CopilotToken
// ============================================================

func TestCopilotToken_IsExpired_NilToken(t *testing.T) {
	var token *CopilotToken
	if !token.IsExpired() {
		t.Error("nil token should be expired")
	}
}

func TestCopilotToken_IsExpired_EmptyToken(t *testing.T) {
	token := &CopilotToken{Token: ""}
	if !token.IsExpired() {
		t.Error("empty token should be expired")
	}
}

func TestCopilotToken_IsExpired_FreshToken(t *testing.T) {
	token := &CopilotToken{
		Token:     "fresh-jwt",
		ExpiresAt: time.Now().Add(30 * time.Minute),
	}
	if token.IsExpired() {
		t.Error("fresh token with 30min remaining should not be expired")
	}
}

func TestCopilotToken_NeedsRefresh_NilToken(t *testing.T) {
	var token *CopilotToken
	if !token.NeedsRefresh(time.Now()) {
		t.Error("nil token should need refresh")
	}
}

func TestCopilotToken_NeedsRefresh_EmptyToken(t *testing.T) {
	token := &CopilotToken{Token: ""}
	if !token.NeedsRefresh(time.Now()) {
		t.Error("empty token should need refresh")
	}
}

func TestCopilotToken_NeedsRefresh_Fresh(t *testing.T) {
	now := time.Now()
	token := &CopilotToken{
		Token:     "jwt-token",
		ExpiresAt: now.Add(30 * time.Minute),
	}
	// Just issued, only 0% elapsed
	if token.NeedsRefresh(now) {
		t.Error("just-issued token should not need refresh yet")
	}
}

func TestCopilotToken_NeedsRefresh_AtThreshold(t *testing.T) {
	now := time.Now()
	// Token issued 30min ago, expires in 5min (total lifetime = 35min, 85% elapsed)
	// Threshold = issuedAt + 0.8 * 35min = (now-30m) + 28m = now - 2min → already passed
	token := &CopilotToken{
		Token:     "jwt-token",
		ExpiresAt: now.Add(5 * time.Minute),
	}
	issuedAt := now.Add(-30 * time.Minute)
	if !token.NeedsRefresh(issuedAt) {
		t.Error("token at 85% lifetime should need refresh")
	}
}

// ============================================================
// toCopilotModel
// ============================================================

func TestToCopilotModel_KnownAlias(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"gpt-4o", "gpt-4o"},
		{"gpt-4o-mini", "gpt-4o-mini"},
		{"claude-sonnet-4-20250514", "claude-sonnet-4-20250514"},
		{"gemini-2.0-flash", "gemini-2.0-flash"},
	}
	for _, tc := range tests {
		got := toCopilotModel(tc.input)
		if got != tc.want {
			t.Errorf("toCopilotModel(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestToCopilotModel_Unknown_Passthrough(t *testing.T) {
	got := toCopilotModel("some-new-model-2025")
	if got != "some-new-model-2025" {
		t.Errorf("unknown model should pass through, got %q", got)
	}
}

func TestToCopilotModel_TrimWhitespace(t *testing.T) {
	got := toCopilotModel("  gpt-4o  ")
	if got != "gpt-4o" {
		t.Errorf("should trim whitespace, got %q", got)
	}
}

// ============================================================
// AuthManager session management
// ============================================================

func TestAuthManager_NewAuthManager(t *testing.T) {
	am := NewAuthManager()
	if am == nil {
		t.Fatal("NewAuthManager returned nil")
	}
	if am.sessions == nil {
		t.Error("sessions map should be initialized")
	}
}

func TestAuthManager_GetSession_NotFound(t *testing.T) {
	am := NewAuthManager()
	_, ok := am.GetSession("nonexistent")
	if ok {
		t.Error("should return false for nonexistent session")
	}
}

func TestAuthManager_RemoveSession(t *testing.T) {
	am := NewAuthManager()

	// Manually add a session
	session := &AuthSession{
		ID:     "test-id",
		Status: "pending",
		stopCh: make(chan struct{}),
	}
	am.mu.Lock()
	am.sessions[session.ID] = session
	am.mu.Unlock()

	// Verify it exists
	_, ok := am.GetSession("test-id")
	if !ok {
		t.Fatal("session should exist")
	}

	// Remove it
	am.RemoveSession("test-id")

	_, ok = am.GetSession("test-id")
	if ok {
		t.Error("session should be removed")
	}
}

func TestAuthManager_RemoveSession_NonExistent(t *testing.T) {
	am := NewAuthManager()
	// Should not panic
	am.RemoveSession("does-not-exist")
}

// ============================================================
// setCopilotHeaders
// ============================================================

func TestSetCopilotHeaders(t *testing.T) {
	req, _ := http.NewRequest("POST", "https://example.com", nil)
	setCopilotHeaders(req, "test-token", "1.100.0")

	if got := req.Header.Get("Authorization"); got != "Bearer test-token" {
		t.Errorf("Authorization = %s, want Bearer test-token", got)
	}
	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %s", got)
	}
	if got := req.Header.Get("Editor-Version"); got != "vscode/1.100.0" {
		t.Errorf("Editor-Version = %s", got)
	}
	if got := req.Header.Get("Copilot-Integration-Id"); got != "vscode-chat" {
		t.Errorf("Copilot-Integration-Id = %s", got)
	}
}

func TestSetGithubHeaders(t *testing.T) {
	req, _ := http.NewRequest("POST", "https://example.com", nil)
	setGithubHeaders(req, "gh-token-123", "1.100.0")

	if got := req.Header.Get("Authorization"); got != "token gh-token-123" {
		t.Errorf("Authorization = %s, want 'token gh-token-123'", got)
	}
}

// ============================================================
// AccountInfo / ListAccounts
// ============================================================

func TestProvider_ListAccounts_Empty(t *testing.T) {
	p := &Provider{
		name:     "test",
		accounts: []*Account{},
	}
	infos := p.ListAccounts()
	if len(infos) != 0 {
		t.Errorf("expected 0 accounts, got %d", len(infos))
	}
}

func TestProvider_ListAccounts(t *testing.T) {
	p := &Provider{
		name: "test",
		accounts: []*Account{
			{
				GithubToken: "token1",
				Username:    "user1",
				healthy:     true,
				CopilotToken: &CopilotToken{
					Token:     "jwt1",
					ExpiresAt: time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC),
				},
			},
			{
				GithubToken: "token2",
				Username:    "user2",
				healthy:     false,
			},
		},
	}

	infos := p.ListAccounts()
	if len(infos) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(infos))
	}

	if infos[0].Username != "user1" || !infos[0].Healthy || !infos[0].HasToken {
		t.Errorf("account 0: %+v", infos[0])
	}
	if infos[1].Username != "user2" || infos[1].Healthy || infos[1].HasToken {
		t.Errorf("account 1: %+v", infos[1])
	}
}

// ============================================================
// pickAccount
// ============================================================

func TestProvider_PickAccount_NoAccounts(t *testing.T) {
	p := &Provider{accounts: []*Account{}}
	_, err := p.pickAccount()
	if err == nil {
		t.Error("should error with no accounts")
	}
}

func TestProvider_PickAccount_RoundRobin(t *testing.T) {
	p := &Provider{
		accounts: []*Account{
			{GithubToken: "a", healthy: true, CopilotToken: &CopilotToken{Token: "jwt-a"}},
			{GithubToken: "b", healthy: true, CopilotToken: &CopilotToken{Token: "jwt-b"}},
		},
	}

	first, _ := p.pickAccount()
	second, _ := p.pickAccount()

	// Should alternate (round-robin)
	if first.GithubToken == second.GithubToken {
		t.Error("round-robin should alternate accounts")
	}
}

func TestProvider_PickAccount_SkipsUnhealthy(t *testing.T) {
	p := &Provider{
		accounts: []*Account{
			{GithubToken: "sick", healthy: false, CopilotToken: &CopilotToken{Token: "jwt-sick"}},
			{GithubToken: "ok", healthy: true, CopilotToken: &CopilotToken{Token: "jwt-ok"}},
		},
	}

	for i := 0; i < 5; i++ {
		acc, err := p.pickAccount()
		if err != nil {
			t.Fatal(err)
		}
		if acc.GithubToken != "ok" {
			t.Errorf("should skip unhealthy, got %s", acc.GithubToken)
		}
	}
}

func TestProvider_PickAccount_SkipsNoToken(t *testing.T) {
	p := &Provider{
		accounts: []*Account{
			{GithubToken: "no-jwt", healthy: true}, // no CopilotToken
			{GithubToken: "has-jwt", healthy: true, CopilotToken: &CopilotToken{Token: "jwt"}},
		},
	}

	acc, err := p.pickAccount()
	if err != nil {
		t.Fatal(err)
	}
	if acc.GithubToken != "has-jwt" {
		t.Errorf("should skip account without token, got %s", acc.GithubToken)
	}
}

func TestProvider_PickAccount_AllUnhealthy(t *testing.T) {
	p := &Provider{
		accounts: []*Account{
			{GithubToken: "a", healthy: false, CopilotToken: &CopilotToken{Token: "jwt"}},
			{GithubToken: "b", healthy: false, CopilotToken: &CopilotToken{Token: "jwt"}},
		},
	}

	_, err := p.pickAccount()
	if err == nil {
		t.Error("should error when all accounts are unhealthy")
	}
}

// ============================================================
// Provider.AddAccount
// ============================================================

func TestProvider_AddAccount(t *testing.T) {
	p := &Provider{
		name:        "test",
		accounts:    []*Account{},
		authManager: NewAuthManager(),
		stopCh:      make(chan struct{}),
		client:      defaultTestClient(),
		vsCodeVer:   "1.100.0",
		logger:      testLogger(),
	}

	p.AddAccount("new-token")

	p.mu.Lock()
	count := len(p.accounts)
	p.mu.Unlock()

	if count != 1 {
		t.Errorf("expected 1 account after AddAccount, got %d", count)
	}

	// Cleanup
	close(p.stopCh)
}

// ============================================================
// Helpers
// ============================================================

func defaultTestClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Second}
}

func testLogger() *zap.Logger {
	logger, _ := zap.NewDevelopment()
	return logger
}
