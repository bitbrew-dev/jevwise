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
)

func TestMCPAutomaticTokenForegroundAndReadOnlyPath(t *testing.T) {
	decisionEnvironment(t)
	dir := filepath.Join(t.TempDir(), "runtime")
	factory := func(config.Config) (service.DecisionService, func(), error) { return mcpFixtureService(t), nil, nil }
	path := filepath.Join(dir, daemon.AgentTokenName)
	out, stderr, err := execute(mcpRoot(factory, nil), "mcp", "token-path", "--runtime-dir", dir, "--config", "/missing", "--token-file", "/missing")
	if err != nil || stderr != "" || out != path+"\n" {
		t.Fatal("token-path loaded credentials", err)
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatal("token-path created storage")
	}
	var first string
	run := func(_ context.Context, _ service.DecisionService, _ time.Duration, options mcpserver.RunOptions) error {
		if first == "" {
			first = options.Token
		}
		if options.Token != first || len(first) != 64 {
			t.Error("automatic token changed")
		}
		return nil
	}
	for range 2 {
		root := mcpRoot(factory, run)
		mcp, _, _ := root.Find([]string{"mcp"})
		instrumentCommands(mcp)
		out, stderr, err := execute(root, "mcp", "--runtime-dir", dir, "--api-key", "private-upstream", "--debug")
		if err != nil || out != "" || !strings.Contains(stderr, "mcp.token.auto_resolve") || strings.Contains(stderr, first) || strings.Contains(stderr, "private-upstream") {
			t.Fatal("automatic startup failed or leaked credentials", err)
		}
	}
	if token, err := daemon.ReadAgentToken(path); err != nil || token != first {
		t.Fatal("server did not use persisted token", err)
	}
}

func TestAutomaticTokenOverridesAndLazyCommandsHaveNoStorageEffects(t *testing.T) {
	decisionEnvironment(t)
	dir := filepath.Join(t.TempDir(), "must-stay-absent")
	factory := func(config.Config) (service.DecisionService, func(), error) { return mcpFixtureService(t), nil, nil }
	run := func(context.Context, service.DecisionService, time.Duration, mcpserver.RunOptions) error { return nil }
	for _, args := range [][]string{
		{"mcp", "--help"}, {"mcp"}, {"mcp", "--api-key", "private-key", "--token-file", "/missing"},
		{"mcp", "--api-key", "private-key", "--provider", "codex"},
	} {
		_, _, _ = execute(mcpRoot(factory, run), append(args, "--runtime-dir", dir)...)
	}
	for _, value := range []string{"valid-explicit-token", "invalid private token"} {
		t.Setenv("JEVWISE_MCP_TOKEN", value)
		_, _, _ = execute(mcpRoot(factory, run), "mcp", "--api-key", "private-key", "--runtime-dir", dir)
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatal("override, lazy or invalid startup created managed token storage")
	}
}

func TestAutomaticTokenCreatesMissingDefaultCacheParent(t *testing.T) {
	decisionEnvironment(t)
	home := t.TempDir()
	for _, name := range []string{"HOME", "USERPROFILE"} {
		t.Setenv(name, home)
	}
	for _, name := range []string{"XDG_CACHE_HOME", "LOCALAPPDATA"} {
		t.Setenv(name, filepath.Join(home, "new-cache"))
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	factory := func(config.Config) (service.DecisionService, func(), error) { return mcpFixtureService(t), nil, nil }
	run := func(context.Context, service.DecisionService, time.Duration, mcpserver.RunOptions) error { return nil }
	if _, _, err := execute(mcpRoot(factory, run), "mcp", "--api-key", "private-upstream"); err != nil {
		t.Fatal("fresh default cache startup failed", err)
	}
	if _, err := daemon.ReadAgentToken(filepath.Join(cache, "jevwise-mcp", daemon.AgentTokenName)); err != nil {
		t.Fatal("fresh default cache token was not saved privately", err)
	}
}

func TestMCPTokenPathFailuresStaySafe(t *testing.T) {
	decisionEnvironment(t)
	for _, args := range [][]string{{"mcp", "token-path", "--runtime-dir", ""}, {"mcp", "token-path", "private-argument"}} {
		if _, _, err := execute(NewRoot(), args...); err == nil || strings.Contains(err.Error(), "private") {
			t.Fatal("invalid token-path accepted or echoed arguments", err)
		}
	}
	root := NewRoot()
	root.SetArgs([]string{"mcp", "token-path"})
	root.SetOut(managementWriter(func(data []byte) (int, error) { return len(data) - 1, nil }))
	if err := root.ExecuteContext(context.Background()); err == nil || err.Error() != "cannot write MCP token path" {
		t.Fatal("token-path short write was not safely reported", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root = NewRoot()
	root.SetArgs([]string{"mcp", "token-path"})
	if err := root.ExecuteContext(ctx); err != context.Canceled {
		t.Fatal("token-path ignored cancellation", err)
	}
}
