package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const flatFixture = "# comment\nAPI_KEY = \"$JEV_TOKEN\"\nbase_url = \"https://flat.test\"\nmodel = \"flat-model\"\nprovider = \"codex\"\ntimeout = \"garbage\"\neditor = \"nano\"\nmcp_base_url = \"127.0.0.2\"\nmcp_port = \"9090\"\ndebug = true\n"

func TestFlatLayoutFailsWithMigrateHint(t *testing.T) {
	mcpEnv(t)
	t.Setenv("TS_JEV_DEBUG", "")
	path := writeConfig(t, filepath.Join(t.TempDir(), "config.toml"), "model = 'private-flat'\n")
	_, loadErr := Load(path, nil)
	_, mcpErr := MCPAddress(path)
	_, debugErr := Debug(path)
	_, viewErr := View(path)
	for _, err := range []error{loadErr, mcpErr, debugErr, viewErr} {
		if !errors.Is(err, ErrFlatLayout) || !strings.Contains(err.Error(), "jevwise config migrate") || strings.Contains(err.Error(), "private") {
			t.Fatal("flat layout not rejected with hint", err)
		}
	}
	writeConfig(t, path, "[provider]\njev = { model = 'grouped' }\n")
	if cfg, err := Load(path, nil); err != nil || cfg.Model != "grouped" {
		t.Fatal("provider table treated as flat", err)
	}
	if editor, err := Editor(writeConfig(t, path, "editor = 'nano'\n")); err != nil || editor != "nano" {
		t.Fatal("flat file cannot be opened for repair", editor, err)
	}
}

func TestMigrateRoundTripKeepsBackupAndLiterals(t *testing.T) {
	mcpEnv(t)
	t.Setenv("TS_JEV_DEBUG", "")
	t.Setenv("JEV_TOKEN", "expanded-token")
	path := writeConfig(t, filepath.Join(t.TempDir(), "config.toml"), flatFixture)
	backup, err := Migrate(context.Background(), path)
	if err != nil || backup != path+BackupSuffix {
		t.Fatal("migration failed", err)
	}
	data, _ := os.ReadFile(path)
	saved, _ := os.ReadFile(backup)
	text := string(data)
	for _, want := range []string{"[general]\nprovider = \"codex\"\ndebug = true\neditor = \"nano\"\n", "[mcp]\nhost = \"127.0.0.2\"\nport = \"9090\"\n", "[provider.codex]\napi_key = \"$JEV_TOKEN\"\nbase_url = \"https://flat.test\"\nmodel = \"flat-model\"\ntimeout = \"garbage\"\n"} {
		if !strings.Contains(text, want) {
			t.Fatal("unexpected migrated layout", text)
		}
	}
	if string(saved) != flatFixture || strings.Contains(text, "# comment") {
		t.Fatal("backup changed or comment carried over")
	}
	for _, name := range []string{path, backup} {
		if info, _ := os.Stat(name); runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
			t.Fatal("migrated files are not private", name)
		}
	}
	writeConfig(t, path, strings.Replace(text, "garbage", "1s", 1))
	cfg, err := Load(path, nil)
	if err != nil || cfg.Provider != "codex" || cfg.APIKey != "expanded-token" || cfg.Model != "flat-model" || cfg.BaseURL != "https://flat.test" {
		t.Fatal("migrated file loads different values", err)
	}
	if address, err := MCPAddress(path); err != nil || address != "127.0.0.2:9090" {
		t.Fatal("migrated MCP address changed", address, err)
	}
	if enabled, err := Debug(path); err != nil || !enabled {
		t.Fatal("migrated debug changed", err)
	}
	if _, err := Migrate(context.Background(), path); !errors.Is(err, ErrNotFlat) {
		t.Fatal("grouped file migrated again", err)
	}
}

func TestMigrateRefusesWithoutChanging(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	for _, content := range []string{
		"model = 'm'\nunknown = 'private-value'\n", "model = 'm'\n[mcp]\nport = 1\n",
		"model = ['private-value']\n", "model = 'm'\n[",
	} {
		writeConfig(t, path, content)
		_, err := Migrate(context.Background(), path)
		after, _ := os.ReadFile(path)
		if err == nil || string(after) != content || strings.Contains(err.Error(), "private") {
			t.Fatal("invalid migration changed file or leaked", content, err)
		}
		if _, statErr := os.Stat(path + BackupSuffix); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatal("refused migration left a backup")
		}
	}
	writeConfig(t, path, "model = 'm'\n")
	writeConfig(t, path+BackupSuffix, "earlier")
	if _, err := Migrate(context.Background(), path); err == nil || !strings.Contains(err.Error(), "backup already exists") {
		t.Fatal("existing backup replaced", err)
	}
	if saved, _ := os.ReadFile(path + BackupSuffix); string(saved) != "earlier" {
		t.Fatal("existing backup changed")
	}
	link := filepath.Join(dir, "link.toml")
	if os.Symlink(writeConfig(t, filepath.Join(dir, "target.toml"), "model = 'm'\n"), link) == nil {
		if _, err := Migrate(context.Background(), link); err == nil {
			t.Fatal("link migrated")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Migrate(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled migration ran", err)
	}
}
