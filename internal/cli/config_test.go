package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitbrew-dev/jevwise/internal/config"
	"github.com/bitbrew-dev/jevwise/internal/service"
	"github.com/spf13/cobra"
)

func configEnvironment(t *testing.T) {
	t.Helper()
	decisionEnvironment(t)
	t.Chdir(t.TempDir())
}

func configRoot(t *testing.T) *cobra.Command {
	t.Helper()
	return NewRootWithFactory(func(config.Config) (service.DecisionService, func(), error) {
		t.Fatal("config command created decision service")
		return nil, nil, nil
	})
}

func TestConfigInitTargetsNoOverwriteAndAutoLoad(t *testing.T) {
	for _, target := range []string{"global", "local", "explicit"} {
		t.Run(target, func(t *testing.T) {
			configEnvironment(t)
			path, _ := config.GlobalPath()
			args := []string{"config", "init", "--api-key=flag-secret"}
			if target == "local" {
				args = append(args, "--local")
				path, _ = filepath.Abs(config.LocalName)
			} else if target == "explicit" {
				path = filepath.Join(t.TempDir(), "selected.toml")
				args = append(args, "--config", path)
			} else {
				_ = os.WriteFile(config.LocalName, []byte("model='local'"), 0600)
			}
			out, stderr, err := execute(configRoot(t), args...)
			data, readErr := os.ReadFile(path)
			if err != nil || readErr != nil || stderr != "" || !strings.Contains(out, "Created configuration:") || strings.Contains(string(data)+out, "flag-secret") {
				t.Fatal("init target or credential handling failed", err)
			}
			if out, _, err := execute(configRoot(t), args...); err == nil || out != "" {
				t.Fatal("existing configuration overwritten")
			}
			viewArgs := []string{"config", "view"}
			if target != "local" {
				viewArgs = append(viewArgs, "--config", path)
			}
			if out, _, err := execute(configRoot(t), viewArgs...); err != nil || !strings.Contains(out, "[redacted]") {
				t.Fatal("created file not viewable", err)
			}
			if target == "local" {
				cfg, err := config.Load("", nil)
				if err != nil || cfg.Model != "jev-latest" {
					t.Fatal("local init not automatically loaded", err)
				}
			}
		})
	}
}

func TestConfigHelpValidationAndSafeView(t *testing.T) {
	configEnvironment(t)
	for _, args := range [][]string{{"config", "--help"}, {"config", "init", "--help"}, {"config", "view", "--help"}} {
		out, stderr, err := execute(configRoot(t), append(args, "--config", "/missing")...)
		if err != nil || stderr != "" || !strings.Contains(out, "jevwise config") {
			t.Fatal("help loaded configuration", err)
		}
	}
	for _, args := range [][]string{{"config", "private-secret"}, {"config", "init", "private-secret"}, {"config", "view", "private-secret"}, {"config", "init", "--local", "--config", "private-secret"}, {"config", "view", "--bad=private-secret"}, {"config", "view"}, {"config", "init", "--local", "--config="}, {"config", "init", "--config="}, {"config", "view", "--config="}} {
		out, stderr, err := execute(configRoot(t), args...)
		if err == nil || out != "" || stderr != "" || strings.Contains(err.Error(), "private-secret") {
			t.Fatal("invalid config request accepted or leaked", err)
		}
	}
	_ = os.WriteFile(config.LocalName, []byte("api_key='private-secret'\nmodel='file-model'"), 0600)
	t.Setenv("TS_JEV_MODEL", "environment-model")
	out, _, err := execute(configRoot(t), "config", "view", "--api-key=flag-secret")
	if err != nil || strings.Contains(out, "secret") || strings.Contains(out, "environment-model") || !strings.Contains(out, "file-model") {
		t.Fatal("view exposed credentials or effective overrides", err)
	}
	_ = os.WriteFile(config.LocalName, []byte("api_key='private-secret'\n["), 0600)
	if out, _, err := execute(configRoot(t), "config", "view"); err == nil || out != "" || strings.Contains(err.Error(), "private-secret") {
		t.Fatal("parser diagnostic leaked", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := configRoot(t)
	cmd.SetArgs([]string{"config", "view"})
	var buffer bytes.Buffer
	cmd.SetOut(&buffer)
	if err := cmd.ExecuteContext(ctx); !errors.Is(err, context.Canceled) || buffer.Len() != 0 {
		t.Fatal("cancelled view ran", err)
	}
}

func TestConfigOutputFailureDoesNotLeakOrUndoInit(t *testing.T) {
	configEnvironment(t)
	for _, operation := range []string{"init", "view"} {
		cmd := configRoot(t)
		cmd.SetArgs([]string{"config", operation})
		cmd.SetOut(secretIO{})
		err := cmd.ExecuteContext(context.Background())
		if err == nil || strings.Contains(err.Error(), "private-content") {
			t.Fatal("writer error leaked", err)
		}
		path, _ := config.GlobalPath()
		if _, err := os.Stat(path); err != nil {
			t.Fatal("output failure undid initialization", err)
		}
	}
}

func TestConfigShortOutputIsAnError(t *testing.T) {
	configEnvironment(t)
	for _, operation := range []string{"init", "view"} {
		cmd := configRoot(t)
		cmd.SetArgs([]string{"config", operation})
		cmd.SetOut(managementWriter(func(data []byte) (int, error) { return len(data) - 1, nil }))
		if err := cmd.ExecuteContext(context.Background()); err == nil {
			t.Fatal("short output accepted")
		}
	}
}
