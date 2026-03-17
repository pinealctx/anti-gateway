package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

// ============================================================
// LoadGatewayConfig from YAML file
// ============================================================

func TestLoadFromFile_FullConfig(t *testing.T) {
	yaml := `
server:
  host: "127.0.0.1"
  port: 9090
  log_level: "debug"

auth:
  api_key: "sk-test-key"

defaults:
  provider: "openai"
  model: "gpt-4o"

tenant:
  enabled: true
  db_path: "test.db"
`
	path := writeTemp(t, "config.yaml", yaml)

	// Use a minimal cobra command to test
	cmd := newTestCmd()
	cmd.SetArgs([]string{"--config", path})
	cmd.Execute()

	gw, err := LoadGatewayConfig(cmd)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if gw.Server.Host != "127.0.0.1" {
		t.Errorf("host = %q", gw.Server.Host)
	}
	if gw.Server.Port != 9090 {
		t.Errorf("port = %d", gw.Server.Port)
	}
	if gw.Auth.APIKey != "sk-test-key" {
		t.Error("api key mismatch")
	}
	if gw.Defaults.Provider != "openai" {
		t.Errorf("default provider = %q", gw.Defaults.Provider)
	}
	if !gw.Tenant.Enabled {
		t.Error("tenant should be enabled")
	}
	if gw.Tenant.DBPath != "test.db" {
		t.Errorf("db_path = %q", gw.Tenant.DBPath)
	}
}

func TestLoadFromFile_Defaults(t *testing.T) {
	yaml := `
defaults:
  model: "test-model"
`
	path := writeTemp(t, "minimal.yaml", yaml)
	cmd := newTestCmd()
	cmd.SetArgs([]string{"--config", path})
	cmd.Execute()

	gw, err := LoadGatewayConfig(cmd)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// Defaults should be applied
	if gw.Server.Host != "0.0.0.0" {
		t.Errorf("default host = %q", gw.Server.Host)
	}
	if gw.Server.Port != 8080 {
		t.Errorf("default port = %d", gw.Server.Port)
	}
	if gw.Server.LogLevel != "info" {
		t.Errorf("default log_level = %q", gw.Server.LogLevel)
	}
	if gw.Defaults.Model != "test-model" {
		t.Errorf("model = %q", gw.Defaults.Model)
	}
}

func TestLoadFromFile_CLIOverridesFile(t *testing.T) {
	yaml := `
server:
  host: "0.0.0.0"
  port: 9090
`
	path := writeTemp(t, "override.yaml", yaml)
	cmd := newTestCmd()
	cmd.SetArgs([]string{"--config", path, "--port", "3333", "--host", "192.168.1.1"})
	cmd.Execute()

	gw, err := LoadGatewayConfig(cmd)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// CLI flags should override file values
	if gw.Server.Port != 3333 {
		t.Errorf("port = %d, want 3333 (CLI override)", gw.Server.Port)
	}
	if gw.Server.Host != "192.168.1.1" {
		t.Errorf("host = %q, want 192.168.1.1 (CLI override)", gw.Server.Host)
	}
}

func TestLoadFromFile_LogLevelNormalized(t *testing.T) {
	yaml := `
server:
  log_level: "DEBUG"
`
	path := writeTemp(t, "loglevel.yaml", yaml)
	cmd := newTestCmd()
	cmd.SetArgs([]string{"--config", path})
	cmd.Execute()

	gw, err := LoadGatewayConfig(cmd)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if gw.Server.LogLevel != "debug" {
		t.Errorf("log_level = %q, want debug", gw.Server.LogLevel)
	}
}

// ============================================================
// No config file → synthesize from flags
// ============================================================

func TestSynthesizeFromFlags(t *testing.T) {
	cmd := newTestCmd()
	cmd.SetArgs([]string{"--port", "7070", "--api-key", "my-key"})
	cmd.Execute()

	gw, err := LoadGatewayConfig(cmd)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if gw.Server.Port != 7070 {
		t.Errorf("port = %d", gw.Server.Port)
	}
	if gw.Auth.APIKey != "my-key" {
		t.Error("api key mismatch")
	}
	if gw.Defaults.Provider != "" {
		t.Errorf("should default to empty provider, got %q", gw.Defaults.Provider)
	}
	if gw.Tenant.DBPath != DefaultDBPath() {
		t.Errorf("db_path = %q, want %q", gw.Tenant.DBPath, DefaultDBPath())
	}
}

// ============================================================
// Invalid config
// ============================================================

func TestLoadFromFile_BadPath(t *testing.T) {
	cmd := newTestCmd()
	cmd.SetArgs([]string{"--config", "/nonexistent/config.yaml"})
	cmd.Execute()

	_, err := LoadGatewayConfig(cmd)
	if err == nil {
		t.Error("should error on missing config file")
	}
}

// ============================================================
// Helpers
// ============================================================

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	return path
}

// newTestCmd creates a minimal cobra command with all flags bound.
func newTestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:  "test",
		RunE: func(cmd *cobra.Command, args []string) error { return nil },
	}
	BindFlags(cmd)
	return cmd
}
