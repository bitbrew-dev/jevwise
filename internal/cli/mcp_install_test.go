package cli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/bitbrew-dev/jevwise/internal/config"
	"github.com/bitbrew-dev/jevwise/internal/service"
	"github.com/spf13/cobra"
)

func installerRoot(t *testing.T, run agentCommand) *cobra.Command {
	t.Helper()
	root := NewRootWithFactory(func(config.Config) (service.DecisionService, func(), error) {
		t.Fatal("installation initialized upstream service")
		return nil, nil, nil
	})
	mcp, _, _ := root.Find([]string{"mcp"})
	for _, child := range mcp.Commands() {
		if child.Name() == "install" {
			mcp.RemoveCommand(child)
		}
	}
	child := newMCPInstall(run)
	mcp.AddCommand(child)
	instrumentCommands(child)
	return root
}

func TestMCPInstallClientArgumentsAndNoCredentialOutput(t *testing.T) {
	for _, client := range []string{"codex", "claude"} {
		calls := 0
		run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
			calls++
			if _, ok := ctx.Deadline(); !ok || name != client {
				t.Error("installer lacks timeout or selected wrong client")
			}
			if client == "codex" && calls == 1 {
				if strings.Join(args, " ") != "mcp list --json" {
					t.Error("Codex preflight changed")
				}
				return []byte(`[{"name":"unrelated"}]`), nil
			}
			if client == "codex" {
				if strings.Join(args, " ") != "mcp add jevwise --url http://127.0.0.1:9090/mcp --bearer-token-env-var JEVWISE_MCP_TOKEN" {
					t.Error("Codex registration changed")
				}
			} else {
				var entry struct {
					Type, URL string
					Headers   map[string]string
				}
				if len(args) != 7 || strings.Join(args[:6], " ") != "--bare mcp add-json --scope user jevwise" || json.Unmarshal([]byte(args[6]), &entry) != nil || entry.Type != "http" || entry.URL != "http://127.0.0.1:9090/mcp" || entry.Headers["Authorization"] != "Bearer ${JEVWISE_MCP_TOKEN}" {
					t.Error("Claude literal environment reference or scope changed")
				}
			}
			return []byte("private-client-output"), nil
		}
		out, stderr, err := execute(installerRoot(t, run), "mcp", "install", client, "--listen", "127.0.0.1:9090", "--debug", "--config", "/missing", "--api-key", "private-key")
		if err != nil || !strings.Contains(out, "Registered") || !strings.Contains(stderr, "mcp.install.register") || strings.Contains(out+stderr, "private") {
			t.Fatal("installation failed or leaked client output", err)
		}
	}
}

func TestMCPInstallPreflightAndClientFailuresNeverOverwriteOrLeak(t *testing.T) {
	cause := errors.New("private-client-error")
	for _, data := range []string{`[{"name":"jevwise"}]`, `null`, `{}`, `[{}]`, `private-invalid`, strings.Repeat("x", agentOutputLimit+1)} {
		calls := 0
		run := func(context.Context, string, ...string) ([]byte, error) { calls++; return []byte(data), nil }
		out, stderr, err := execute(installerRoot(t, run), "mcp", "install", "codex")
		if err == nil || calls != 1 || out != "" || stderr != "" || strings.Contains(err.Error(), "private") {
			t.Fatal("bad inventory overwrote settings or leaked", err)
		}
	}
	for _, client := range []string{"codex", "claude"} {
		run := func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if len(args) > 1 && args[1] == "list" {
				return []byte(`[]`), nil
			}
			return []byte("private-output"), cause
		}
		out, stderr, err := execute(installerRoot(t, run), "mcp", "install", client)
		if !errors.Is(err, cause) || out != "" || stderr != "" || strings.Contains(err.Error(), "private") {
			t.Fatal("client failure not safely retained", err)
		}
	}
}

func TestMCPInstallLazyHelpAndInvalidInput(t *testing.T) {
	run := func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("lazy or invalid command ran client")
		return nil, nil
	}
	for _, args := range [][]string{{"mcp", "install"}, {"mcp", "install", "--help"}, {"mcp", "install", "codex", "--help"}, {"mcp", "install", "claude", "--help"}} {
		if _, _, err := execute(installerRoot(t, run), args...); err != nil {
			t.Fatal("installer help not lazy", err)
		}
	}
	for _, extra := range [][]string{{"--listen", "0.0.0.0:9090"}, {"--listen", "localhost:9090"}, {"--scope", "local"}, {"--timeout", "invalid"}, {"private-argument"}} {
		if _, _, err := execute(installerRoot(t, run), append([]string{"mcp", "install", "codex"}, extra...)...); err == nil || strings.Contains(err.Error(), "private") {
			t.Fatal("invalid input accepted or echoed", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root := installerRoot(t, run)
	root.SetArgs([]string{"mcp", "install", "claude"})
	if err := root.ExecuteContext(ctx); err != context.Canceled {
		t.Fatal("pre-canceled installer executed client", err)
	}
}

func TestMCPInstallClaudeScopesAndCanceledPreflight(t *testing.T) {
	for _, scope := range []string{"local", "project"} {
		run := func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if args[4] != scope || !strings.Contains(args[6], "http://[::1]:9090/mcp") {
				t.Error("Claude scope or IPv6 endpoint changed")
			}
			return nil, nil
		}
		if _, _, err := execute(installerRoot(t, run), "mcp", "install", "claude", "--scope", scope, "--listen", "[::1]:9090"); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	run := func(context.Context, string, ...string) ([]byte, error) { calls++; cancel(); return []byte(`[]`), nil }
	root := installerRoot(t, run)
	root.SetArgs([]string{"mcp", "install", "codex"})
	if err := root.ExecuteContext(ctx); err != context.Canceled || calls != 1 {
		t.Fatal("canceled preflight still registered client", err)
	}
}
