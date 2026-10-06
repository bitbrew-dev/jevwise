package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/config"
	"github.com/bitbrew-dev/jevwise/internal/mcpserver"
	"github.com/bitbrew-dev/jevwise/internal/service"
	"github.com/spf13/cobra"
)

func mcpRoot(factory ServiceFactory, run mcpRunner) *cobra.Command {
	root := NewRootWithFactory(factory)
	for _, cmd := range root.Commands() {
		if cmd.Name() == "mcp" {
			root.RemoveCommand(cmd)
		}
	}
	root.AddCommand(newMCP(factory, run))
	return root
}

type mcpFixture struct{ t *testing.T }

func (f *mcpFixture) Decide(context.Context, service.Request) (service.Response, error) {
	f.t.Error("CLI lifecycle must not make decisions")
	return service.Response{}, nil
}

func mcpFixtureService(t *testing.T) service.DecisionService {
	t.Helper()
	return &mcpFixture{t}
}

func TestMCPHelpIsLazyAndRegistered(t *testing.T) {
	decisionEnvironment(t)
	t.Setenv("JEV_MCP_TOKEN", "")
	factory := func(config.Config) (service.DecisionService, func(), error) {
		t.Fatal("help initialized decision service")
		return nil, nil, nil
	}
	for _, args := range [][]string{{"mcp", "--help", "--config", "/nonexistent"}, {"help", "mcp"}} {
		out, stderr, err := execute(NewRootWithFactory(factory), args...)
		if err != nil || stderr != "" || !strings.Contains(out, "--listen") || !strings.Contains(out, "JEV_MCP_TOKEN") {
			t.Fatal("MCP help failed", err)
		}
	}
}

func TestMCPInjectedRunnerAndCleanup(t *testing.T) {
	decisionEnvironment(t)
	t.Setenv("JEV_MCP_TOKEN", "private-agent-token")
	created, cleaned, ran := 0, 0, 0
	svc := mcpFixtureService(t)
	factory := func(cfg config.Config) (service.DecisionService, func(), error) {
		created++
		if cfg.Model != "fixture-model" || cfg.APIKey != "private-upstream-key" || cfg.Timeout != time.Millisecond {
			t.Error("inherited configuration changed")
		}
		return svc, func() { cleaned++ }, nil
	}
	run := func(ctx context.Context, current service.DecisionService, timeout time.Duration, options mcpserver.RunOptions) error {
		ran++
		if current != svc || timeout != time.Millisecond || options.Address != "127.0.0.1:8080" || options.Token != "private-agent-token" {
			t.Error("runner configuration changed")
		}
		if _, ok := ctx.Deadline(); ok {
			t.Error("decision timeout became a server lifetime deadline")
		}
		time.Sleep(5 * time.Millisecond)
		return options.Ready(ctx, "http://127.0.0.1:8080/mcp")
	}
	out, stderr, err := execute(mcpRoot(factory, run), "mcp", "--model", "fixture-model", "--api-key", "private-upstream-key", "--timeout", "1ms")
	if err != nil || stderr != "" || out != "http://127.0.0.1:8080/mcp\n" || created != 1 || cleaned != 1 || ran != 1 {
		t.Fatal("unexpected lifecycle or output", err, created, cleaned, ran)
	}
}

func TestMCPValidationBeforeFactory(t *testing.T) {
	decisionEnvironment(t)
	factory := func(config.Config) (service.DecisionService, func(), error) {
		t.Fatal("invalid invocation initialized service")
		return nil, nil, nil
	}
	run := func(context.Context, service.DecisionService, time.Duration, mcpserver.RunOptions) error {
		t.Fatal("invalid invocation started runner")
		return nil
	}
	for _, token := range []string{"", "private token", "private\nvalue", "private-秘密", strings.Repeat("p", 4097)} {
		t.Setenv("JEV_MCP_TOKEN", token)
		out, stderr, err := execute(mcpRoot(factory, run), "mcp")
		if err == nil || out != "" || stderr != "" || strings.Contains(err.Error(), "private") {
			t.Fatal("unsafe token validation", err)
		}
	}
	t.Setenv("JEV_MCP_TOKEN", "fixture-token")
	for _, args := range [][]string{
		{"mcp", "private-input"}, {"mcp", "--listen", "private-address"},
		{"mcp", "--listen", "localhost:8080"}, {"mcp", "--listen", "0.0.0.0:8080"},
		{"mcp", "--config", "/nonexistent"}, {"mcp", "--timeout", "invalid"},
		{"mcp", "--api-key", "fixture-token"},
	} {
		out, stderr, err := execute(mcpRoot(factory, run), args...)
		if err == nil || out != "" || stderr != "" || strings.Contains(err.Error(), "private") {
			t.Fatal("unsafe argument validation", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := mcpRoot(factory, run)
	cmd.SetArgs([]string{"mcp"})
	if err := cmd.ExecuteContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled invocation started", err)
	}
}

func TestMCPFailuresAreSafeAndCleanUpOnce(t *testing.T) {
	decisionEnvironment(t)
	t.Setenv("JEV_MCP_TOKEN", "fixture-token")
	cause := errors.New("private-underlying-error")
	for _, kind := range []string{"factory", "nil", "placeholder", "runner", "writer", "short-writer", "canceled-factory", "canceled-ready"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cleaned, ran := 0, 0
			factory := func(config.Config) (service.DecisionService, func(), error) {
				cleanup := func() { cleaned++ }
				switch kind {
				case "factory":
					return nil, cleanup, cause
				case "nil":
					return nil, cleanup, nil
				case "placeholder":
					return nil, cleanup, service.ErrNotImplemented
				case "canceled-factory":
					cancel()
				}
				return mcpFixtureService(t), cleanup, nil
			}
			run := func(ctx context.Context, _ service.DecisionService, _ time.Duration, options mcpserver.RunOptions) error {
				ran++
				if kind == "runner" {
					return cause
				}
				if kind == "canceled-ready" {
					cancel()
				}
				return options.Ready(ctx, "http://127.0.0.1:8080/mcp")
			}
			cmd := mcpRoot(factory, run)
			var out, stderr bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&stderr)
			cmd.SetArgs([]string{"mcp"})
			if kind == "writer" {
				cmd.SetOut(secretIO{})
			}
			if kind == "short-writer" {
				cmd.SetOut(versionWriter{short: true})
			}
			err := cmd.ExecuteContext(ctx)
			if err == nil || strings.Contains(err.Error(), "private") || out.Len() != 0 || stderr.Len() != 0 || cleaned != 1 {
				t.Fatal("unsafe failure or cleanup", err, cleaned)
			}
			if (kind == "factory" || kind == "runner") && !errors.Is(err, cause) {
				t.Fatal("inspectable error cause lost")
			}
			if kind == "short-writer" && !errors.Is(err, io.ErrShortWrite) {
				t.Fatal("short write cause lost")
			}
			wantRuns := 0
			if kind == "runner" || kind == "writer" || kind == "short-writer" || kind == "canceled-ready" {
				wantRuns = 1
			}
			if ran != wantRuns {
				t.Fatal("runner initialized unexpectedly", ran)
			}
		})
	}
}
