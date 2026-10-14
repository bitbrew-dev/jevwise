package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAgentCommandEnvironmentAndOutputAreBounded(t *testing.T) {
	for _, name := range []string{"JEV_API_KEY", "JEVWISE_MCP_TOKEN", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "MCP_CLIENT_SECRET", "UNRELATED_SECRET"} {
		t.Setenv(name, "private-secret")
	}
	t.Setenv("CODEX_HOME", "client-config-fixture")
	env := strings.Join(agentEnvironment(), "\n")
	if strings.Contains(env, "private") || !strings.Contains(env, "CODEX_HOME=client-config-fixture") || !strings.Contains(env, "DISABLE_AUTOUPDATER=1") {
		t.Fatal("registration child inherited credentials or lost configuration location")
	}
	var output agentOutput
	data := []byte(strings.Repeat("x", agentOutputLimit+1))
	if n, err := output.Write(data); err != nil || n != len(data) || len(output.data) != agentOutputLimit || !output.full {
		t.Fatal("client output was not bounded")
	}
	_, _ = output.Write(data)
	if len(output.data) != agentOutputLimit {
		t.Fatal("overflowing output kept growing")
	}
	if _, err := runAgentCommand(context.Background(), "unsupported-client"); err == nil {
		t.Fatal("unsupported client executable was launched")
	}
}

// Only the task-owned test binary, installed under a temporary client name,
// enters this fixture. No installed agent or user configuration is accessed.
func runAgentCommandFixture() int {
	switch os.Args[2] {
	case "timeout":
		time.Sleep(time.Hour)
	case "large":
		fmt.Print(strings.Repeat("x", agentOutputLimit+1))
	default:
		for _, name := range []string{"JEV_API_KEY", "JEVWISE_MCP_TOKEN", "OPENAI_API_KEY", "ANTHROPIC_API_KEY"} {
			if os.Getenv(name) != "" {
				return 2
			}
		}
		if os.Getenv("CODEX_HOME") != "isolated-client-fixture" {
			return 3
		}
		fmt.Fprintln(os.Stderr, "private-client-diagnostic")
		fmt.Print("clean")
	}
	return 0
}

func TestAgentCommandRunsOnlyIsolatedFixtureWithDeadline(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	name := "codex"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	if err := os.Link(binary, path); err != nil {
		data, err := os.ReadFile(binary)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	t.Setenv("CODEX_HOME", "isolated-client-fixture")
	for _, key := range []string{"JEV_API_KEY", "JEVWISE_MCP_TOKEN", "OPENAI_API_KEY", "ANTHROPIC_API_KEY"} {
		t.Setenv(key, "private-fixture-key")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	data, err := runAgentCommand(ctx, "codex", "__jevwise-client-fixture", "clean")
	if err != nil || string(data) != "clean" {
		t.Fatal("child inherited credentials or diagnostics", err)
	}
	if data, err := runAgentCommand(ctx, "codex", "__jevwise-client-fixture", "large"); err == nil || data != nil {
		t.Fatal("oversized subprocess output was accepted")
	}
	ctx, stop := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer stop()
	started := time.Now()
	if _, err := runAgentCommand(ctx, "codex", "__jevwise-client-fixture", "timeout"); !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 5*time.Second {
		t.Fatal("owned client process ignored deadline", err)
	}
}
