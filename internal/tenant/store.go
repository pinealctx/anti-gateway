package tenant

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Store manages API keys and usage records.
type Store struct {
	db *sql.DB
	mu sync.RWMutex
	// In-memory key cache for fast auth lookup
	cache map[string]*APIKey // key string → APIKey
}

// NewStore opens (or creates) an SQLite database for tenant management.
func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open tenant db: %w", err)
	}

	s := &Store{
		db:    db,
		cache: make(map[string]*APIKey),
	}

	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}

	if err := s.loadCache(); err != nil {
		db.Close()
		return nil, err
	}

	return s, nil
}

func (s *Store) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS api_keys (
		id          TEXT PRIMARY KEY,
		key         TEXT UNIQUE NOT NULL,
		name        TEXT NOT NULL DEFAULT '',
		enabled     INTEGER NOT NULL DEFAULT 1,
		allowed_models    TEXT NOT NULL DEFAULT '[]',
		allowed_providers TEXT NOT NULL DEFAULT '[]',
		default_provider  TEXT NOT NULL DEFAULT '',
		qpm         INTEGER NOT NULL DEFAULT 0,
		tpm         INTEGER NOT NULL DEFAULT 0,
		metadata    TEXT NOT NULL DEFAULT '{}',
		created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS usage_records (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		key_id        TEXT NOT NULL,
		model         TEXT NOT NULL DEFAULT '',
		provider      TEXT NOT NULL DEFAULT '',
		input_tokens  INTEGER NOT NULL DEFAULT 0,
		output_tokens INTEGER NOT NULL DEFAULT 0,
		total_tokens  INTEGER NOT NULL DEFAULT 0,
		duration_ms   REAL    NOT NULL DEFAULT 0,
		created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (key_id) REFERENCES api_keys(id)
	);

	CREATE INDEX IF NOT EXISTS idx_usage_key_time ON usage_records(key_id, created_at);
	CREATE INDEX IF NOT EXISTS idx_usage_model ON usage_records(model, created_at);
	`
	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("migrate tenant db: %w", err)
	}
	// Safe column addition for existing databases
	s.db.Exec("ALTER TABLE api_keys ADD COLUMN default_provider TEXT NOT NULL DEFAULT ''")
	return nil
}

func (s *Store) loadCache() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query("SELECT id, key, name, enabled, allowed_models, allowed_providers, default_provider, qpm, tpm, metadata, created_at, updated_at FROM api_keys")
	if err != nil {
		return err
	}
	defer rows.Close()

	s.cache = make(map[string]*APIKey)
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return err
		}
		s.cache[k.Key] = k
	}
	return rows.Err()
}

func scanKey(rows *sql.Rows) (*APIKey, error) {
	var k APIKey
	var modelsJSON, providersJSON, metaJSON string
	var enabled int
	err := rows.Scan(&k.ID, &k.Key, &k.Name, &enabled, &modelsJSON, &providersJSON, &k.DefaultProvider, &k.QPM, &k.TPM, &metaJSON, &k.CreatedAt, &k.UpdatedAt)
	if err != nil {
		return nil, err
	}
	k.Enabled = enabled == 1
	json.Unmarshal([]byte(modelsJSON), &k.AllowedModels)
	json.Unmarshal([]byte(providersJSON), &k.AllowedProviders)
	json.Unmarshal([]byte(metaJSON), &k.Metadata)
	if k.AllowedModels == nil {
		k.AllowedModels = []string{}
	}
	if k.AllowedProviders == nil {
		k.AllowedProviders = []string{}
	}
	if k.Metadata == nil {
		k.Metadata = make(map[string]string)
	}
	return &k, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// ============================================================
// Key CRUD
// ============================================================

// CreateKey creates a new API key and returns it.
func (s *Store) CreateKey(name string, opts ...KeyOption) (*APIKey, error) {
	k := &APIKey{
		ID:               generateID(),
		Key:              generateAPIKey(),
		Name:             name,
		Enabled:          true,
		AllowedModels:    []string{},
		AllowedProviders: []string{},
		Metadata:         make(map[string]string),
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	}
	for _, opt := range opts {
		opt(k)
	}

	modelsJSON, _ := json.Marshal(k.AllowedModels)
	providersJSON, _ := json.Marshal(k.AllowedProviders)
	metaJSON, _ := json.Marshal(k.Metadata)

	_, err := s.db.Exec(
		`INSERT INTO api_keys (id, key, name, enabled, allowed_models, allowed_providers, default_provider, qpm, tpm, metadata, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		k.ID, k.Key, k.Name, boolToInt(k.Enabled),
		string(modelsJSON), string(providersJSON), k.DefaultProvider,
		k.QPM, k.TPM, string(metaJSON),
		k.CreatedAt, k.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create key: %w", err)
	}

	s.mu.Lock()
	s.cache[k.Key] = k
	s.mu.Unlock()
	return k, nil
}

// GetKeyByToken looks up an API key by its token string (for auth).
func (s *Store) GetKeyByToken(token string) (*APIKey, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	k, ok := s.cache[token]
	return k, ok
}

// GetKeyByID looks up an API key by its ID.
func (s *Store) GetKeyByID(id string) (*APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, k := range s.cache {
		if k.ID == id {
			return k, nil
		}
	}
	return nil, fmt.Errorf("key not found: %s", id)
}

// ListKeys returns all API keys.
func (s *Store) ListKeys() []*APIKey {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]*APIKey, 0, len(s.cache))
	for _, k := range s.cache {
		keys = append(keys, k)
	}
	return keys
}

// UpdateKey updates an existing API key.
func (s *Store) UpdateKey(id string, opts ...KeyOption) (*APIKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Find in cache
	var target *APIKey
	for _, k := range s.cache {
		if k.ID == id {
			target = k
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("key not found: %s", id)
	}

	oldToken := target.Key
	for _, opt := range opts {
		opt(target)
	}
	target.UpdatedAt = time.Now().UTC()

	modelsJSON, _ := json.Marshal(target.AllowedModels)
	providersJSON, _ := json.Marshal(target.AllowedProviders)
	metaJSON, _ := json.Marshal(target.Metadata)

	_, err := s.db.Exec(
		`UPDATE api_keys SET name=?, enabled=?, allowed_models=?, allowed_providers=?, default_provider=?, qpm=?, tpm=?, metadata=?, updated_at=? WHERE id=?`,
		target.Name, boolToInt(target.Enabled),
		string(modelsJSON), string(providersJSON), target.DefaultProvider,
		target.QPM, target.TPM, string(metaJSON),
		target.UpdatedAt, target.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("update key: %w", err)
	}

	// Update cache (key token doesn't change, but re-index just in case)
	if oldToken != target.Key {
		delete(s.cache, oldToken)
	}
	s.cache[target.Key] = target
	return target, nil
}

// DeleteKey removes an API key.
func (s *Store) DeleteKey(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var token string
	for _, k := range s.cache {
		if k.ID == id {
			token = k.Key
			break
		}
	}
	if token == "" {
		return fmt.Errorf("key not found: %s", id)
	}

	_, err := s.db.Exec("DELETE FROM api_keys WHERE id=?", id)
	if err != nil {
		return fmt.Errorf("delete key: %w", err)
	}
	delete(s.cache, token)
	return nil
}

// ============================================================
// Usage Recording + Query
// ============================================================

// RecordUsage inserts a usage record.
func (s *Store) RecordUsage(r *UsageRecord) error {
	_, err := s.db.Exec(
		`INSERT INTO usage_records (key_id, model, provider, input_tokens, output_tokens, total_tokens, duration_ms, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.KeyID, r.Model, r.Provider, r.InputTokens, r.OutputTokens, r.TotalTokens, r.Duration, r.CreatedAt,
	)
	return err
}

// QueryUsage returns aggregated usage summaries.
func (s *Store) QueryUsage(q UsageQuery) ([]UsageSummary, error) {
	where := []string{"1=1"}
	args := []any{}

	if q.KeyID != "" {
		where = append(where, "u.key_id = ?")
		args = append(args, q.KeyID)
	}
	if !q.From.IsZero() {
		where = append(where, "u.created_at >= ?")
		args = append(args, q.From)
	}
	if !q.To.IsZero() {
		where = append(where, "u.created_at <= ?")
		args = append(args, q.To)
	}
	if q.Model != "" {
		where = append(where, "u.model = ?")
		args = append(args, q.Model)
	}

	query := fmt.Sprintf(`
		SELECT u.key_id, COALESCE(k.name, ''), COUNT(*) as total_requests,
		       SUM(u.input_tokens), SUM(u.output_tokens), SUM(u.total_tokens)
		FROM usage_records u
		LEFT JOIN api_keys k ON u.key_id = k.id
		WHERE %s
		GROUP BY u.key_id
		ORDER BY total_requests DESC
	`, strings.Join(where, " AND "))

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []UsageSummary
	for rows.Next() {
		var us UsageSummary
		if err := rows.Scan(&us.KeyID, &us.KeyName, &us.TotalRequests, &us.InputTokens, &us.OutputTokens, &us.TotalTokens); err != nil {
			return nil, err
		}
		results = append(results, us)
	}
	return results, rows.Err()
}

// CountRecentRequests counts requests in the last minute for a key (for QPM check).
func (s *Store) CountRecentRequests(keyID string, window time.Duration) (int, error) {
	since := time.Now().UTC().Add(-window)
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM usage_records WHERE key_id=? AND created_at>=?", keyID, since).Scan(&count)
	return count, err
}

// CountRecentTokens sums tokens in the last minute for a key (for TPM check).
func (s *Store) CountRecentTokens(keyID string, window time.Duration) (int, error) {
	since := time.Now().UTC().Add(-window)
	var total int
	err := s.db.QueryRow("SELECT COALESCE(SUM(total_tokens), 0) FROM usage_records WHERE key_id=? AND created_at>=?", keyID, since).Scan(&total)
	return total, err
}

// ============================================================
// Key options (functional options pattern)
// ============================================================

// KeyOption is a functional option for configuring an API key.
type KeyOption func(*APIKey)

func WithModels(models []string) KeyOption {
	return func(k *APIKey) { k.AllowedModels = models }
}

func WithProviders(providers []string) KeyOption {
	return func(k *APIKey) { k.AllowedProviders = providers }
}

func WithQPM(qpm int) KeyOption {
	return func(k *APIKey) { k.QPM = qpm }
}

func WithTPM(tpm int) KeyOption {
	return func(k *APIKey) { k.TPM = tpm }
}

func WithEnabled(enabled bool) KeyOption {
	return func(k *APIKey) { k.Enabled = enabled }
}

func WithName(name string) KeyOption {
	return func(k *APIKey) { k.Name = name }
}

func WithDefaultProvider(provider string) KeyOption {
	return func(k *APIKey) { k.DefaultProvider = provider }
}

// ============================================================
// Helpers
// ============================================================

func generateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func generateAPIKey() string {
	b := make([]byte, 24)
	rand.Read(b)
	return "ag-" + hex.EncodeToString(b)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
