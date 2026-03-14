package copilot

import (
	"net/http"
	"testing"
)

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
		t.Errorf("Content-Type = %s, got", got)
	}
	if got := req.Header.Get("Editor-Version"); got != "vscode/1.100.0" {
		t.Errorf("Editor-Version = %s, want vscode/1.100.0", got)
	}
	if got := req.Header.Get("Copilot-Integration-Id"); got != "vscode-chat" {
		t.Errorf("Copilot-Integration-Id = %s, want vscode-chat", got)
	}
}

func TestSetGithubHeaders(t *testing.T) {
	req, _ := http.NewRequest("POST", "https://example.com", nil)
	setGithubHeaders(req, "gh-token-123", "1.100.0")

	if got := req.Header.Get("Authorization"); got != "token gh-token-123" {
		t.Errorf("Authorization = %s, want 'token gh-token-123'", got)
	}
}
