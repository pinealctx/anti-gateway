package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const Version = "0.2.0"

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
	f.StringP("host", "H", "0.0.0.0", "Listen address (env: HOST)")
	f.IntP("port", "p", 8080, "Listen port (env: PORT)")
	f.StringP("log-level", "l", "info", "Log level: debug|info|warn|error (env: LOG_LEVEL)")
	f.StringP("api-key", "k", "", "API key for authentication (env: API_KEY)")
	f.StringP("model", "m", "claude-opus-4.6", "Default upstream model (env: DEFAULT_MODEL)")
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

	return &gw, nil
}

// synthesizeFromFlags builds a GatewayConfig from CLI flags only (no config file).
func synthesizeFromFlags(cmd *cobra.Command) *GatewayConfig {
	legacy := FromCommand(cmd)
	return &GatewayConfig{
		Server: ServerConfig{
			Host:     legacy.Host,
			Port:     legacy.Port,
			LogLevel: legacy.LogLevel,
		},
		Auth: AuthConfig{
			APIKey: legacy.APIKey,
		},
		Defaults: DefaultsConfig{
			Model: legacy.DefaultModel,
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
