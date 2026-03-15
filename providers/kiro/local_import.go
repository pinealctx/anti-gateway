package kiro

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// DefaultKiroDBPath returns the platform-specific default path for the kiro-cli SQLite database.
// Returns empty string on unsupported platforms.
func DefaultKiroDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "kiro-cli", "data.sqlite3")
	case "linux":
		return filepath.Join(home, ".local", "share", "kiro-cli", "data.sqlite3")
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "kiro-cli", "data.sqlite3")
		}
		return filepath.Join(home, "AppData", "Roaming", "kiro-cli", "data.sqlite3")
	default:
		return ""
	}
}

// ImportLocalToken reads the kiro-cli SQLite database and returns a LoginToken.
// dbPath is the path to data.sqlite3; if empty, the platform default is used.
// It tries the new external-idp token first, then falls back to the legacy IdC token.
func ImportLocalToken(dbPath string) (*LoginToken, error) {
	if dbPath == "" {
		dbPath = DefaultKiroDBPath()
	}
	if dbPath == "" {
		return nil, fmt.Errorf("cannot determine kiro-cli database path on %s; please specify db_path", runtime.GOOS)
	}
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("kiro-cli database not found at %s: %w", dbPath, err)
	}

	db, err := sql.Open("sqlite", dbPath+"?mode=ro&_journal_mode=WAL&_busy_timeout=3000")
	if err != nil {
		return nil, fmt.Errorf("open kiro-cli database: %w", err)
	}
	defer db.Close()

	// Try new external-idp token first
	lt, err := readExternalIdPToken(db)
	if err == nil {
		return lt, nil
	}

	// Fall back to legacy IdC token
	lt, err = readLegacyIdCToken(db)
	if err == nil {
		return lt, nil
	}

	return nil, fmt.Errorf("no kiro-cli token found in database; please run 'kiro-cli login' first")
}

// readExternalIdPToken reads the new-style Microsoft OAuth2 token from auth_kv.
func readExternalIdPToken(db *sql.DB) (*LoginToken, error) {
	var raw string
	err := db.QueryRow("SELECT value FROM auth_kv WHERE key='kirocli:external-idp:token'").Scan(&raw)
	if err != nil {
		return nil, err
	}

	var data struct {
		AccessToken   string `json:"access_token"`
		RefreshToken  string `json:"refresh_token"`
		ExpiresAt     string `json:"expires_at"`
		ClientID      string `json:"client_id"`
		TokenEndpoint string `json:"token_endpoint"`
	}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, fmt.Errorf("parse external-idp token: %w", err)
	}
	if data.AccessToken == "" {
		return nil, fmt.Errorf("empty access_token in external-idp token")
	}

	expiresAt := parseKiroTimestamp(data.ExpiresAt)
	profileArn := readProfileArn(db)

	lt := &LoginToken{
		AccessToken:   data.AccessToken,
		RefreshToken:  data.RefreshToken,
		ClientID:      data.ClientID,
		TokenEndpoint: data.TokenEndpoint,
		ExpiresAt:     expiresAt,
		IsExternalIdP: true,
		ProfileArn:    profileArn,
	}

	// Extract refresh scope from JWT for Azure AD
	if lt.TokenEndpoint != "" && !isAWSIdCEndpoint(lt.TokenEndpoint) {
		lt.RefreshScope = extractRefreshScope(lt.AccessToken)
	}

	return lt, nil
}

// readLegacyIdCToken reads the old-style Builder ID / IdC token from auth_kv.
func readLegacyIdCToken(db *sql.DB) (*LoginToken, error) {
	var raw string
	err := db.QueryRow("SELECT value FROM auth_kv WHERE key='kirocli:odic:token'").Scan(&raw)
	if err != nil {
		return nil, err
	}

	var data struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresAt    string `json:"expires_at"`
	}
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, fmt.Errorf("parse legacy IdC token: %w", err)
	}
	if data.AccessToken == "" {
		return nil, fmt.Errorf("empty access_token in legacy IdC token")
	}

	// Read device registration for client_id/client_secret
	var clientID, clientSecret string
	var regRaw string
	err = db.QueryRow("SELECT value FROM auth_kv WHERE key='kirocli:odic:device-registration'").Scan(&regRaw)
	if err == nil {
		var reg struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		}
		if json.Unmarshal([]byte(regRaw), &reg) == nil {
			clientID = reg.ClientID
			clientSecret = reg.ClientSecret
		}
	}

	expiresAt := parseKiroTimestamp(data.ExpiresAt)
	profileArn := readProfileArn(db)

	return &LoginToken{
		AccessToken:   data.AccessToken,
		RefreshToken:  data.RefreshToken,
		ClientID:      clientID,
		ClientSecret:  clientSecret,
		TokenEndpoint: "https://oidc.us-east-1.amazonaws.com/token", // legacy default
		ExpiresAt:     expiresAt,
		IsExternalIdP: true,
		ProfileArn:    profileArn,
	}, nil
}

// readProfileArn reads the CodeWhisperer profile ARN from the state table.
func readProfileArn(db *sql.DB) string {
	var raw string
	err := db.QueryRow("SELECT value FROM state WHERE key='api.codewhisperer.profile'").Scan(&raw)
	if err != nil {
		return ""
	}
	var profile struct {
		Arn string `json:"arn"`
	}
	if json.Unmarshal([]byte(raw), &profile) == nil {
		return profile.Arn
	}
	return ""
}

// parseKiroTimestamp parses kiro-cli's ISO 8601 timestamp format.
// e.g. "2026-03-15T12:00:00.000Z" or "2026-03-15T12:00:00Z"
func parseKiroTimestamp(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	// Try common formats
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	// Last resort: strip fractional seconds and trailing Z
	s = strings.TrimSuffix(s, "Z")
	if idx := strings.LastIndex(s, "."); idx > 0 {
		s = s[:idx]
	}
	if t, err := time.Parse("2006-01-02T15:04:05", s); err == nil {
		return t.UTC()
	}
	return time.Time{}
}
