package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/config"
	"github.com/bitbrew-dev/jevwise/internal/daemon"
	"github.com/bitbrew-dev/jevwise/internal/mcpserver"
	"github.com/bitbrew-dev/jevwise/internal/service"
	"github.com/spf13/cobra"
)

func cliTokenFile(t *testing.T, token string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "private-runtime")
	store, err := daemon.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Create("control.key", []byte(token)); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "control.key")
}

func TestMCPTokenFileOverridesEnvironmentAndAcceptsRelativePath(t *testing.T) {
	decisionEnvironment(t)
	t.Setenv("JEV_MCP_TOKEN", "invalid environment token")
	path := cliTokenFile(t, "private-file-token\r\n")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{path, relative} {
		created, cleaned := 0, 0
		factory := func(config.Config) (service.DecisionService, func(), error) {
			created++
			return mcpFixtureService(t), func() { cleaned++ }, nil
		}
		run := func(ctx context.Context, _ service.DecisionService, _ time.Duration, options mcpserver.RunOptions) error {
			if options.Token != "private-file-token" {
				t.Error("file did not override environment")
			}
			return options.Ready(ctx, "http://127.0.0.1:8080/mcp")
		}
		out, stderr, err := execute(mcpRoot(factory, run), "mcp", "--token-file", name)
		if err != nil || stderr != "" || out != "http://127.0.0.1:8080/mcp\n" || created != 1 || cleaned != 1 {
			t.Fatal("file token lifecycle failed", err)
		}
	}
}

func TestMCPInvalidSelectedTokenFileNeverFallsBackOrInitializes(t *testing.T) {
	decisionEnvironment(t)
	t.Setenv("JEV_MCP_TOKEN", "valid-env-token")
	factory := func(config.Config) (service.DecisionService, func(), error) {
		t.Fatal("invalid token file initialized service")
		return nil, nil, nil
	}
	for _, path := range []string{"", filepath.Join(t.TempDir(), "private-missing"), t.TempDir(), cliTokenFile(t, "private invalid token"), cliTokenFile(t, "private-file-token\n\n")} {
		out, stderr, err := execute(NewRootWithFactory(factory), "mcp", "--token-file", path)
		if err == nil || out != "" || stderr != "" || strings.Contains(err.Error(), "private") {
			t.Fatal("invalid file token was accepted or echoed", err)
		}
	}
	path := cliTokenFile(t, "private-file-token")
	if _, _, err := execute(NewRootWithFactory(factory), "mcp", "--token-file", path, "--api-key", "private-file-token"); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal("file token reused upstream key", err)
	}
}

func TestMCPTokenFileHelpAndControlCommandsDoNotRead(t *testing.T) {
	decisionEnvironment(t)
	t.Setenv("JEV_MCP_TOKEN", "")
	factory := func(config.Config) (service.DecisionService, func(), error) {
		t.Fatal("help or control initialized service")
		return nil, nil, nil
	}
	out, stderr, err := execute(NewRootWithFactory(factory), "mcp", "--token-file", "/private-missing", "--help", "--config", "/nonexistent")
	if err != nil || stderr != "" || !strings.Contains(out, "--token-file") {
		t.Fatal("token file help was not lazy", err)
	}
	// Future real control subcommands inherit the flag but never execute MCP RunE.
	for _, name := range []string{"status", "stop"} {
		root := NewRootWithFactory(factory)
		mcp, _, err := root.Find([]string{"mcp"})
		if err != nil {
			t.Fatal(err)
		}
		for _, child := range mcp.Commands() {
			if child.Name() == name {
				mcp.RemoveCommand(child)
			}
		}
		called := false
		mcp.AddCommand(&cobra.Command{Use: name, RunE: func(*cobra.Command, []string) error { called = true; return nil }})
		out, stderr, err := execute(root, "mcp", name, "--token-file", "/private-missing", "--config", "/nonexistent")
		if err != nil || out != "" || stderr != "" || !called {
			t.Fatal("control subcommand loaded token/config", err)
		}
	}
}
