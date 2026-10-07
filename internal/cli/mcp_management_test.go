//go:build linux || darwin || windows

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bitbrew-dev/jevwise/internal/config"
	"github.com/bitbrew-dev/jevwise/internal/daemon"
	"github.com/bitbrew-dev/jevwise/internal/service"
	"github.com/spf13/cobra"
)

func managementRoot(t *testing.T) *cobra.Command {
	t.Helper()
	return NewRootWithFactory(func(config.Config) (service.DecisionService, func(), error) {
		t.Error("management initialized a decision service")
		return nil, nil, errors.New("private-backend-error")
	})
}

func managementState(t *testing.T, directory, address string, pid ...int) {
	t.Helper()
	store, err := daemon.OpenStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	state := daemon.State{Schema: 1, Address: address, Version: "dev", Instance: strings.Repeat("i", 32)}
	if len(pid) != 0 {
		state.Schema, state.PID = 2, pid[0]
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Create("state.json", data); err != nil {
		t.Fatal(err)
	}
	if err := store.Create("control.key", []byte(strings.Repeat("k", 32))); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func lazyManagementArgs(action, directory string) []string {
	return []string{"mcp", action, "--runtime-dir", directory, "--config", "/missing-private-config", "--token-file", "/missing-agent-token", "--timeout", "invalid-timeout", "--provider", "invalid-provider"}
}

func TestMCPManagementAbsentAndHelpAreLazy(t *testing.T) {
	t.Setenv("JEV_MCP_TOKEN", "invalid\nagent-token")
	directory := filepath.Join(t.TempDir(), "absent")
	for _, action := range []string{"status", "stop"} {
		out, _, err := execute(managementRoot(t), lazyManagementArgs(action, directory)...)
		if err != nil || out != "MCP background instance is not running\n" {
			t.Fatalf("absent %s = %q, %v", action, out, err)
		}
		args := append(lazyManagementArgs(action, directory), "--help")
		out, _, err = execute(managementRoot(t), args...)
		if err != nil || !strings.Contains(out, "--runtime-dir") {
			t.Fatalf("help %s = %q, %v", action, out, err)
		}
	}
	if _, err := os.Lstat(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("management created runtime directory")
	}
	store, err := daemon.OpenStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	out, _, err := execute(managementRoot(t), "mcp", "status", "--runtime-dir", directory)
	if err != nil || out != "MCP background instance is not running\n" {
		t.Fatal("empty private runtime not distinguished", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatal("management acquired ownership or created state", err)
	}
}

func TestMCPManagementVerifiedPhasesAndStop(t *testing.T) {
	for _, phase := range []string{"prepared", "running", "stopping"} {
		t.Run(phase, func(t *testing.T) {
			for _, action := range []string{"status", "stop"} {
				server := httptest.NewUnstartedServer(nil)
				address := server.Listener.Addr().String()
				var stops atomic.Int32
				management, err := daemon.NewManagement(address, strings.Repeat("i", 32), strings.Repeat("k", 32), func() { stops.Add(1) })
				if err != nil {
					t.Fatal(err)
				}
				server.Config.Handler = management.Handler()
				server.Start()
				defer server.Close()
				if phase == "running" {
					if err := management.MarkRunning(); err != nil {
						t.Fatal(err)
					}
				}
				if phase == "stopping" {
					controller, err := daemon.NewController(address, strings.Repeat("i", 32), strings.Repeat("k", 32))
					if err != nil {
						t.Fatal(err)
					}
					if _, err := controller.Stop(context.Background()); err != nil {
						t.Fatal(err)
					}
				}
				directory := filepath.Join(t.TempDir(), "runtime")
				managementState(t, directory, address)
				out, _, err := execute(managementRoot(t), lazyManagementArgs(action, directory+string(os.PathSeparator)+".")...)
				server.Close()
				want := "MCP background instance is " + phase + "\n"
				if action == "stop" {
					want = "MCP background stop acknowledged\n"
				}
				if err != nil || out != want {
					t.Fatalf("%s = %q, %v", action, out, err)
				}
				wantStops := int32(0)
				if phase == "stopping" || action == "stop" {
					wantStops = 1
				}
				if stops.Load() != wantStops {
					t.Fatalf("stop cancellation count = %d, want %d", stops.Load(), wantStops)
				}
				entries, err := os.ReadDir(directory)
				if err != nil || len(entries) != 2 {
					t.Fatal("management modified state or acquired ownership", err)
				}
			}
		})
	}
}

func TestMCPManagementPIDIsInspectionOnlyAfterAuthentication(t *testing.T) {
	for _, authenticated := range []bool{true, false} {
		server := httptest.NewUnstartedServer(nil)
		address := server.Listener.Addr().String()
		key := strings.Repeat("k", 32)
		if !authenticated {
			key = strings.Repeat("x", 32)
		}
		var stops atomic.Int32
		management, err := daemon.NewManagement(address, strings.Repeat("i", 32), key, func() { stops.Add(1) })
		if err != nil {
			t.Fatal(err)
		}
		if err := management.MarkRunning(); err != nil {
			t.Fatal(err)
		}
		server.Config.Handler = management.Handler()
		server.Start()
		defer server.Close()
		directory := filepath.Join(t.TempDir(), "runtime")
		// Deliberately unrelated to the HTTP server's actual PID: this value
		// must only be printed, never establish identity or authorize a stop.
		managementState(t, directory, address, 424242)
		before, err := os.ReadFile(filepath.Join(directory, "state.json"))
		if err != nil {
			t.Fatal(err)
		}
		for _, action := range []string{"status", "stop"} {
			out, _, err := execute(managementRoot(t), lazyManagementArgs(action, directory)...)
			if !authenticated {
				if err == nil || out != "" || stops.Load() != 0 {
					t.Fatal("unverified state disclosed a PID or authorized stop", err)
				}
				continue
			}
			want := "MCP background instance is running\nPID: 424242 (inspection only)\n"
			if action == "stop" {
				want = "MCP background stop acknowledged\n"
			}
			if err != nil || out != want {
				t.Fatalf("PID %s = %q, %v", action, out, err)
			}
		}
		after, err := os.ReadFile(filepath.Join(directory, "state.json"))
		if err != nil || !bytes.Equal(before, after) || (authenticated && stops.Load() != 1) {
			t.Fatal("PID metadata changed management authority or state", err)
		}
	}
}

func TestMCPManagementRefusesPartialAndStaleState(t *testing.T) {
	for _, kind := range []string{"partial", "stale", "wrong instance"} {
		t.Run(kind, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "runtime")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(daemon.ManagementStatus{Instance: strings.Repeat("x", 32), State: "running"})
			}))
			address := strings.TrimPrefix(server.URL, "http://")
			managementState(t, directory, address)
			if kind == "stale" {
				server.Close()
			} else {
				defer server.Close()
			}
			if kind == "partial" {
				if err := os.Remove(filepath.Join(directory, "state.json")); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(filepath.Join(directory, "control.key"))
			if err != nil {
				t.Fatal(err)
			}
			for _, action := range []string{"status", "stop"} {
				out, _, err := execute(managementRoot(t), "mcp", action, "--runtime-dir", directory)
				if err == nil || err.Error() != "cannot verify MCP background instance; inspect the runtime directory manually" || out != "" {
					t.Fatalf("%s %s = %q, %v", kind, action, out, err)
				}
			}
			after, err := os.ReadFile(filepath.Join(directory, "control.key"))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("management cleaned up unfamiliar state", err)
			}
		})
	}
}

func TestMCPManagementOutputAndCanceledContext(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "absent")
	for _, writer := range []io.Writer{managementWriter(func([]byte) (int, error) { return 0, nil }), managementWriter(func([]byte) (int, error) { return 0, errors.New("private-output-error") })} {
		root := managementRoot(t)
		root.SetOut(writer)
		root.SetArgs([]string{"mcp", "status", "--runtime-dir", directory})
		if err := root.Execute(); err == nil || err.Error() != "cannot write MCP background status" {
			t.Fatalf("writer error = %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root := managementRoot(t)
	root.SetArgs([]string{"mcp", "stop", "--runtime-dir", directory})
	if err := root.ExecuteContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled management = %v", err)
	}
	root = managementRoot(t)
	root.SetArgs([]string{"mcp", "status", "--runtime-dir", ""})
	if err := root.Execute(); err == nil || err.Error() != "invalid MCP runtime directory" {
		t.Fatalf("empty path = %v", err)
	}
}

type managementWriter func([]byte) (int, error)

func (w managementWriter) Write(data []byte) (int, error) { return w(data) }

func TestMCPManagementUnsafeLeafAndStopOutput(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "runtime-file")
	if err := os.WriteFile(directory, []byte("private-invalid-state"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _, err := execute(managementRoot(t), "mcp", "status", "--runtime-dir", directory)
	if err == nil || err.Error() != "cannot verify MCP background instance; inspect the runtime directory manually" || out != "" {
		t.Fatalf("unsafe leaf = %q, %v", out, err)
	}
	if data, err := os.ReadFile(directory); err != nil || string(data) != "private-invalid-state" {
		t.Fatal("unsafe state was changed", err)
	}
	cause := errors.New("private-output-cause")
	cmd := &cobra.Command{}
	cmd.SetOut(managementWriter(func([]byte) (int, error) { return 0, cause }))
	if err := writeMCPManagement(cmd, "MCP background stop acknowledged", true); err == nil || err.Error() != "MCP stop acknowledged but output failed" || !errors.Is(err, cause) {
		t.Fatalf("stop output failure = %v", err)
	}
}
