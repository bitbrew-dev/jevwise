package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDebugPrecedenceAndValidation(t *testing.T) {
	cleanEnv(t)
	t.Setenv("TS_JEV_DEBUG", "")
	dir := t.TempDir()
	if enabled, err := Debug(filepath.Join(dir, "missing.toml")); err != nil || enabled {
		t.Fatal("missing file did not default to false", err)
	}
	path := filepath.Join(dir, "config.toml")
	for content, want := range map[string]bool{
		"[general]\ndebug = true\n":                                      true,
		"[general]\ndebug = false\n":                                     false,
		"[general]\ndebug = 'true'\n":                                    true,
		"[general]\ndebug = true\n[provider.jev]\ntimeout = 'invalid'\n": true,
		"[provider.jev]\nmodel = 'unrelated'\n":                          false,
	} {
		if enabled, err := Debug(writeConfig(t, path, content)); err != nil || enabled != want {
			t.Fatal("wrong debug value", content, err)
		}
	}
	t.Setenv("TS_JEV_DEBUG", "false")
	if enabled, err := Debug(writeConfig(t, path, "[general]\ndebug = true\n")); err != nil || enabled {
		t.Fatal("environment did not override TOML", err)
	}
	t.Setenv("TS_JEV_DEBUG", "maybe")
	if _, err := Debug(path); err == nil {
		t.Fatal("invalid environment debug accepted")
	}
	t.Setenv("TS_JEV_DEBUG", "")
	for _, content := range []string{"[general]\ndebug = 'maybe'\n", "[general]\ndebug = 1\n", "[general]\ndebug = [\n"} {
		if _, err := Debug(writeConfig(t, path, content)); err == nil {
			t.Fatal("invalid debug accepted", content)
		}
		if _, err := View(path); err == nil {
			t.Fatal("view accepted invalid debug", content)
		}
	}
	text, err := View(writeConfig(t, path, string(template)))
	if err != nil || !strings.Contains(text, "debug = false\n") {
		t.Fatal("view or template lacks debug", err, text)
	}
}
