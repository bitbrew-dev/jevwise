package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func cleanEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"TS_JEV_API_KEY", "TS_JEV_BASE_URL", "TS_JEV_MODEL", "TS_JEV_PROVIDER", "TS_JEV_TIMEOUT", "TYPESAFE_API_KEY", "TYPESAFE_BASE_URL", "TYPESAFE_DEFAULT_MODEL"} {
		t.Setenv(name, "")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
}

func writeConfig(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefaultsAndDiscovery(t *testing.T) {
	cleanEnv(t)
	want := Config{BaseURL: "https://api.typesafe.ai", Model: "jev-latest", Provider: "jev", Timeout: 10 * time.Second}
	got, err := Load("", nil)
	if err != nil || got != want {
		t.Fatalf("defaults: %+v %v", got, err)
	}
	xdg := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "ts-jev", "config.toml")
	home := filepath.Join(os.Getenv("HOME"), ".config", "ts-jev", "config.toml")
	writeConfig(t, xdg, `model = "xdg"`)
	writeConfig(t, home, `model = "home"`)
	got, err = Load("", nil)
	if err != nil || got.Model != "xdg" {
		t.Fatalf("XDG: %+v %v", got, err)
	}
	t.Setenv("XDG_CONFIG_HOME", " ")
	got, err = Load("", nil)
	if err != nil || got.Model != "home" {
		t.Fatalf("HOME: %+v %v", got, err)
	}
	t.Setenv("HOME", "")
	if _, err = Load("", nil); err == nil {
		t.Fatal("expected discovery error without HOME")
	}
}

func TestPrecedenceAndIsolation(t *testing.T) {
	cleanEnv(t)
	path := writeConfig(t, filepath.Join(t.TempDir(), "config.toml"), "api_key='file'\nbase_url='https://file.test'\nmodel='file'\nprovider='codex'\ntimeout='1s'")
	file, err := Load(path, nil)
	if err != nil || file.APIKey != "file" || file.Provider != "codex" || file.Timeout != time.Second {
		t.Fatalf("file: %+v %v", file, err)
	}
	for name, value := range map[string]string{"TS_JEV_API_KEY": " env ", "TS_JEV_BASE_URL": "https://env.test", "TS_JEV_MODEL": "env", "TS_JEV_PROVIDER": "claude", "TS_JEV_TIMEOUT": "2s"} {
		t.Setenv(name, value)
	}
	t.Setenv("TYPESAFE_API_KEY", "fallback")
	t.Setenv("TYPESAFE_BASE_URL", "https://fallback.test")
	t.Setenv("TYPESAFE_DEFAULT_MODEL", "fallback")
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	for _, name := range []string{"api-key", "base-url", "model", "provider"} {
		flags.String(name, "ignored", "")
	}
	flags.Duration("timeout", 99*time.Second, "")
	env, err := Load(path, flags)
	if err != nil || env.APIKey != "env" || env.BaseURL != "https://env.test" || env.Model != "env" || env.Provider != "claude" || env.Timeout != 2*time.Second {
		t.Fatalf("environment: %+v %v", env, err)
	}
	if err := flags.Parse([]string{"--api-key=flag", "--base-url=https://flag.test", "--model=flag", "--provider=jev", "--timeout=3s"}); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path, flags)
	if err != nil || got.APIKey != "flag" || got.BaseURL != "https://flag.test" || got.Model != "flag" || got.Provider != "jev" || got.Timeout != 3*time.Second {
		t.Fatalf("flags: %+v %v", got, err)
	}
	for _, name := range []string{"API_KEY", "BASE_URL", "MODEL"} {
		t.Setenv("TS_JEV_"+name, " \t ")
	}
	fallback, err := Load("", nil)
	if err != nil || fallback.APIKey != "fallback" || fallback.BaseURL != "https://fallback.test" || fallback.Model != "fallback" {
		t.Fatalf("upstream fallback: %+v %v", fallback, err)
	}
	// Loading again does not inherit flags, files or global Viper values.
	viper.Set("model", "global")
	t.Cleanup(viper.Reset)
	cleanEnv(t)
	isolated, err := Load("", nil)
	if err != nil || isolated.Model != "jev-latest" || isolated.APIKey != "" {
		t.Fatalf("isolation: %+v %v", isolated, err)
	}
}

func TestInvalidFilesAndValues(t *testing.T) {
	cleanEnv(t)
	if _, err := Load(filepath.Join(t.TempDir(), "missing.toml"), nil); err == nil {
		t.Fatal("explicit missing file accepted")
	}
	for _, content := range []string{"api_key = 'secret'\n[", "timeout='garbage'", "timeout='0s'", "timeout='-1s'", "timeout=10", "base_url='/relative'", "base_url='ftp://example.com'", "base_url='https://user:secret@example.com'", "base_url='https://example.com?q=secret'", "provider='other'", "model=' '", "api_key=42"} {
		t.Run(content, func(t *testing.T) {
			path := writeConfig(t, filepath.Join(t.TempDir(), "config.toml"), content)
			if _, err := Load(path, nil); err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatalf("unsafe or missing error: %v", err)
			}
		})
	}
	for name, value := range map[string]string{"TIMEOUT": "not-a-duration", "BASE_URL": "relative", "PROVIDER": "unknown"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("TS_JEV_"+name, value)
			if _, err := Load("", nil); err == nil {
				t.Fatal("invalid environment accepted")
			}
		})
	}
	path := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "ts-jev", "config.toml")
	writeConfig(t, path, "[")
	if _, err := Load("", nil); err == nil {
		t.Fatal("invalid discovered file accepted")
	}
}
