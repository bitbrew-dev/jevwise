package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/update"
)

type updateWrite func([]byte) (int, error)

func (w updateWrite) Write(b []byte) (int, error) { return w(b) }

func TestUpdateInstallationStagesAndPublicationState(t *testing.T) {
	t.Setenv("TS_JEV_TIMEOUT", "invalid")
	t.Setenv("TS_JEV_PROVIDER", "invalid")
	cause := errors.New("private-path-or-credential")
	for _, kind := range []string{"success", "check", "checkfalse", "current", "newer", "norelease", "dev", "windows", "arch", "lookup-error", "lookup-deadline", "download-deadline", "replace-deadline", "published-deadline", "invalid-latest", "download-error", "nil-binary", "replace-error", "no-install", "cleanup-error", "writer-error", "short-write", "before-cancel", "lookup-cancel", "download-cancel", "replace-cancel", "published-cancel", "writer-cancel"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			current, tag := "v1.0.0", "v1.2.0"
			calls := [3]int{}
			var operationCtx context.Context
			ops := updateOps{goos: "linux", goarch: "amd64"}
			ops.latest = func(ctx context.Context) (update.Release, error) {
				calls[0]++
				operationCtx = ctx
				if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 10*time.Second {
					t.Error("default operation timeout missing")
				}
				if kind == "lookup-deadline" {
					return update.Release{}, context.DeadlineExceeded
				}
				if kind == "lookup-cancel" {
					cancel()
				}
				if kind == "lookup-error" {
					return update.Release{}, cause
				}
				if kind == "norelease" {
					return update.Release{}, update.ErrNoRelease
				}
				return update.Release{Tag: tag}, nil
			}
			ops.download = func(ctx context.Context, release update.Release, goos, goarch string) (*update.Binary, error) {
				calls[1]++
				if kind == "download-deadline" {
					return nil, context.DeadlineExceeded
				}
				if ctx != operationCtx || release.Tag != tag || goos != "linux" || goarch != "amd64" {
					t.Error("download arguments or shared context changed")
				}
				if kind == "download-cancel" {
					cancel()
				}
				if kind == "download-error" {
					return nil, cause
				}
				if kind == "nil-binary" {
					return nil, nil
				}
				return &update.Binary{}, nil
			}
			ops.replace = func(ctx context.Context, binary *update.Binary, version string) (update.InstallResult, error) {
				calls[2]++
				if kind == "replace-deadline" || kind == "published-deadline" {
					return update.InstallResult{Installed: kind == "published-deadline"}, context.DeadlineExceeded
				}
				if ctx != operationCtx || binary == nil || version != current {
					t.Error("replacement arguments or shared context changed")
				}
				if kind == "replace-cancel" || kind == "published-cancel" {
					cancel()
				}
				if kind == "replace-error" {
					return update.InstallResult{}, cause
				}
				if kind == "no-install" || kind == "replace-cancel" {
					return update.InstallResult{}, nil
				}
				if kind == "cleanup-error" {
					return update.InstallResult{Installed: true}, cause
				}
				return update.InstallResult{Installed: true}, nil
			}
			args := []string{"update"}
			switch kind {
			case "check":
				current, ops.goos = "dev", "windows"
				args = append(args, "--check")
			case "checkfalse":
				args = append(args, "--check=false")
			case "current":
				current = tag
			case "newer":
				current = "v2.0.0"
			case "dev":
				current = "private-current"
			case "windows":
				ops.goos = "windows"
			case "arch":
				ops.goarch = "private-arch"
			case "invalid-latest":
				tag = "private-tag"
			case "before-cancel":
				cancel()
			}
			cmd := updateRoot(nil, current, ops)
			var output bytes.Buffer
			cmd.SetOut(&output)
			if kind == "writer-cancel" {
				cmd.SetOut(updateWrite(func(b []byte) (int, error) { cancel(); return output.Write(b) }))
			}
			if kind == "writer-error" {
				cmd.SetOut(versionWriter{err: cause})
			}
			if kind == "short-write" {
				cmd.SetOut(versionWriter{short: true})
			}
			cmd.SetArgs(args)
			err := cmd.ExecuteContext(ctx)
			wantCalls := [3]int{1, 1, 1}
			success := false
			switch kind {
			case "dev", "windows", "arch", "before-cancel":
				wantCalls = [3]int{}
			case "check", "current", "newer", "norelease":
				wantCalls = [3]int{1, 0, 0}
				success = true
			case "lookup-error", "invalid-latest", "lookup-cancel", "lookup-deadline":
				wantCalls = [3]int{1, 0, 0}
			case "download-error", "nil-binary", "download-cancel", "download-deadline":
				wantCalls = [3]int{1, 1, 0}
			case "success", "checkfalse":
				success = true
			}
			if calls != wantCalls || (err == nil) != success {
				t.Fatal("unexpected stages/result", calls, err)
			}
			if err != nil && (output.Len() != 0 && kind != "writer-cancel" || strings.Contains(err.Error(), "private")) {
				t.Fatal("failure output leaked", err)
			}
			if strings.HasSuffix(kind, "deadline") && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("deadline cause lost", err)
			}
			if strings.HasSuffix(kind, "cancel") && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
			if strings.HasSuffix(kind, "error") && !errors.Is(err, cause) {
				t.Fatal("stage/writer cause lost", err)
			}
			if kind == "short-write" && !errors.Is(err, io.ErrShortWrite) {
				t.Fatal("short write cause lost", err)
			}
			installedError := kind == "cleanup-error" || kind == "writer-error" || kind == "short-write" || kind == "published-cancel" || kind == "published-deadline" || kind == "writer-cancel"
			if err != nil && strings.Contains(err.Error(), "installed; restart required") != installedError {
				t.Fatal("publication state misreported", err)
			}
			if kind == "success" || kind == "checkfalse" {
				if output.String() != "Installed Jev v1.0.0 -> v1.2.0; restart required\n" {
					t.Fatal("installation confirmation wrong", output.String())
				}
			}
		})
	}
}

func TestUpdateInstallationDeadlineAndNilOperations(t *testing.T) {
	for _, kind := range []string{"deadline", "bad-timeout", "nil-lookup", "nil-download", "nil-replace"} {
		t.Run(kind, func(t *testing.T) {
			forbidden := func(context.Context) (update.Release, error) {
				t.Fatal("invalid setup dispatched lookup")
				return update.Release{}, nil
			}
			ops := updateOps{latest: forbidden, goos: "darwin", goarch: "arm64",
				download: func(context.Context, update.Release, string, string) (*update.Binary, error) {
					t.Fatal("unexpected download")
					return nil, nil
				},
				replace: func(context.Context, *update.Binary, string) (update.InstallResult, error) {
					t.Fatal("unexpected replace")
					return update.InstallResult{}, nil
				}}
			ctx := context.Background()
			if kind == "deadline" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Hour))
				defer cancel()
			}
			if kind == "nil-lookup" {
				ops.latest = nil
			}
			if kind == "nil-download" {
				ops.download = nil
			}
			if kind == "nil-replace" {
				ops.replace = nil
			}
			cmd := updateRoot(nil, "v1.0.0", ops)
			var output bytes.Buffer
			cmd.SetOut(&output)
			if kind == "bad-timeout" {
				cmd.SetArgs([]string{"update", "--timeout", "0s"})
			} else {
				cmd.SetArgs([]string{"update"})
			}
			err := cmd.ExecuteContext(ctx)
			if err == nil || output.Len() != 0 || kind == "deadline" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("invalid setup accepted", err)
			}
		})
	}
}
