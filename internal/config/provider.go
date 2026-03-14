package config

// ProviderConfig defines a single upstream provider.
type ProviderConfig struct {
	Name    string `mapstructure:"name"`
	Type    string `mapstructure:"type"`    // "kiro", "openai", "openai-compat", "copilot", "anthropic"
	Weight  int    `mapstructure:"weight"`  // Load-balance weight (default 1)
	Enabled bool   `mapstructure:"enabled"` // Whether the provider is active

	// OpenAI / OpenAI-compatible settings
	BaseURL string `mapstructure:"base_url"` // e.g. "https://api.openai.com/v1"
	APIKey  string `mapstructure:"api_key"`

	// Copilot-specific
	GithubToken string `mapstructure:"github_token"` // Single GitHub OAuth token for Copilot

	// Model routing: which models this provider handles
	// Empty = handles all models (fallback)
	Models []string `mapstructure:"models"`

	// Default model to use when client sends unknown model
	DefaultModel string `mapstructure:"default_model"`
}

// GatewayConfig is the top-level YAML configuration.
// Providers are managed at runtime via the Admin API and persisted in SQLite.
type GatewayConfig struct {
	Server   ServerConfig   `mapstructure:"server"`
	Auth     AuthConfig     `mapstructure:"auth"`
	Defaults DefaultsConfig `mapstructure:"defaults"`
	Tenant   TenantConfig   `mapstructure:"tenant"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Host        string   `mapstructure:"host"`
	Port        int      `mapstructure:"port"`
	LogLevel    string   `mapstructure:"log_level"`
	CORSOrigins []string `mapstructure:"cors_origins"` // Allowed CORS origins (empty = allow all)
}

// AuthConfig holds auth settings.
type AuthConfig struct {
	APIKey   string `mapstructure:"api_key"`
	AdminKey string `mapstructure:"admin_key"` // Separate key for /admin/* endpoints
}

// DefaultsConfig holds fallback settings.
type DefaultsConfig struct {
	Provider           string `mapstructure:"provider"`             // default provider name
	Model              string `mapstructure:"model"`                // default model
	LBStrategy         string `mapstructure:"lb_strategy"`          // load balancing strategy: weighted, round-robin, least-used, priority, smart
	HealthCheckEnabled bool   `mapstructure:"health_check_enabled"` // whether to run periodic health checks
	HealthCheckSeconds int    `mapstructure:"health_check_seconds"` // health check interval in seconds (default 60)
}

// TenantConfig holds multi-tenant settings.
type TenantConfig struct {
	Enabled bool   `mapstructure:"enabled"` // Enable multi-tenant mode
	DBPath  string `mapstructure:"db_path"` // SQLite database path for tenant data
}
