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

// Store manages API keys, usage records, and provider configurations.
type Store struct {
	db *sql.DB
	mu sync.RWMutex
	// In-memory key cache for fast auth lookup
	cache map[string]*APIKey // key string → APIKey
	// In-memory provider cache
	providerCache map[string]*ProviderRecord // provider ID → ProviderRecord
}

// NewStore opens (or creates) an SQLite database for tenant management.
func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open tenant db: %w", err)
	}

	s := &Store{
		db:            db,
		cache:         make(map[string]*APIKey),
		providerCache: make(map[string]*ProviderRecord),
	}

	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}

	if err := s.loadCache(); err != nil {
		_ = db.Close()
		return nil, err
	}

	if err := s.loadProviderCache(); err != nil {
		_ = db.Close()
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

	CREATE TABLE IF NOT EXISTS providers (
		id            TEXT PRIMARY KEY,
		name          TEXT UNIQUE NOT NULL,
		type          TEXT NOT NULL,
		weight        INTEGER NOT NULL DEFAULT 1,
		enabled       INTEGER NOT NULL DEFAULT 1,
		base_url      TEXT NOT NULL DEFAULT '',
		api_key       TEXT NOT NULL DEFAULT '',
		github_tokens TEXT NOT NULL DEFAULT '[]',
		models        TEXT NOT NULL DEFAULT '[]',
		default_model TEXT NOT NULL DEFAULT '',
		created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	`
	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("migrate tenant db: %w", err)
	}
	// Safe column addition for existing databases
	_, _ = s.db.Exec("ALTER TABLE api_keys ADD COLUMN default_provider TEXT NOT NULL DEFAULT ''")
	return nil
}

func (s *Store) loadCache() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query("SELECT id, key, name, enabled, allowed_models, allowed_providers, default_provider, qpm, tpm, metadata, created_at, updated_at FROM api_keys")
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

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
	_ = json.Unmarshal([]byte(modelsJSON), &k.AllowedModels)
	_ = json.Unmarshal([]byte(providersJSON), &k.AllowedProviders)
	_ = json.Unmarshal([]byte(metaJSON), &k.Metadata)
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
	defer func() { _ = rows.Close() }()

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
// Provider persistence
// ============================================================

func (s *Store) loadProviderCache() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query("SELECT id, name, type, weight, enabled, base_url, api_key, github_tokens, models, default_model, created_at, updated_at FROM providers")
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	s.providerCache = make(map[string]*ProviderRecord)
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return err
		}
		s.providerCache[p.ID] = p
	}
	return rows.Err()
}

func scanProvider(rows *sql.Rows) (*ProviderRecord, error) {
	var p ProviderRecord
	var enabled int
	var tokensJSON, modelsJSON string
	err := rows.Scan(&p.ID, &p.Name, &p.Type, &p.Weight, &enabled,
		&p.BaseURL, &p.APIKey, &tokensJSON, &modelsJSON, &p.DefaultModel,
		&p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	p.Enabled = enabled == 1
	_ = json.Unmarshal([]byte(tokensJSON), &p.GithubTokens)
	_ = json.Unmarshal([]byte(modelsJSON), &p.Models)
	if p.GithubTokens == nil {
		p.GithubTokens = []string{}
	}
	if p.Models == nil {
		p.Models = []string{}
	}
	return &p, nil
}

// CreateProvider persists a new provider configuration.
func (s *Store) CreateProvider(name, typ string, opts ...ProviderOption) (*ProviderRecord, error) {
	p := &ProviderRecord{
		ID:           generateID(),
		Name:         name,
		Type:         typ,
		Weight:       1,
		Enabled:      true,
		GithubTokens: []string{},
		Models:       []string{},
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	for _, opt := range opts {
		opt(p)
	}

	tokensJSON, _ := json.Marshal(p.GithubTokens)
	modelsJSON, _ := json.Marshal(p.Models)

	_, err := s.db.Exec(
		`INSERT INTO providers (id, name, type, weight, enabled, base_url, api_key, github_tokens, models, default_model, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Type, p.Weight, boolToInt(p.Enabled),
		p.BaseURL, p.APIKey, string(tokensJSON), string(modelsJSON), p.DefaultModel,
		p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create provider: %w", err)
	}

	s.mu.Lock()
	s.providerCache[p.ID] = p
	s.mu.Unlock()
	return p, nil
}

// GetProvider returns a provider by ID.
func (s *Store) GetProvider(id string) (*ProviderRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if p, ok := s.providerCache[id]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("provider not found: %s", id)
}

// GetProviderByName returns a provider by name.
func (s *Store) GetProviderByName(name string) (*ProviderRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.providerCache {
		if p.Name == name {
			return p, true
		}
	}
	return nil, false
}

// ListProviderRecords returns all persisted provider configurations.
func (s *Store) ListProviderRecords() []*ProviderRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*ProviderRecord, 0, len(s.providerCache))
	for _, p := range s.providerCache {
		result = append(result, p)
	}
	return result
}

// UpdateProvider updates an existing provider configuration.
func (s *Store) UpdateProvider(id string, opts ...ProviderOption) (*ProviderRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	target, ok := s.providerCache[id]
	if !ok {
		return nil, fmt.Errorf("provider not found: %s", id)
	}

	for _, opt := range opts {
		opt(target)
	}
	target.UpdatedAt = time.Now().UTC()

	tokensJSON, _ := json.Marshal(target.GithubTokens)
	modelsJSON, _ := json.Marshal(target.Models)

	_, err := s.db.Exec(
		`UPDATE providers SET name=?, type=?, weight=?, enabled=?, base_url=?, api_key=?, github_tokens=?, models=?, default_model=?, updated_at=? WHERE id=?`,
		target.Name, target.Type, target.Weight, boolToInt(target.Enabled),
		target.BaseURL, target.APIKey, string(tokensJSON), string(modelsJSON), target.DefaultModel,
		target.UpdatedAt, target.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("update provider: %w", err)
	}

	s.providerCache[id] = target
	return target, nil
}

// DeleteProvider removes a provider configuration.
func (s *Store) DeleteProvider(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.providerCache[id]; !ok {
		return fmt.Errorf("provider not found: %s", id)
	}

	_, err := s.db.Exec("DELETE FROM providers WHERE id=?", id)
	if err != nil {
		return fmt.Errorf("delete provider: %w", err)
	}
	delete(s.providerCache, id)
	return nil
}

// ============================================================
// Helpers
// ============================================================

func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func generateAPIKey() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return "ag-" + hex.EncodeToString(b)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
