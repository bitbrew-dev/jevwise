package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bitbrew-dev/jevwise/internal/config"
	"github.com/bitbrew-dev/jevwise/internal/service"
)

func TestDecideRejectsMissingOrBlankExpandedKey(t *testing.T) {
	configEnvironment(t)
	const name = "JEVWISE_TEST_DECISION_KEY"
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("api_key = '${"+name+"}'"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", " \t\n "} {
		t.Setenv(name, value)
		out, stderr, err := execute(NewRoot(), "decide", "--config", path, "--prompt", "synthetic", "--option", "A", "--option", "B")
		if cause := errors.Unwrap(err); cause == nil || cause.Error() != "API key is required" || out != "" || stderr != "" {
			t.Fatal("missing expanded key not rejected at service initialization", err)
		}
	}
}

func TestHiddenConfigCustomKeyAndView(t *testing.T) {
	configEnvironment(t)
	t.Setenv("JEV_MCP_API_KEY", "synthetic-custom-key")
	if err := os.WriteFile(config.LocalName, []byte("api_key='${JEV_MCP_API_KEY}'\nmodel='hidden-model'"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("configs.toml", []byte("[invalid TOML"), 0600); err != nil {
		t.Fatal(err)
	}
	created := 0
	factory := func(cfg config.Config) (service.DecisionService, func(), error) {
		created++
		if cfg.APIKey != "synthetic-custom-key" || cfg.Model != "hidden-model" {
			t.Error("wrong config or custom key loaded")
		}
		return decideFunc(func(context.Context, service.Request) (service.Response, error) {
			return service.Response{Choice: "fixture"}, nil
		}), nil, nil
	}
	_, stderr, err := execute(NewRootWithFactory(factory), "decide", "--prompt=fixture", "--option=A", "--option=B")
	if err != nil || stderr != "" || created != 1 {
		t.Fatal("hidden config not used for decisions", err)
	}
	out, stderr, err := execute(NewRootWithFactory(factory), "config", "view")
	if err != nil || stderr != "" || !strings.Contains(out, "hidden-model") || !strings.Contains(out, "[redacted]") || strings.Contains(out, "synthetic-custom-key") || created != 1 {
		t.Fatal("view selected wrong config or exposed expanded key", err)
	}
}

func TestConfigGlobalHomeAndTildeInitViewEdit(t *testing.T) {
	configEnvironment(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	path := filepath.Join(home, ".config", "jevwise", "jevwise.toml")
	literal := "~/.config/jevwise/jevwise.toml"
	_, stderr, err := execute(configRoot(t), "config", "init", "--config", literal)
	if err != nil || stderr != "" {
		t.Fatal("tilde init failed", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("init created literal tilde directory instead of global file")
	}
	if err := os.WriteFile(path, []byte("model='home-global'\napi_key='$JEV_MCP_API_KEY'"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"config", "view"}, {"config", "view", "--config", literal}} {
		out, stderr, err := execute(configRoot(t), args...)
		if err != nil || stderr != "" || !strings.Contains(out, "home-global") || !strings.Contains(out, "[redacted]") {
			t.Fatal("home/tilde global view failed", err)
		}
	}
	called := false
	cmd := editorRoot(t, func(_ context.Context, editor, selected string, _ io.Reader, _, _ io.Writer) error {
		called = true
		if editor != "vim" || selected != path {
			t.Error("tilde edit selected wrong file/editor")
		}
		return nil
	})
	if _, stderr, err := execute(cmd, "config", "edit", "--config", literal); err != nil || stderr != "" || !called {
		t.Fatal("tilde edit failed", err)
	}
}
