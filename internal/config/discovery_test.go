package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDiscoveryAllowlistsHiddenLocalAndNamedGlobal(t *testing.T) {
	cleanEnv(t)
	for _, name := range []string{"jevwise.toml", "config.toml", "configs.toml", ".config.toml", "config.yaml", "jevwise.yaml", ".jevwise.toml.bak"} {
		writeConfig(t, name, "[invalid TOML")
	}
	writeConfig(t, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "ts-jev", "config.toml"), "[old invalid config")
	writeConfig(t, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "jevwise", "config.toml"), "[unrelated config")
	t.Setenv("JEV_MCP_API_KEY", "custom-variable-is-not-an-override")
	cfg, err := Load("", nil)
	if err != nil || cfg.Model != "jev-latest" || cfg.APIKey != "" {
		t.Fatal("unrelated file or custom environment override loaded", err)
	}
	global, _ := GlobalPath()
	writeConfig(t, global, "model='global'\napi_key='global-only-key'")
	cfg, err = Load("", nil)
	if err != nil || cfg.Model != "global" {
		t.Fatal("named Jevwise global file not selected", err)
	}
	writeConfig(t, LocalName, "model='local'\napi_key='$JEV_MCP_API_KEY'")
	cfg, err = Load("", nil)
	path, required, pathErr := ResolvePath("")
	if err != nil || pathErr != nil || !required || filepath.Base(path) != LocalName || cfg.Model != "local" || cfg.APIKey != "custom-variable-is-not-an-override" {
		t.Fatal("local file or custom reference not selected", err)
	}
	writeConfig(t, LocalName, "model='local-only'")
	cfg, err = Load("", nil)
	if err != nil || cfg.APIKey != "" {
		t.Fatal("local and global files merged", err)
	}
	writeConfig(t, LocalName, "[invalid TOML")
	if _, err = Load("", nil); err == nil {
		t.Fatal("invalid local config fell back to global")
	}
}

func TestGlobalHomeAndLiteralTildePaths(t *testing.T) {
	cleanEnv(t)
	t.Setenv("XDG_CONFIG_HOME", "")
	home := os.Getenv("HOME")
	expected := filepath.Join(home, ".config", "jevwise", GlobalName)
	writeConfig(t, expected, "model='home-global'\napi_key='${JEV_MCP_API_KEY}'")
	t.Setenv("JEV_MCP_API_KEY", "synthetic-custom-key")
	for _, path := range []string{"", "~/.config/jevwise/jevwise.toml", expected} {
		cfg, err := Load(path, nil)
		if err != nil || cfg.Model != "home-global" || cfg.APIKey != "synthetic-custom-key" {
			t.Fatal("home/global/tilde config not loaded", err)
		}
	}
	actual, err := GlobalPath()
	if err != nil || actual != expected {
		t.Fatal("wrong global namespace", err)
	}
	t.Setenv("XDG_CONFIG_HOME", "~/.config")
	actual, err = GlobalPath()
	if err != nil || actual != expected {
		t.Fatal("tilde XDG path not expanded", err)
	}
	if _, _, err := ResolvePath("~otheruser/jevwise.toml"); err == nil {
		t.Fatal("unsupported tilde form accepted")
	}
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	if _, _, err := ResolvePath("~/jevwise.toml"); err == nil {
		t.Fatal("missing home resolved literal tilde path")
	}
}

func TestGlobalDirectorySymlinkSupportsDotfiles(t *testing.T) {
	cleanEnv(t)
	target := t.TempDir()
	global, _ := GlobalPath()
	if runtime.GOOS == "windows" {
		t.Skip("directory symlink creation may require privileges")
	}
	if err := os.Symlink(target, filepath.Dir(global)); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, filepath.Join(target, GlobalName), "model='dotfiles'")
	cfg, err := Load("", nil)
	if err != nil || cfg.Model != "dotfiles" {
		t.Fatal("trusted dotfiles directory symlink rejected", err)
	}
}
