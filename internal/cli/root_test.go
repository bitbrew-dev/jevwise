package cli

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

type forbiddenTransport struct{ t *testing.T }

func (f forbiddenTransport) RoundTrip(*http.Request) (*http.Response, error) {
	f.t.Fatal("help must not access the network")
	return nil, nil
}

func execute(cmd *cobra.Command, args ...string) (string, string, error) {
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return out.String(), stderr.String(), err
}

func TestHelpNeedsNeitherConfigCredentialsNorNetwork(t *testing.T) {
	t.Setenv("TS_JEV_API_KEY", "")
	t.Setenv("TYPESAFE_API_KEY", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	before := http.DefaultTransport
	http.DefaultTransport = forbiddenTransport{t}
	t.Cleanup(func() { http.DefaultTransport = before })
	for _, args := range [][]string{nil, {"--help", "--config", "/nonexistent/config.toml"}} {
		out, stderr, err := execute(NewRoot(), args...)
		if err != nil || stderr != "" || !strings.Contains(out, "Make decisions with Jev") {
			t.Fatalf("help = %q, stderr = %q, error = %v", out, stderr, err)
		}
		for _, flag := range []string{"config", "provider", "api-key", "base-url", "model", "timeout"} {
			if !strings.Contains(out, "--"+flag) {
				t.Errorf("help missing %s", flag)
			}
		}
	}
}

func TestInvalidFlagsAndArguments(t *testing.T) {
	for _, args := range [][]string{{"--unknown=private-secret"}, {"--api-key"}, {"private-secret"}} {
		out, stderr, err := execute(NewRoot(), args...)
		if err == nil || out != "" || stderr != "" || strings.Contains(err.Error(), "private-secret") {
			t.Fatalf("args %q: out = %q, stderr = %q, error = %v", args, out, stderr, err)
		}
	}
}

func configCommand(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{Use: "check", RunE: func(cmd *cobra.Command, _ []string) error {
		_, err := LoadConfig(cmd)
		return err
	}}
	root.AddCommand(cmd)
	return cmd
}

func TestConfigHelperAndIsolatedRoots(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	first, second := NewRoot(), NewRoot()
	child := configCommand(first)
	if _, _, err := execute(first, "check", "--model", "custom", "--provider", "claude"); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(child)
	if err != nil || cfg.Model != "custom" || cfg.Provider != "claude" {
		t.Fatalf("child config = %+v, error = %v", cfg, err)
	}
	if second.PersistentFlags().Lookup("model").Changed {
		t.Fatal("flag change leaked between roots")
	}
	if _, _, err := execute(second); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadConfig(second)
	if err != nil || cfg.Model == "custom" {
		t.Fatalf("second root inherited config, error = %v", err)
	}
}

func TestConfigErrorsAreDeferredAndSafe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.toml")
	if err := os.WriteFile(path, []byte("api_key = private-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"check", "--config", path}, {"check", "--timeout", "invalid"}, {"check", "--provider", "unknown"}} {
		root := NewRoot()
		configCommand(root)
		out, _, err := execute(root, args...)
		if err == nil || out != "" || strings.Contains(err.Error(), "private-secret") {
			t.Fatalf("config error = %v, out = %q", err, out)
		}
	}
}

func TestLoggerIsScopedToErrorStream(t *testing.T) {
	first, second := NewRoot(), NewRoot()
	var firstOut, firstErr, secondErr bytes.Buffer
	first.SetOut(&firstOut)
	first.SetErr(&firstErr)
	second.SetErr(&secondErr)
	logger := Logger(first)
	logger.Info().Msg("safe status")
	if firstOut.Len() != 0 || secondErr.Len() != 0 || !strings.Contains(firstErr.String(), "safe status") {
		t.Fatal("logger escaped its command's stderr stream")
	}
}

func TestCommandReceivesExecutionContext(t *testing.T) {
	root := NewRoot()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root.AddCommand(&cobra.Command{Use: "context", RunE: func(cmd *cobra.Command, _ []string) error {
		return cmd.Context().Err()
	}})
	root.SetArgs([]string{"context"})
	if err := root.ExecuteContext(ctx); err != context.Canceled {
		t.Fatalf("context not propagated: %v", err)
	}
}
