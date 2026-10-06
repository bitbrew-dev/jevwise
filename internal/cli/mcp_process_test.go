//go:build linux || darwin || windows

package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
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

// Only the task-owned test binary gets this synthetic child service. Production
// CLI registration, bootstrap, privacy, HTTP and cleanup remain real code paths.
func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == daemon.ChildArgument && os.Getenv("JEVWISE_PROCESS_FIXTURE") == "1" {
		os.Exit(runProcessFixture())
	}
	os.Exit(m.Run())
}

func runProcessFixture() (code int) {
	exitMarker := os.Getenv("JEVWISE_PROCESS_EXIT")
	defer func() { _ = os.WriteFile(exitMarker, []byte("exited"), 0600) }()
	factory := func(cfg config.Config) (service.DecisionService, func(), error) {
		if cfg.Model != "fixture" || cfg.BaseURL != "http://127.0.0.1:1" {
			return nil, nil, errors.New("invalid fixture configuration")
		}
		return decideFunc(func(context.Context, service.Request) (service.Response, error) {
			return service.Response{Model: "fixture", Choice: "a", Confidence: .8, Probabilities: map[string]float64{"a": .8, "b": .2}}, nil
		}), func() {}, nil
	}
	cmd := NewRootWithFactory(factory)
	cmd.SetIn(os.Stdin)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{daemon.ChildArgument})
	if cmd.ExecuteContext(context.Background()) != nil {
		return 1
	}
	return 0
}

func processReadiness(controller *daemon.Controller) func(context.Context, bool) error {
	return func(ctx context.Context, running bool) error {
		status, err := controller.Status(ctx)
		if err != nil {
			return err
		}
		want := "prepared"
		if running {
			want = "running"
		}
		if status.State != want {
			return errors.New("fixture phase mismatch")
		}
		return nil
	}
}

func waitProcessCondition(t *testing.T, condition func() bool, message string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-ctx.Done():
			t.Error(message)
			return
		case <-ticker.C:
		}
	}
}

func processExited(marker string) bool {
	data, err := os.ReadFile(marker)
	return err == nil && string(data) == "exited"
}

func runtimeEmpty(directory string) bool {
	entries, err := os.ReadDir(directory)
	return errors.Is(err, os.ErrNotExist) || (err == nil && len(entries) == 0)
}

func processEnvironment(t *testing.T, marker string) {
	t.Helper()
	t.Setenv("JEVWISE_PROCESS_FIXTURE", "1")
	t.Setenv("JEVWISE_PROCESS_EXIT", marker)
}

func TestMCPActualDetachedChildLifecycle(t *testing.T) {
	boot := childBootstrap(t)
	boot.AgentToken = strings.Repeat("c", 64)
	if err := daemon.ValidateBootstrap(boot); err != nil {
		t.Fatal(err)
	}
	exitMarker := filepath.Join(t.TempDir(), "original-exited")
	processEnvironment(t, exitMarker)
	controller, err := daemon.NewController(boot.Address, boot.Instance, boot.ControlToken)
	if err != nil {
		t.Fatal(err)
	}
	// Registered before launch so every post-ACK test failure still uses the
	// exact authenticated instance to stop its child. Never a PID from disk.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = controller.Stop(ctx)
		waitProcessCondition(t, func() bool { return processExited(exitMarker) && runtimeEmpty(boot.RuntimeDir) }, "owned child or runtime state survived cleanup")
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	result, err := daemon.Start(ctx, "", boot, daemon.ValidateBootstrap, processReadiness(controller))
	cancel() // Closing parent startup lifetime and stdin must not stop the child.
	if err != nil || !result.Started {
		t.Fatal("actual detached child did not start", err)
	}
	status, err := controller.Status(context.Background())
	if err != nil || status.State != "running" || status.Instance != boot.Instance {
		t.Fatal("child did not survive parent startup completion", err)
	}
	data := childToolCall(t, boot)
	if !bytes.Contains(data, []byte(`"confidence":0.8`)) || !bytes.Contains(data, []byte(`"probabilities":{"a":0.8,"b":0.2}`)) {
		t.Fatal("actual child did not return synthetic decision probabilities")
	}
	store, err := daemon.OpenExistingStore(boot.RuntimeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	state, key, err := daemon.Read(store)
	if err != nil || state.Instance != boot.Instance || key != boot.ControlToken {
		t.Fatal("actual child did not publish verified state", err)
	}
	stateJSON, err := store.Read("state.json", 4096)
	if err != nil || bytes.Contains(stateJSON, []byte(boot.Config.APIKey)) || bytes.Contains(stateJSON, []byte(boot.AgentToken)) || bytes.Contains(stateJSON, []byte(boot.ControlToken)) {
		t.Fatal("public state contains credentials", err)
	}
	agentController, err := daemon.NewController(boot.Address, boot.Instance, boot.AgentToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agentController.Stop(context.Background()); err == nil {
		t.Fatal("agent bearer authorized management stop")
	}
	for _, kind := range []string{"same-runtime", "occupied-port"} {
		t.Run(kind, func(t *testing.T) {
			duplicate := boot
			duplicate.Instance, duplicate.ControlToken, err = daemon.NewIdentity()
			if err != nil {
				t.Fatal(err)
			}
			if kind == "occupied-port" {
				duplicate.RuntimeDir = filepath.Join(t.TempDir(), "runtime")
			}
			marker := filepath.Join(t.TempDir(), "duplicate-exited")
			processEnvironment(t, marker)
			other, err := daemon.NewController(duplicate.Address, duplicate.Instance, duplicate.ControlToken)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			result, err := daemon.Start(ctx, "", duplicate, daemon.ValidateBootstrap, processReadiness(other))
			if err == nil || result.Started {
				t.Fatal("duplicate runtime or occupied listener was adopted")
			}
			waitProcessCondition(t, func() bool { return processExited(marker) }, "duplicate child was not reaped")
			current, currentKey, err := daemon.Read(store)
			if err != nil || current != state || currentKey != key {
				t.Fatal("duplicate startup changed original instance state", err)
			}
			status, err := controller.Status(context.Background())
			if err != nil || status.State != "running" {
				t.Fatal("duplicate startup stopped original instance", err)
			}
			if kind == "occupied-port" && !runtimeEmpty(duplicate.RuntimeDir) {
				t.Fatal("failed bind left child-owned state or lock")
			}
		})
	}
	if _, err := controller.Stop(context.Background()); err != nil {
		t.Fatal("authenticated stop failed", err)
	}
	waitProcessCondition(t, func() bool { return processExited(exitMarker) && runtimeEmpty(boot.RuntimeDir) }, "stop did not clean state, key and lock")
}

func TestMCPActualChildPreACKEOFCleansOwnedState(t *testing.T) {
	boot := childBootstrap(t)
	marker := filepath.Join(t.TempDir(), "unacknowledged-exited")
	processEnvironment(t, marker)
	controller, err := daemon.NewController(boot.Address, boot.Instance, boot.ControlToken)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = controller.Stop(ctx)
		waitProcessCondition(t, func() bool { return processExited(marker) && runtimeEmpty(boot.RuntimeDir) }, "unacknowledged child survived cleanup")
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	prepared := false
	result, err := daemon.Start(ctx, "", boot, daemon.ValidateBootstrap, func(ctx context.Context, running bool) error {
		status, err := controller.Status(ctx)
		if err == nil && status.State == "prepared" && !running {
			prepared = true
			cancel() // Forces parent stdin EOF before ACK, not an arbitrary PID kill.
		}
		return errors.New("fixture intentionally refuses acknowledgement")
	})
	if err == nil || result.Started || !prepared {
		t.Fatal("fixture did not exercise a prepared pre-ACK startup failure")
	}
	waitProcessCondition(t, func() bool { return processExited(marker) && runtimeEmpty(boot.RuntimeDir) }, "pre-ACK EOF did not release state, key and lease")
}

func TestMCPActualBackgroundCLIStartStatusDuplicateAndStop(t *testing.T) {
	fixture := childBootstrap(t)
	marker := filepath.Join(t.TempDir(), "background-cli-exited")
	processEnvironment(t, marker)
	t.Setenv("JEV_MCP_TOKEN", fixture.AgentToken)
	t.Setenv("TS_JEV_API_KEY", fixture.Config.APIKey)
	configPath := filepath.Join(t.TempDir(), "empty.toml")
	if err := os.WriteFile(configPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	var captured daemon.Bootstrap
	var started, parentCreated int
	factory := func(config.Config) (service.DecisionService, func(), error) {
		parentCreated++
		return nil, nil, errors.New("parent must not initialize a background service")
	}
	start := func(ctx context.Context, executable string, boot daemon.Bootstrap, validate func(daemon.Bootstrap) error, ready func(context.Context, bool) error) (daemon.StartResult, error) {
		started++
		if started == 1 {
			captured = boot
		}
		return daemon.Start(ctx, executable, boot, validate, ready)
	}
	t.Cleanup(func() {
		if captured.Instance == "" {
			return
		}
		controller, err := daemon.NewController(captured.Address, captured.Instance, captured.ControlToken)
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, _ = controller.Stop(ctx)
		}
		waitProcessCondition(t, func() bool { return processExited(marker) && runtimeEmpty(fixture.RuntimeDir) }, "background CLI child survived cleanup")
	})
	execute := func(args ...string) (string, error) {
		root := NewRootWithFactory(factory)
		for _, command := range root.Commands() {
			if command.Name() == "mcp" {
				root.RemoveCommand(command)
			}
		}
		root.AddCommand(newMCPWithBackground(factory, mcpserver.Run, start))
		var output, stderr bytes.Buffer
		root.SetOut(&output)
		root.SetErr(&stderr)
		root.SetArgs(args)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := root.ExecuteContext(ctx)
		if stderr.Len() != 0 {
			t.Error("background lifecycle unexpectedly wrote stderr")
		}
		return output.String(), err
	}
	args := []string{"mcp", "--background", "--listen", fixture.Address, "--runtime-dir", fixture.RuntimeDir,
		"--config", configPath, "--model", "fixture", "--base-url", fixture.Config.BaseURL, "--timeout", "100ms"}
	output, err := execute(args...)
	wantEndpoint := "http://" + fixture.Address + "/mcp\n"
	if err != nil || output != wantEndpoint || started != 1 || captured.Config.APIKey != fixture.Config.APIKey {
		t.Fatal("actual background CLI did not resolve and launch bootstrap", err)
	}
	output, err = execute(args...)
	if err != nil || output != wantEndpoint || started != 1 {
		t.Fatal("duplicate background CLI started another child", err)
	}
	managementArgs := []string{"--runtime-dir", fixture.RuntimeDir, "--config", filepath.Join(t.TempDir(), "missing.toml"),
		"--token-file", filepath.Join(t.TempDir(), "missing-token")}
	output, err = execute(append([]string{"mcp", "status"}, managementArgs...)...)
	if err != nil || output != "MCP background instance is running\n" {
		t.Fatal("status loaded invalid config or missing agent token file", err)
	}
	if data := childToolCall(t, captured); !bytes.Contains(data, []byte(`"choice":"a"`)) {
		t.Fatal("background CLI child did not serve the synthetic MCP decision")
	}
	output, err = execute(append([]string{"mcp", "stop"}, managementArgs...)...)
	if err != nil || output != "MCP background stop acknowledged\n" || parentCreated != 0 {
		t.Fatal("stop loaded service config or initialized a parent service", err)
	}
	waitProcessCondition(t, func() bool { return processExited(marker) && runtimeEmpty(fixture.RuntimeDir) }, "background CLI stop did not release runtime ownership")
}
