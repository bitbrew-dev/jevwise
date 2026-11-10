package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/config"
	"github.com/bitbrew-dev/jevwise/internal/mcpserver"
	"github.com/bitbrew-dev/jevwise/internal/service"
)

func mcpListenConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMCPListenFromConfigMatchesFlag(t *testing.T) {
	decisionEnvironment(t)
	t.Setenv("JEV_MCP_TOKEN", "fixture-agent-token")
	factory := func(config.Config) (service.DecisionService, func(), error) { return mcpFixtureService(t), nil, nil }
	path := mcpListenConfig(t, "api_key = 'fixture-upstream-key'\nmcp_base_url = '127.0.0.1'\nmcp_port = 9090\n")
	for _, args := range [][]string{
		{"mcp", "--config", path},
		{"mcp", "--config", path, "--listen", "127.0.0.1:9090"},
		{"mcp", "--config", mcpListenConfig(t, "api_key = 'fixture-upstream-key'\nmcp_port = 7070\n"), "--listen", "127.0.0.1:9090"},
	} {
		address := ""
		run := func(ctx context.Context, _ service.DecisionService, _ time.Duration, options mcpserver.RunOptions) error {
			address = options.Address
			return nil
		}
		if _, _, err := execute(mcpRoot(factory, run), args...); err != nil || address != "127.0.0.1:9090" {
			t.Fatal("listen address not resolved from config or flag", args, address, err)
		}
	}
	run := func(context.Context, service.DecisionService, time.Duration, mcpserver.RunOptions) error {
		t.Fatal("invalid MCP setting started runner")
		return nil
	}
	for _, content := range []string{"mcp_base_url = 'http://127.0.0.1'\n", "mcp_port = 0\n"} {
		if _, _, err := execute(mcpRoot(factory, run), "mcp", "--config", mcpListenConfig(t, "api_key = 'k'\n"+content)); err == nil {
			t.Fatal("invalid MCP setting accepted", content)
		}
	}
}

func TestMCPInstallListenFromConfig(t *testing.T) {
	for _, tc := range []struct {
		content string
		args    []string
		want    string
	}{
		{"mcp_port = 9090\n", nil, "http://127.0.0.1:9090/mcp"},
		{"mcp_port = 9090\ntimeout = 'invalid'\n", []string{"--listen", "127.0.0.1:7070"}, "http://127.0.0.1:7070/mcp"},
	} {
		url := ""
		run := func(_ context.Context, _ string, args ...string) ([]byte, error) {
			url = args[len(args)-1]
			return nil, nil
		}
		root := installerRoot(t, run)
		args := append([]string{"mcp", "install", "claude", "--config", mcpListenConfig(t, tc.content)}, tc.args...)
		if _, _, err := execute(root, args...); err != nil || !strings.Contains(url, `"url":"`+tc.want+`"`) {
			t.Fatal("install endpoint not resolved from config or flag", url, err)
		}
	}
	if _, _, err := execute(installerRoot(t, nil), "mcp", "install", "claude", "--config", mcpListenConfig(t, "mcp_base_url = 'localhost'\n")); err == nil {
		t.Fatal("install accepted non-literal MCP host")
	}
}
