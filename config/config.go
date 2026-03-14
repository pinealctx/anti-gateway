package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Version is injected at build time via:
// -ldflags "-X github.com/pinealctx/anti-gateway/config.Version=vX.Y.Z"
// Default remains "dev" for local builds.
var Version = "dev"

// Config holds the resolved application configuration.
type Config struct {
	// Server
	Host     string
	Port     int
	LogLevel string

	// Auth
	APIKey string

	// Provider defaults
	DefaultModel string
}

// BindFlags registers persistent flags on the given cobra.Command.
// Call this once during command initialization (e.g. in init()).
func BindFlags(cmd *cobra.Command) {
	f := cmd.PersistentFlags()

	// Server
	f.StringP("host", "H", "0.0.0.0", "Listen address (env: HOST)")
	f.IntP("port", "p", 8080, "Listen port (env: PORT)")
	f.StringP("log-level", "l", "info", "Log level: debug|info|warn|error (env: LOG_LEVEL)")

	// Auth
	f.StringP("api-key", "k", "", "API key for authentication (env: API_KEY)")
	f.String("admin-key", "", "Admin key for /admin/* endpoints (env: ADMIN_KEY)")

	// Defaults
	f.StringP("model", "m", "claude-opus-4.6", "Default upstream model (env: DEFAULT_MODEL)")
	f.String("lb-strategy", "smart", "Load balance strategy: weighted|round-robin|least-used|priority|smart (env: LB_STRATEGY)")
	f.String("default-provider", "", "Default provider name (env: DEFAULT_PROVIDER)")

	// Health check
	f.Bool("no-health-check", false, "Disable provider health checks (env: NO_HEALTH_CHECK)")
	f.Int("health-check-interval", 60, "Health check interval in seconds (env: HEALTH_CHECK_INTERVAL)")

	// Tenant
	f.Bool("tenant", false, "Enable multi-tenant mode (env: TENANT_ENABLED)")
	f.String("db-path", "antigateway.db", "SQLite database path (env: DB_PATH)")

	// Config file
	f.StringP("config", "c", "", "Path to config file (YAML/TOML/JSON)")
}

// FromCommand reads flag values from the executed cobra.Command,
// applies env-var fallback ( flag > env > default ), and returns the Config.
func FromCommand(cmd *cobra.Command) *Config {
	cfg := &Config{
		Host:         resolveStr(cmd, "host", "HOST"),
		Port:         resolveInt(cmd, "port", "PORT"),
		LogLevel:     strings.ToLower(resolveStr(cmd, "log-level", "LOG_LEVEL")),
		APIKey:       resolveStr(cmd, "api-key", "API_KEY"),
		DefaultModel: resolveStr(cmd, "model", "DEFAULT_MODEL"),
	}
	return cfg
}

// LoadGatewayConfig loads the full gateway configuration.
// Priority: config file → CLI flags → env → defaults.
// If no config file is specified, returns a GatewayConfig synthesized from flags
// with a single Kiro provider (backward-compatible mode).
func LoadGatewayConfig(cmd *cobra.Command) (*GatewayConfig, error) {
	configFile := resolveStr(cmd, "config", "ANTIGATEWAY_CONFIG")

	if configFile != "" {
		return loadFromFile(configFile, cmd)
	}
	// No config file: synthesize from flags (backward compatibility)
	return synthesizeFromFlags(cmd), nil
}

func loadFromFile(path string, cmd *cobra.Command) (*GatewayConfig, error) {
	v := viper.New()
	v.SetConfigFile(path)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var gw GatewayConfig
	if err := v.Unmarshal(&gw); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// CLI flags override file values when explicitly set
	if cmd.Flags().Changed("host") {
		gw.Server.Host, _ = cmd.Flags().GetString("host")
	}
	if cmd.Flags().Changed("port") {
		gw.Server.Port, _ = cmd.Flags().GetInt("port")
	}
	if cmd.Flags().Changed("log-level") {
		gw.Server.LogLevel, _ = cmd.Flags().GetString("log-level")
	}
	if cmd.Flags().Changed("api-key") {
		gw.Auth.APIKey, _ = cmd.Flags().GetString("api-key")
	}
	if cmd.Flags().Changed("admin-key") {
		gw.Auth.AdminKey, _ = cmd.Flags().GetString("admin-key")
	}
	if cmd.Flags().Changed("model") {
		gw.Defaults.Model, _ = cmd.Flags().GetString("model")
	}
	if cmd.Flags().Changed("lb-strategy") {
		gw.Defaults.LBStrategy, _ = cmd.Flags().GetString("lb-strategy")
	}
	if cmd.Flags().Changed("default-provider") {
		gw.Defaults.Provider, _ = cmd.Flags().GetString("default-provider")
	}
	if cmd.Flags().Changed("no-health-check") {
		noHealthCheck, _ := cmd.Flags().GetBool("no-health-check")
		gw.Defaults.HealthCheckEnabled = !noHealthCheck
	}
	if cmd.Flags().Changed("health-check-interval") {
		gw.Defaults.HealthCheckSeconds, _ = cmd.Flags().GetInt("health-check-interval")
	}
	if cmd.Flags().Changed("tenant") {
		gw.Tenant.Enabled, _ = cmd.Flags().GetBool("tenant")
	}
	if cmd.Flags().Changed("db-path") {
		gw.Tenant.DBPath, _ = cmd.Flags().GetString("db-path")
	}

	// Apply defaults for missing fields
	if gw.Server.Host == "" {
		gw.Server.Host = "0.0.0.0"
	}
	if gw.Server.Port == 0 {
		gw.Server.Port = 8080
	}
	if gw.Server.LogLevel == "" {
		gw.Server.LogLevel = "info"
	}
	if gw.Defaults.Model == "" {
		gw.Defaults.Model = "claude-opus-4.6"
	}
	if gw.Defaults.LBStrategy == "" {
		gw.Defaults.LBStrategy = "smart"
	}
	if gw.Defaults.HealthCheckSeconds == 0 {
		gw.Defaults.HealthCheckSeconds = 60
	}
	if gw.Tenant.DBPath == "" {
		gw.Tenant.DBPath = "antigateway.db"
	}

	return &gw, nil
}

// synthesizeFromFlags builds a GatewayConfig from CLI flags only (no config file).
func synthesizeFromFlags(cmd *cobra.Command) *GatewayConfig {
	// Server
	host := resolveStr(cmd, "host", "HOST")
	port := resolveInt(cmd, "port", "PORT")
	logLevel := strings.ToLower(resolveStr(cmd, "log-level", "LOG_LEVEL"))

	// Auth
	apiKey := resolveStr(cmd, "api-key", "API_KEY")
	adminKey := resolveStr(cmd, "admin-key", "ADMIN_KEY")

	// Defaults
	model := resolveStr(cmd, "model", "DEFAULT_MODEL")
	lbStrategy := resolveStr(cmd, "lb-strategy", "LB_STRATEGY")
	defaultProvider := resolveStr(cmd, "default-provider", "DEFAULT_PROVIDER")

	// Health check
	noHealthCheck := resolveBool(cmd, "no-health-check", "NO_HEALTH_CHECK")
	healthCheckInterval := resolveInt(cmd, "health-check-interval", "HEALTH_CHECK_INTERVAL")

	// Tenant
	tenantEnabled := resolveBool(cmd, "tenant", "TENANT_ENABLED")
	dbPath := resolveStr(cmd, "db-path", "DB_PATH")

	// Apply defaults
	if host == "" {
		host = "0.0.0.0"
	}
	if port == 0 {
		port = 8080
	}
	if logLevel == "" {
		logLevel = "info"
	}
	if model == "" {
		model = "claude-opus-4.6"
	}
	if lbStrategy == "" {
		lbStrategy = "smart"
	}
	if healthCheckInterval == 0 {
		healthCheckInterval = 60
	}
	if dbPath == "" {
		dbPath = "antigateway.db"
	}

	return &GatewayConfig{
		Server: ServerConfig{
			Host:     host,
			Port:     port,
			LogLevel: logLevel,
		},
		Auth: AuthConfig{
			APIKey:   apiKey,
			AdminKey: adminKey,
		},
		Defaults: DefaultsConfig{
			Model:              model,
			LBStrategy:         lbStrategy,
			Provider:           defaultProvider,
			HealthCheckEnabled: !noHealthCheck,
			HealthCheckSeconds: healthCheckInterval,
		},
		Tenant: TenantConfig{
			Enabled: tenantEnabled,
			DBPath:  dbPath,
		},
	}
}

// resolveStr: flag (if explicitly set) > env > flag default.
func resolveStr(cmd *cobra.Command, flag, envKey string) string {
	if cmd.Flags().Changed(flag) {
		v, _ := cmd.Flags().GetString(flag)
		return v
	}
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	v, _ := cmd.Flags().GetString(flag)
	return v
}

func resolveInt(cmd *cobra.Command, flag, envKey string) int {
	if cmd.Flags().Changed(flag) {
		v, _ := cmd.Flags().GetInt(flag)
		return v
	}
	if v := os.Getenv(envKey); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n
		}
	}
	v, _ := cmd.Flags().GetInt(flag)
	return v
}

func resolveBool(cmd *cobra.Command, flag, envKey string) bool {
	if cmd.Flags().Changed(flag) {
		v, _ := cmd.Flags().GetBool(flag)
		return v
	}
	if v := os.Getenv(envKey); v != "" {
		return strings.ToLower(v) == "true" || v == "1"
	}
	v, _ := cmd.Flags().GetBool(flag)
	return v
}
