package copilot

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	copilotVersion   = "0.39.1"
	githubAPIVersion = "2026-03-10"

	copilotChatURL       = "https://api.githubcopilot.com/chat/completions"
	copilotModelsURL     = "https://api.githubcopilot.com/models"
	copilotEmbedURL      = "https://api.githubcopilot.com/embeddings"
	vsCodeVersionURL     = "https://aur.archlinux.org/cgit/aur.git/plain/PKGBUILD?h=visual-studio-code-bin"
	defaultVSCodeVersion = "1.104.3"
)

// CopilotToken holds a Copilot JWT token with expiry info.
type CopilotToken struct {
	Token     string
	ExpiresAt time.Time
}

// IsExpired checks if the token needs refreshing (at 80% lifetime).
func (t *CopilotToken) IsExpired() bool {
	if t == nil || t.Token == "" {
		return true
	}
	remaining := time.Until(t.ExpiresAt)
	total := t.ExpiresAt.Sub(t.ExpiresAt.Add(-30 * time.Minute)) // approximate
	return remaining < total/5                                   // refresh at 80% lifetime
}

// NeedsRefresh returns true when 80% of the token's lifetime has elapsed.
func (t *CopilotToken) NeedsRefresh(issuedAt time.Time) bool {
	if t == nil || t.Token == "" {
		return true
	}
	lifetime := t.ExpiresAt.Sub(issuedAt)
	threshold := issuedAt.Add(time.Duration(float64(lifetime) * 0.8))
	return time.Now().After(threshold)
}

// setCopilotHeaders sets the standard Copilot API headers on a request.
func setCopilotHeaders(req *http.Request, token, vsCodeVersion string) {
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Editor-Version", "vscode/"+vsCodeVersion)
	req.Header.Set("Editor-Plugin-Version", "copilot-chat/"+copilotVersion)
	req.Header.Set("Copilot-Integration-Id", "vscode-chat")
	req.Header.Set("User-Agent", "GitHubCopilotChat/"+copilotVersion)
	req.Header.Set("X-GitHub-API-Version", githubAPIVersion)
}

// vsCodeVersionCache caches the fetched VSCode version.
var (
	vsCodeVersionMu    sync.Mutex
	cachedVSCodeVer    string
	vsCodeVerExpiresAt time.Time
)

// getVSCodeVersion fetches and caches the latest VSCode version.
func getVSCodeVersion(client *http.Client) string {
	vsCodeVersionMu.Lock()
	defer vsCodeVersionMu.Unlock()

	if cachedVSCodeVer != "" && time.Now().Before(vsCodeVerExpiresAt) {
		return cachedVSCodeVer
	}

	ver := fetchVSCodeVersion(client)
	cachedVSCodeVer = ver
	vsCodeVerExpiresAt = time.Now().Add(24 * time.Hour)
	return ver
}

func fetchVSCodeVersion(client *http.Client) string {
	ctx, cancel := contextWithTimeout(5 * time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", vsCodeVersionURL, nil)
	if err != nil {
		return defaultVSCodeVersion
	}

	resp, err := client.Do(req)
	if err != nil {
		return defaultVSCodeVersion
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return defaultVSCodeVersion
	}

	re := regexp.MustCompile(`pkgver=([0-9]+\.[0-9]+\.[0-9]+)`)
	matches := re.FindSubmatch(body)
	if len(matches) < 2 {
		return defaultVSCodeVersion
	}
	return string(matches[1])
}

// contextWithTimeout creates a context with timeout using context.Background.
func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// modelAliases maps common external model names to Copilot model IDs.
var modelAliases = map[string]string{
	// Claude models
	"claude-sonnet-4-20250514": "claude-sonnet-4-20250514",
	"claude-3.5-sonnet":        "claude-3.5-sonnet",
	"claude-3-opus":            "claude-3-opus",
	"claude-3-haiku":           "claude-3-haiku",
	// GPT models
	"gpt-4o":      "gpt-4o",
	"gpt-4o-mini": "gpt-4o-mini",
	"gpt-4-turbo": "gpt-4-turbo",
	"gpt-4":       "gpt-4",
	// Gemini models
	"gemini-2.0-flash": "gemini-2.0-flash",
	"gemini-2.5-pro":   "gemini-2.5-pro",
}

// toCopilotModel resolves a model name to the Copilot model ID.
// If the model is unknown, it passes through unchanged (Copilot may still support it).
func toCopilotModel(model string) string {
	model = strings.TrimSpace(model)
	if alias, ok := modelAliases[model]; ok {
		return alias
	}
	return model
}
