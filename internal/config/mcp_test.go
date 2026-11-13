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
		"[mcp]\nport = 9090\n":                                       "127.0.0.1:9090",
		"[mcp]\nport = \"9091\"\n":                                   "127.0.0.1:9091",
		"[mcp]\nhost = \"::1\"\nport = 9092\n":                       "[::1]:9092",
		"[mcp]\nhost = \" 127.0.0.2 \"\n":                            "127.0.0.2:8080",
		"[provider.jev]\nmodel = 'unrelated'\ntimeout = 'invalid'\n": "127.0.0.1:8080",
	} {
		path := writeConfig(t, filepath.Join(dir, "config.toml"), content)
		if got, err := MCPAddress(path); err != nil || got != want {
			t.Fatal("wrong address", content, got, err)
		}
	}
	if err := os.Symlink(writeConfig(t, filepath.Join(dir, "target.toml"), "[mcp]\nport = 9090\n"), LocalName); err == nil {
		if _, err := MCPAddress(""); err == nil {
			t.Fatal("linked local configuration accepted")
		}
	}
	path := writeConfig(t, filepath.Join(dir, "config.toml"), "[mcp]\nport = 9090\n")
	t.Setenv("TS_JEV_MCP_PORT", "9093")
	if got, err := MCPAddress(path); err != nil || got != "127.0.0.1:9093" {
		t.Fatal("environment did not override TOML", got, err)
	}
}

func TestMCPAddressRejectsInvalidValues(t *testing.T) {
	mcpEnv(t)
	dir := t.TempDir()
	for _, content := range []string{
		"[mcp]\nhost = 'http://127.0.0.1'\n", "[mcp]\nhost = 'localhost'\n", "[mcp]\nhost = '0.0.0.0'\n",
		"[mcp]\nhost = '127.0.0.1:9090'\n", "[mcp]\nhost = ''\n", "[mcp]\nhost = 1\n",
		"[mcp]\nport = 0\n", "[mcp]\nport = 65536\n", "[mcp]\nport = -1\n", "[mcp]\nport = 'http'\n", "[mcp]\nport = true\n", "[mcp]\nport = [",
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
	if err != nil || !strings.Contains(text, "[mcp]\nhost = \"127.0.0.1\"\nport = 8080\n") {
		t.Fatal("view or template lacks MCP settings", err, text)
	}
	if got, err := MCPAddress(path); err != nil || got != "127.0.0.1:8080" {
		t.Fatal("template MCP settings invalid", got, err)
	}
	writeConfig(t, path, "[mcp]\nport = 0\n")
	if _, err := View(path); err == nil {
		t.Fatal("view accepted invalid MCP port")
	}
}
