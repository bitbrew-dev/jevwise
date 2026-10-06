package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain makes the test binary an owned fake child, never a real daemon.
func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == ChildArgument {
		os.Exit(fakeChild())
	}
	os.Exit(m.Run())
}

func launchFixture() Bootstrap {
	return Bootstrap{Address: "normal", AgentToken: "agent-secret", Instance: "instance", ControlToken: "control-secret"}
}

func validateLaunch(b Bootstrap) error {
	if b.AgentToken != "agent-secret" || b.RuntimeDir == "" {
		return errors.New("secret validation detail")
	}
	return nil
}

func fakeChild() int {
	if os.Getenv("JEVWISE_TEST_NO_READ") != "" {
		time.Sleep(3 * time.Second)
		return 8
	}
	boot, startup, err := ReadBootstrap(context.Background(), os.Stdin, time.Second, validateLaunch)
	if err != nil {
		return 1
	}
	defer startup.Close()
	if os.Getenv("TYPESAFE_API_KEY") != "" || strings.Contains(strings.Join(os.Args, " "), "secret") {
		return 2
	}
	if boot.Address == "exit" {
		return 3
	}
	if os.WriteFile(filepath.Join(boot.RuntimeDir, "owned-lock"), nil, 0600) != nil {
		return 4
	}
	defer os.Remove(filepath.Join(boot.RuntimeDir, "owned-lock"))
	if os.WriteFile(filepath.Join(boot.RuntimeDir, "prepared"), nil, 0600) != nil {
		return 4
	}
	defer os.Remove(filepath.Join(boot.RuntimeDir, "prepared"))
	if startup.AwaitAcknowledgement() != nil {
		return 5
	}
	if os.WriteFile(filepath.Join(boot.RuntimeDir, "running"), nil, 0600) != nil {
		return 6
	}
	defer os.WriteFile(filepath.Join(boot.RuntimeDir, "exited"), nil, 0600)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(boot.RuntimeDir, "stop")); err == nil {
			return 0
		}
		time.Sleep(5 * time.Millisecond)
	}
	return 7
}

func fixtureReadiness(directory string) func(context.Context, bool) error {
	return func(_ context.Context, running bool) error {
		name := "prepared"
		if running {
			name = "running"
		}
		_, err := os.Stat(filepath.Join(directory, name))
		return err
	}
}

func stopFixture(t *testing.T, directory string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, "stop"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(directory, "exited")); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("owned fake child did not exit")
}

func TestStartDetachedAndIndependent(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "env-secret")
	boot := launchFixture()
	boot.RuntimeDir = t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	result, err := Start(ctx, "", boot, validateLaunch, fixtureReadiness(boot.RuntimeDir))
	cancel()
	if err != nil || !result.Started {
		t.Fatalf("start failed: %v", err)
	}
	defer stopFixture(t, boot.RuntimeDir)
	if _, err := os.Stat(filepath.Join(boot.RuntimeDir, "running")); err != nil {
		t.Fatal("child did not promote after ACK")
	}
}

func TestStartPreACKFailureAndCancellation(t *testing.T) {
	for _, mode := range []string{"exit", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			boot := launchFixture()
			boot.Address = mode
			boot.RuntimeDir = t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ready := fixtureReadiness(boot.RuntimeDir)
			prepared := false
			if mode == "cancel" {
				ready = func(ctx context.Context, running bool) error {
					if fixtureReadiness(boot.RuntimeDir)(ctx, running) == nil {
						prepared = true
						cancel()
					}
					return errors.New("secret readiness detail")
				}
			}
			result, err := Start(ctx, "", boot, validateLaunch, ready)
			if err == nil || result.Started || strings.Contains(err.Error(), "secret") {
				t.Fatalf("unsafe failure: %+v %v", result, err)
			}
			if _, err := os.Stat(filepath.Join(boot.RuntimeDir, "running")); err == nil {
				t.Fatal("failed startup was acknowledged")
			}
			if mode == "cancel" && !prepared {
				t.Fatal("test never observed prepared child ownership")
			}
			for _, name := range []string{"owned-lock", "prepared"} {
				if _, err := os.Stat(filepath.Join(boot.RuntimeDir, name)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("pre-ACK failure skipped child-owned cleanup")
				}
			}
		})
	}
}

func TestStartPostACKFailureKeepsChild(t *testing.T) {
	boot := launchFixture()
	boot.RuntimeDir = t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ready := fixtureReadiness(boot.RuntimeDir)
	result, err := Start(ctx, "", boot, validateLaunch, func(ctx context.Context, running bool) error {
		if running {
			if ready(ctx, true) == nil {
				cancel() // ACK promotion was observed before confirmation fails.
			}
			return errors.New("secret reporting detail")
		}
		return ready(ctx, false)
	})
	if err == nil || !result.Started || strings.Contains(err.Error(), "secret") {
		t.Fatalf("incorrect post-ACK result: %+v %v", result, err)
	}
	defer stopFixture(t, boot.RuntimeDir)
	if _, err := os.Stat(filepath.Join(boot.RuntimeDir, "running")); err != nil {
		t.Fatal("acknowledged child was killed")
	}
}

func TestStartInvalidArguments(t *testing.T) {
	boot := launchFixture()
	boot.RuntimeDir = t.TempDir()
	for _, executable := range []string{"relative", filepath.Join(boot.RuntimeDir, "missing-executable")} {
		if result, err := Start(context.Background(), executable, boot, validateLaunch, fixtureReadiness(boot.RuntimeDir)); err == nil || result.Started {
			t.Fatal("invalid executable accepted")
		}
	}
	if _, err := Start(nil, "", boot, validateLaunch, nil); err == nil {
		t.Fatal("nil arguments accepted")
	}
}

func TestStartCancellationWithoutFrameConsumer(t *testing.T) {
	t.Setenv("JEVWISE_TEST_NO_READ", "1")
	boot := launchFixture()
	boot.RuntimeDir = t.TempDir()
	payload, err := json.Marshal(boot)
	if err != nil {
		t.Fatal(err)
	}
	boot.Config.APIKey = strings.Repeat("x", bootstrapLimit-len(payload))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	result, err := Start(ctx, "", boot, validateLaunch, fixtureReadiness(boot.RuntimeDir))
	if err == nil || result.Started {
		t.Fatal("unconsumed bootstrap survived cancellation")
	}
}
