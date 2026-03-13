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

providers:
  - name: openai
    type: openai
    base_url: "https://api.openai.com/v1"
    api_key: "sk-xxx"
    weight: 3
    models:
      - gpt-4
      - gpt-4o

  - name: kiro
    type: kiro
    weight: 1
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
	if len(gw.Providers) != 2 {
		t.Fatalf("providers count = %d", len(gw.Providers))
	}

	openai := gw.Providers[0]
	if openai.Name != "openai" || openai.Type != "openai" || openai.Weight != 3 {
		t.Errorf("openai provider: %+v", openai)
	}
	if len(openai.Models) != 2 {
		t.Errorf("openai models = %v", openai.Models)
	}

	kiro := gw.Providers[1]
	if kiro.Name != "kiro" {
		t.Errorf("kiro provider: %+v", kiro)
	}
}

func TestLoadFromFile_Defaults(t *testing.T) {
	yaml := `
providers:
  - name: test
    type: openai-compat
    base_url: "http://localhost:8000/v1"
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
	if gw.Defaults.Model != "claude-opus-4.6" {
		t.Errorf("default model = %q", gw.Defaults.Model)
	}
	// Weight default
	if gw.Providers[0].Weight != 1 {
		t.Errorf("default weight = %d", gw.Providers[0].Weight)
	}
}

func TestLoadFromFile_CLIOverridesFile(t *testing.T) {
	yaml := `
server:
  host: "0.0.0.0"
  port: 9090

providers:
  - name: test
    type: openai-compat
    base_url: "http://localhost:8000/v1"
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
	if gw.Defaults.Provider != "kiro" {
		t.Errorf("should default to kiro, got %q", gw.Defaults.Provider)
	}
	if len(gw.Providers) != 1 || gw.Providers[0].Type != "kiro" {
		t.Error("should synthesize a single kiro provider")
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
// FromCommand
// ============================================================

func TestFromCommand(t *testing.T) {
	cmd := newTestCmd()
	cmd.SetArgs([]string{"--host", "10.0.0.1", "--port", "5555", "--log-level", "DEBUG", "--model", "gpt-4"})
	cmd.Execute()

	cfg := FromCommand(cmd)
	if cfg.Host != "10.0.0.1" {
		t.Errorf("host = %q", cfg.Host)
	}
	if cfg.Port != 5555 {
		t.Errorf("port = %d", cfg.Port)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("log_level = %q (should lowercase)", cfg.LogLevel)
	}
	if cfg.DefaultModel != "gpt-4" {
		t.Errorf("model = %q", cfg.DefaultModel)
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
