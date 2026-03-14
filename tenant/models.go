package tenant

import (
	"time"
)

// APIKey represents a tenant's API key with permissions and limits.
type APIKey struct {
	ID        string    `json:"id"`
	Key       string    `json:"key"`
	Name      string    `json:"name"` // Human-readable label
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Permissions
	AllowedModels    []string `json:"allowed_models"`    // Empty = all models allowed
	AllowedProviders []string `json:"allowed_providers"` // Empty = all providers allowed

	// Routing
	DefaultProvider string `json:"default_provider"` // Preferred provider for this key (empty = use global routing)

	// Rate limits (0 = unlimited)
	QPM int `json:"qpm"` // Queries per minute
	TPM int `json:"tpm"` // Tokens per minute

	// Metadata
	Metadata map[string]string `json:"metadata,omitempty"`
}

// UsageRecord tracks token consumption per request.
type UsageRecord struct {
	ID           int64     `json:"id"`
	KeyID        string    `json:"key_id"`
	Model        string    `json:"model"`
	Provider     string    `json:"provider"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	TotalTokens  int       `json:"total_tokens"`
	Duration     float64   `json:"duration_ms"` // Request duration in ms
	CreatedAt    time.Time `json:"created_at"`
}

// UsageSummary aggregates usage over a time period.
type UsageSummary struct {
	KeyID         string `json:"key_id"`
	KeyName       string `json:"key_name"`
	Model         string `json:"model,omitempty"`
	Provider      string `json:"provider,omitempty"`
	TotalRequests int64  `json:"total_requests"`
	InputTokens   int64  `json:"input_tokens"`
	OutputTokens  int64  `json:"output_tokens"`
	TotalTokens   int64  `json:"total_tokens"`
}

// UsageQuery defines parameters for querying usage data.
type UsageQuery struct {
	KeyID    string    // Filter by specific key
	From     time.Time // Start time
	To       time.Time // End time
	Model    string    // Filter by model
	Provider string    // Filter by provider
	GroupBy  string    // Group by: "key" (default), "model", "provider", "key_model"
}

// ProviderRecord represents a persisted provider configuration.
type ProviderRecord struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Type         string    `json:"type"` // "kiro", "openai", "openai-compat", "copilot", "anthropic"
	Weight       int       `json:"weight"`
	Enabled      bool      `json:"enabled"`
	BaseURL      string    `json:"base_url,omitempty"`
	APIKey       string    `json:"-"` // never expose in JSON
	GithubToken  string    `json:"-"` // single GitHub token for copilot providers
	Models       []string  `json:"models,omitempty"`
	DefaultModel string    `json:"default_model,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// ProviderOption is a functional option for configuring a ProviderRecord.
type ProviderOption func(*ProviderRecord)

func WithProviderType(t string) ProviderOption   { return func(p *ProviderRecord) { p.Type = t } }
func WithProviderWeight(w int) ProviderOption    { return func(p *ProviderRecord) { p.Weight = w } }
func WithProviderEnabled(e bool) ProviderOption  { return func(p *ProviderRecord) { p.Enabled = e } }
func WithProviderBaseURL(u string) ProviderOption { return func(p *ProviderRecord) { p.BaseURL = u } }
func WithProviderAPIKey(k string) ProviderOption  { return func(p *ProviderRecord) { p.APIKey = k } }
func WithProviderGithubToken(t string) ProviderOption {
	return func(p *ProviderRecord) { p.GithubToken = t }
}
func WithProviderModels(m []string) ProviderOption { return func(p *ProviderRecord) { p.Models = m } }
func WithProviderDefaultModel(m string) ProviderOption {
	return func(p *ProviderRecord) { p.DefaultModel = m }
}
func WithProviderName(n string) ProviderOption { return func(p *ProviderRecord) { p.Name = n } }
