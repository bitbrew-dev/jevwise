package daemon

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/debuglog"
)

// ChildArgument is the fixed hidden command. Credentials never appear in argv.
const ChildArgument = "__mcp-child"
const launchTimeout = 20 * time.Second
const startupCleanupGrace = 7 * time.Second

// StartResult distinguishes pre-ACK failure from an independently started child.
// Started may be true with an error if post-ACK confirmation fails. Do not retry
// blindly: inspect authenticated daemon status before another launch.
type StartResult struct{ Started bool }

// Start launches the current executable, unless an absolute trusted executable
// is supplied by the caller. Readiness must authenticate boot.Instance using
// boot.ControlToken; running=false checks preparation, true checks ACK promotion.
// Readiness must honor its bounded context. Acknowledged children are never
// killed when this context expires.
func Start(ctx context.Context, executable string, boot Bootstrap, validate func(Bootstrap) error, readiness func(context.Context, bool) error) (_ StartResult, resultErr error) {
	finish := debuglog.Trace(ctx, "mcp.background.start")
	defer func() { finish(resultErr) }()
	fail := errors.New("background startup failed: inspect authenticated status")
	if ctx == nil || readiness == nil {
		return StartResult{}, fail
	}
	var frame bytes.Buffer
	if WriteBootstrap(&frame, boot, validate) != nil {
		return StartResult{}, fail
	}
	if executable == "" {
		var err error
		executable, err = os.Executable()
		if err != nil {
			return StartResult{}, fail
		}
	}
	if !filepath.IsAbs(executable) {
		return StartResult{}, fail
	}
	startup, cancel := context.WithTimeout(ctx, launchTimeout)
	defer cancel()
	if startup.Err() != nil {
		return StartResult{}, fail
	}
	cmd := exec.Command(executable, ChildArgument)
	cmd.Env = childEnvironment()
	if detach(cmd) != nil {
		return StartResult{}, fail
	}
	input, output, err := os.Pipe()
	if err != nil {
		return StartResult{}, fail
	}
	defer input.Close()
	defer output.Close()
	cmd.Stdin = input
	if err := cmd.Start(); err != nil {
		return StartResult{}, fail
	}
	_ = input.Close()
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	acknowledged := false
	defer func() {
		if !acknowledged {
			_ = output.Close()
			// EOF gives the child time to drain calls and release owned state.
			grace := time.NewTimer(startupCleanupGrace)
			defer grace.Stop()
			select {
			case <-done:
				return
			case <-grace.C:
			}
			_ = cmd.Process.Kill() // Only this freshly launched process handle.
			select {
			case <-done:
			case <-time.After(2 * time.Second):
			}
		}
	}()
	stopClose := context.AfterFunc(startup, func() { _ = output.Close() })
	defer stopClose()
	if n, err := output.Write(frame.Bytes()); err != nil || n != frame.Len() {
		return StartResult{}, fail
	}
	debuglog.Event(ctx, "mcp.background.wait.prepared")
	if waitReady(startup, done, readiness, false) != nil {
		return StartResult{}, fail
	}
	debuglog.Event(ctx, "mcp.background.acknowledge")
	n, err := output.Write([]byte{acknowledgement})
	acknowledged = n == 1
	_ = output.Close()
	if err != nil || !acknowledged || waitReady(startup, done, readiness, true) != nil {
		return StartResult{Started: acknowledged}, fail
	}
	return StartResult{Started: true}, nil
}

func waitReady(ctx context.Context, done <-chan struct{}, ready func(context.Context, bool) error, running bool) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
			return errors.New("child exited")
		default:
		}
		if err := ready(ctx, running); err == nil {
			select {
			case <-done:
				return errors.New("child exited")
			default:
				return ctx.Err()
			}
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-done:
			timer.Stop()
			return errors.New("child exited")
		case <-timer.C:
		}
	}
}

func childEnvironment() []string {
	var result []string
	for _, entry := range os.Environ() {
		key := strings.ToUpper(strings.SplitN(entry, "=", 2)[0])
		if !strings.HasPrefix(key, "TS_JEV_") && !strings.HasPrefix(key, "TYPESAFE_") && !strings.HasPrefix(key, "JEV_MCP_") {
			result = append(result, entry)
		}
	}
	return result
}
