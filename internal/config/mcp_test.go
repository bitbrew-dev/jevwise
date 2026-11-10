package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mcpEnv(t *testing.T) {
	t.Helper()
	cleanEnv(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TS_JEV_MCP_BASE_URL", "")
	t.Setenv("TS_JEV_MCP_PORT", "")
}

func TestMCPAddressPrecedence(t *testing.T) {
	mcpEnv(t)
	dir := t.TempDir()
	if got, err := MCPAddress(filepath.Join(dir, "missing.toml")); err != nil || got != "127.0.0.1:8080" {
		t.Fatal("missing file did not fall back to defaults", got, err)
	}
	for content, want := range map[string]string{
		"mcp_port = 9090\n":                          "127.0.0.1:9090",
		"mcp_port = \"9091\"\n":                      "127.0.0.1:9091",
		"mcp_base_url = \"::1\"\nmcp_port = 9092\n":  "[::1]:9092",
		"mcp_base_url = \" 127.0.0.2 \"\n":           "127.0.0.2:8080",
		"model = 'unrelated'\ntimeout = 'invalid'\n": "127.0.0.1:8080",
	} {
		path := writeConfig(t, filepath.Join(dir, "config.toml"), content)
		if got, err := MCPAddress(path); err != nil || got != want {
			t.Fatal("wrong address", content, got, err)
		}
	}
	if err := os.Symlink(writeConfig(t, filepath.Join(dir, "target.toml"), "mcp_port = 9090\n"), LocalName); err == nil {
		if _, err := MCPAddress(""); err == nil {
			t.Fatal("linked local configuration accepted")
		}
	}
	path := writeConfig(t, filepath.Join(dir, "config.toml"), "mcp_port = 9090\n")
	t.Setenv("TS_JEV_MCP_PORT", "9093")
	if got, err := MCPAddress(path); err != nil || got != "127.0.0.1:9093" {
		t.Fatal("environment did not override TOML", got, err)
	}
}

func TestMCPAddressRejectsInvalidValues(t *testing.T) {
	mcpEnv(t)
	dir := t.TempDir()
	for _, content := range []string{
		"mcp_base_url = 'http://127.0.0.1'\n", "mcp_base_url = 'localhost'\n", "mcp_base_url = '0.0.0.0'\n",
		"mcp_base_url = '127.0.0.1:9090'\n", "mcp_base_url = ''\n", "mcp_base_url = 1\n",
		"mcp_port = 0\n", "mcp_port = 65536\n", "mcp_port = -1\n", "mcp_port = 'http'\n", "mcp_port = true\n", "mcp_port = [",
	} {
		path := writeConfig(t, filepath.Join(dir, "config.toml"), content)
		if got, err := MCPAddress(path); err == nil || got != "" {
			t.Fatal("invalid MCP setting accepted", content, got)
		}
	}
}

func TestViewAndTemplateIncludeMCPSettings(t *testing.T) {
	mcpEnv(t)
	path := writeConfig(t, filepath.Join(t.TempDir(), "config.toml"), string(template))
	text, err := View(path)
	if err != nil || !strings.Contains(text, "mcp_base_url = \"127.0.0.1\"\n") || !strings.Contains(text, "mcp_port = 8080\n") {
		t.Fatal("view or template lacks MCP settings", err, text)
	}
	if got, err := MCPAddress(path); err != nil || got != "127.0.0.1:8080" {
		t.Fatal("template MCP settings invalid", got, err)
	}
	writeConfig(t, path, "mcp_port = 0\n")
	if _, err := View(path); err == nil {
		t.Fatal("view accepted invalid MCP port")
	}
}
