package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func cleanEnv(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	for _, name := range []string{"JEV_API_KEY", "JEVWISE_API_KEY", "TS_JEV_API_KEY", "TS_JEV_BASE_URL", "TS_JEV_MODEL", "TS_JEV_PROVIDER", "TS_JEV_TIMEOUT", "TYPESAFE_API_KEY", "TYPESAFE_BASE_URL", "TYPESAFE_DEFAULT_MODEL", "GOOGLE_API_KEY", "GEMINI_API_KEY", "GOOGLE_GEMINI_BASE_URL"} {
		t.Setenv(name, "")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", os.Getenv("HOME"))
	}
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
	xdg := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "jevwise", GlobalName)
	home := filepath.Join(os.Getenv("HOME"), ".config", "jevwise", GlobalName)
	writeConfig(t, xdg, `[provider.jev]`+"\n"+`model = "xdg"`)
	writeConfig(t, home, `[provider.jev]`+"\n"+`model = "home"`)
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
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", "")
	}
	if _, err = Load("", nil); err == nil {
		t.Fatal("expected discovery error without HOME")
	}
}

func TestPrecedenceAndIsolation(t *testing.T) {
	cleanEnv(t)
	path := writeConfig(t, filepath.Join(t.TempDir(), "config.toml"), "[general]\nprovider='codex'\n[provider.codex]\napi_key='file'\nbase_url='https://file.test'\nmodel='file'\ntimeout='1s'")
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
	for _, content := range []string{"[provider.jev]\napi_key = 'secret'\n[", "[provider.jev]\ntimeout='garbage'", "[provider.jev]\ntimeout='0s'", "[provider.jev]\ntimeout='-1s'", "[provider.jev]\ntimeout=10", "[provider.jev]\nbase_url='/relative'", "[provider.jev]\nbase_url='ftp://example.com'", "[provider.jev]\nbase_url='https://user:secret@example.com'", "[provider.jev]\nbase_url='https://example.com?q=secret'", "[general]\nprovider='other'", "[provider.jev]\nmodel=' '", "[provider.jev]\napi_key=42"} {
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
	path := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "jevwise", GlobalName)
	writeConfig(t, path, "[")
	if _, err := Load("", nil); err == nil {
		t.Fatal("invalid discovered file accepted")
	}
}

func TestProviderTablesAreIsolated(t *testing.T) {
	cleanEnv(t)
	path := writeConfig(t, filepath.Join(t.TempDir(), "config.toml"), "[provider.jev]\napi_key='jev-only-key'\nmodel='jev-model'\n[provider.codex]\nmodel='codex-model'\n[provider.gemini]\napi_key='gemini-only-key'\nmodel='gemini-decision-1'\n")
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("provider", "jev", "")
	if err := flags.Parse([]string{"--provider=codex"}); err != nil {
		t.Fatal(err)
	}
	codex, err := Load(path, flags)
	if err != nil || codex.Provider != "codex" || codex.Model != "codex-model" || codex.APIKey != "" {
		t.Fatal("selected provider read another provider table", err)
	}
	jev, err := Load(path, nil)
	if err != nil || jev.Model != "jev-model" || jev.APIKey != "jev-only-key" {
		t.Fatal("default provider table not used or unknown provider table rejected", err)
	}
	text, err := View(path)
	if err != nil || strings.Contains(text, "gemini") || strings.Contains(text, "codex-model") {
		t.Fatal("view printed an unselected provider table", err)
	}
}

func TestGeminiSettingsNeverUseJevCredentialsOrDestinations(t *testing.T) {
	cleanEnv(t)
	for name, value := range map[string]string{"JEV_API_KEY": "jev-key", "TYPESAFE_API_KEY": "typesafe-key", "TS_JEV_BASE_URL": "https://jev.test", "TYPESAFE_DEFAULT_MODEL": "jev-model"} {
		t.Setenv(name, value)
	}
	dir := t.TempDir()
	path := writeConfig(t, filepath.Join(dir, "config.toml"), "[general]\nprovider='gemini'\n")
	cfg, err := Load(path, nil)
	if err != nil || cfg.APIKey != "" || cfg.BaseURL != "https://generativelanguage.googleapis.com" || cfg.Model != "gemini-decision-1" || cfg.Timeout != 10*time.Second {
		t.Fatal("gemini picked up Jev settings or lost its defaults", err)
	}
	t.Setenv("GEMINI_API_KEY", "gemini-key")
	if cfg, err := Load(path, nil); err != nil || cfg.APIKey != "gemini-key" {
		t.Fatal("GEMINI_API_KEY ignored", err)
	}
	t.Setenv("GOOGLE_API_KEY", "google-key")
	t.Setenv("GOOGLE_GEMINI_BASE_URL", "https://gemini.test")
	if cfg, err := Load(path, nil); err != nil || cfg.APIKey != "google-key" || cfg.BaseURL != "https://gemini.test" {
		t.Fatal("GOOGLE_API_KEY must win and GOOGLE_GEMINI_BASE_URL must apply", err)
	}
	jevPath := writeConfig(t, filepath.Join(dir, "jev.toml"), "[provider.gemini]\napi_key='file-gemini-key'\n")
	if cfg, err := Load(jevPath, nil); err != nil || cfg.APIKey != "jev-key" || cfg.BaseURL != "https://jev.test" || cfg.Model != "jev-model" {
		t.Fatal("Jev picked up Gemini settings", err)
	}
	t.Setenv("GOOGLE_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	writeConfig(t, path, "[general]\nprovider='gemini'\n[provider.gemini]\napi_key='$GEMINI_FILE_KEY'\nmodel='gemini-decision-2'\n")
	t.Setenv("GEMINI_FILE_KEY", "file-gemini-key")
	text, viewErr := View(path)
	if cfg, err := Load(path, nil); err != nil || viewErr != nil || cfg.APIKey != "file-gemini-key" || cfg.Model != "gemini-decision-2" || !strings.Contains(text, "[provider.gemini]\napi_key = \"[redacted]\"\nbase_url = \"https://generativelanguage.googleapis.com\"") {
		t.Fatal("gemini table not used by load or view", err, viewErr, text)
	}
}
