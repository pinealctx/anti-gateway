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
	TotalRequests int64  `json:"total_requests"`
	InputTokens   int64  `json:"input_tokens"`
	OutputTokens  int64  `json:"output_tokens"`
	TotalTokens   int64  `json:"total_tokens"`
}

// UsageQuery defines parameters for querying usage data.
type UsageQuery struct {
	KeyID string    // Filter by specific key
	From  time.Time // Start time
	To    time.Time // End time
	Model string    // Filter by model
}
