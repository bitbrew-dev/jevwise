package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"time"
)

type agentCommand func(context.Context, string, ...string) ([]byte, error)

const agentOutputLimit = 1 << 20

type agentOutput struct {
	data []byte
	full bool
}

func (w *agentOutput) Write(p []byte) (int, error) {
	n := min(len(p), agentOutputLimit-len(w.data))
	w.data = append(w.data, p[:n]...)
	w.full = w.full || n != len(p)
	return len(p), nil
}

// Native clients own their settings edits. No shell, raw diagnostics or inherited
// credentials; only platform paths and client configuration locations are passed.
func runAgentCommand(ctx context.Context, client string, args ...string) ([]byte, error) {
	if client != "codex" && client != "claude" {
		return nil, errors.New("unsupported MCP client")
	}
	process := exec.CommandContext(ctx, client, args...)
	process.WaitDelay = 250 * time.Millisecond
	process.Env = agentEnvironment()
	var output agentOutput
	process.Stdout, process.Stderr = &output, io.Discard
	if err := process.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	if output.full {
		return nil, errors.New("client output exceeds limit")
	}
	return output.data, nil
}

func agentEnvironment() []string {
	var env []string
	for _, name := range []string{"PATH", "PATHEXT", "HOME", "USERPROFILE", "SystemRoot", "WINDIR", "LOCALAPPDATA", "APPDATA", "TEMP", "TMP", "TMPDIR", "CODEX_HOME", "CLAUDE_CONFIG_DIR", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME"} {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	return append(env, "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "DISABLE_AUTOUPDATER=1", "DISABLE_TELEMETRY=1", "DISABLE_ERROR_REPORTING=1")
}
