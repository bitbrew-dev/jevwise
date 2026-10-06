package cli

import (
	"context"
	"errors"
	"net/http/httptest"
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

func backgroundRoot(t *testing.T, start mcpStarter) *cobra.Command {
	t.Helper()
	factory := func(config.Config) (service.DecisionService, func(), error) {
		t.Fatal("background parent initialized a decision service")
		return nil, nil, nil
	}
	run := func(context.Context, service.DecisionService, time.Duration, mcpserver.RunOptions) error {
		t.Fatal("background parent ran foreground server")
		return nil
	}
	root := NewRootWithFactory(factory)
	for _, cmd := range root.Commands() {
		if cmd.Name() == "mcp" {
			root.RemoveCommand(cmd)
		}
	}
	root.AddCommand(newMCPWithBackground(factory, run, start))
	return root
}

func noBackgroundStart(t *testing.T) mcpStarter {
	return func(context.Context, string, daemon.Bootstrap, func(daemon.Bootstrap) error, func(context.Context, bool) error) (daemon.StartResult, error) {
		t.Fatal("unexpected background launch")
		return daemon.StartResult{}, nil
	}
}

func TestBackgroundBootstrapAndVerifiedReadiness(t *testing.T) {
	decisionEnvironment(t)
	t.Setenv("JEV_MCP_TOKEN", "private-agent-token")
	dir := filepath.Join(t.TempDir(), "runtime")
	server := httptest.NewUnstartedServer(nil)
	address := server.Listener.Addr().String()
	started := 0
	start := func(ctx context.Context, executable string, boot daemon.Bootstrap, validate func(daemon.Bootstrap) error, ready func(context.Context, bool) error) (daemon.StartResult, error) {
		started++
		if executable != "" || validate(boot) != nil || boot.RuntimeDir != dir || boot.AgentToken != "private-agent-token" || boot.Config.APIKey != "private-upstream-key" {
			t.Error("bootstrap or fixed executable ownership changed")
		}
		management, err := daemon.NewManagement(address, boot.Instance, boot.ControlToken, func() {})
		if err != nil {
			t.Fatal(err)
		}
		server.Config.Handler = management.Handler()
		server.Start()
		if ready(ctx, false) == nil {
			t.Error("unpublished prepared instance was acknowledged")
		}
		store, err := daemon.OpenStore(dir)
		if err != nil {
			t.Fatal(err)
		}
		lease, err := store.Acquire()
		if err != nil {
			t.Fatal(err)
		}
		publication, err := daemon.Publish(store, daemon.State{Schema: 1, Address: address, Version: "dev", Instance: boot.Instance}, boot.ControlToken)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { server.Close(); _ = publication.Close(); _ = lease.Close(); _ = store.Close() })
		if err := ready(ctx, false); err != nil || ready(ctx, true) == nil {
			t.Error("prepared phase was not verified", err)
		}
		if err := management.MarkRunning(); err != nil {
			t.Fatal(err)
		}
		if err := ready(ctx, true); err != nil {
			t.Error("running phase was not verified", err)
		}
		return daemon.StartResult{Started: true}, nil
	}
	out, stderr, err := execute(backgroundRoot(t, start), "mcp", "--background", "--listen", address, "--runtime-dir", dir, "--api-key", "private-upstream-key")
	if err != nil || stderr != "" || out != "http://"+address+"/mcp\n" || started != 1 {
		t.Fatal("background startup reporting failed", err)
	}
}

func TestBackgroundInvalidConfigurationHasNoRuntimeEffects(t *testing.T) {
	decisionEnvironment(t)
	t.Setenv("JEV_MCP_TOKEN", "fixture-agent-token")
	dir := filepath.Join(t.TempDir(), "must-not-exist")
	for _, extra := range [][]string{
		nil, {"--provider", "codex", "--api-key", "fixture-key"}, {"--provider", "claude", "--api-key", "fixture-key"},
		{"--api-key", "fixture-agent-token"}, {"--api-key", "fixture-key", "--timeout", "0s"},
		{"--api-key", "fixture-key", "--timeout", time.Duration(1<<63 - 1).String()},
		{"--api-key", "fixture-key", "--base-url", "https://private-key@fixture.test"},
	} {
		args := append([]string{"mcp", "--background", "--runtime-dir", dir}, extra...)
		out, stderr, err := execute(backgroundRoot(t, noBackgroundStart(t)), args...)
		if err == nil || out != "" || stderr != "" || strings.Contains(err.Error(), "private") {
			t.Fatal("unsafe invalid background configuration", err)
		}
		if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("invalid configuration created runtime storage")
		}
	}
}

func existingBackground(t *testing.T, phase string) (string, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "runtime")
	server := httptest.NewUnstartedServer(nil)
	address := server.Listener.Addr().String()
	instance, token := strings.Repeat("a", 32), strings.Repeat("b", 64)
	management, err := daemon.NewManagement(address, instance, token, func() {})
	if err != nil {
		t.Fatal(err)
	}
	server.Config.Handler = management.Handler()
	server.Start()
	if phase == "running" {
		_ = management.MarkRunning()
	}
	if phase == "stopping" {
		controller, _ := daemon.NewController(address, instance, token)
		if _, err := controller.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	store, err := daemon.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := store.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	publication, err := daemon.Publish(store, daemon.State{Schema: 1, Address: address, Version: "dev", Instance: instance}, token)
	if err != nil {
		t.Fatal(err)
	}
	if phase == "stale" {
		server.Close()
	}
	t.Cleanup(func() { server.Close(); _ = publication.Close(); _ = lease.Close(); _ = store.Close() })
	return dir, address
}

func TestBackgroundExistingOwnedInstanceNeverLaunches(t *testing.T) {
	decisionEnvironment(t)
	t.Setenv("JEV_MCP_TOKEN", "fixture-agent-token")
	for _, phase := range []string{"prepared", "running", "stopping", "stale"} {
		t.Run(phase, func(t *testing.T) {
			dir, address := existingBackground(t, phase)
			before, keyBefore, readErr := readBackgroundInstance(dir)
			if readErr != nil {
				t.Fatal(readErr)
			}
			out, stderr, err := execute(backgroundRoot(t, noBackgroundStart(t)), "mcp", "--background", "--runtime-dir", dir, "--api-key", "fixture-key")
			if phase == "running" {
				if err != nil || out != "http://"+address+"/mcp\n" {
					t.Fatal("existing authenticated instance was not reported", err)
				}
			} else if err == nil || out != "" {
				t.Fatal("unsafe existing instance was adopted")
			}
			if phase == "prepared" {
				if err.Error() != "MCP background instance is preparing; wait and check jev mcp status" {
					t.Fatalf("prepared instance = %v", err)
				}
				controller, err := daemon.NewController(address, before.Instance, keyBefore)
				if err != nil {
					t.Fatal(err)
				}
				status, err := controller.Status(context.Background())
				if err != nil || status.State != "prepared" {
					t.Fatal("prepared instance was acknowledged or stopped", err)
				}
			}
			after, keyAfter, readErr := readBackgroundInstance(dir)
			if stderr != "" || readErr != nil || after != before || keyAfter != keyBefore {
				t.Fatal("existing state was modified or leaked", readErr)
			}
		})
	}
}

func TestBackgroundPartialStateIsPreservedWithoutLaunch(t *testing.T) {
	decisionEnvironment(t)
	t.Setenv("JEV_MCP_TOKEN", "fixture-agent-token")
	dir := filepath.Join(t.TempDir(), "runtime")
	store, err := daemon.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Create("state.json", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	out, stderr, err := execute(backgroundRoot(t, noBackgroundStart(t)), "mcp", "--background", "--runtime-dir", dir, "--api-key", "fixture-key")
	data, readErr := store.Read("state.json", 4096)
	if err == nil || out != "" || stderr != "" || readErr != nil || string(data) != "{}" {
		t.Fatal("partial state was adopted, changed or leaked", err)
	}
}

func TestBackgroundUncertainStartAndOutputFailures(t *testing.T) {
	decisionEnvironment(t)
	t.Setenv("JEV_MCP_TOKEN", "fixture-agent-token")
	for _, kind := range []string{"before", "after", "unconfirmed", "shortwrite"} {
		start := func(context.Context, string, daemon.Bootstrap, func(daemon.Bootstrap) error, func(context.Context, bool) error) (daemon.StartResult, error) {
			if kind == "unconfirmed" {
				return daemon.StartResult{}, nil
			}
			if kind == "shortwrite" {
				return daemon.StartResult{Started: true}, nil
			}
			return daemon.StartResult{Started: kind == "after"}, errors.New("private-start-error")
		}
		cmd := backgroundRoot(t, start)
		dir := filepath.Join(t.TempDir(), "runtime")
		cmd.SetArgs([]string{"mcp", "--background", "--runtime-dir", dir, "--api-key", "fixture-key"})
		cmd.SetOut(versionWriter{short: true})
		err := cmd.ExecuteContext(context.Background())
		if err == nil || strings.Contains(err.Error(), "private") || ((kind == "after" || kind == "shortwrite") && !strings.Contains(err.Error(), "may be running")) {
			t.Fatal("unsafe or misleading startup failure", err)
		}
		if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("read-only startup precheck created runtime storage")
		}
	}
}
