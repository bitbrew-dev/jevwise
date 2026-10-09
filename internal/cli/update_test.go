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
	"github.com/bitbrew-dev/jevwise/internal/service"
	"github.com/bitbrew-dev/jevwise/internal/update"
	"github.com/spf13/cobra"
)

func updateRoot(latest func(context.Context) (update.Release, error), current string, installs ...updateOps) *cobra.Command {
	root := NewRootWithFactory(func(config.Config) (service.DecisionService, func(), error) {
		panic("release checks must not create decision services")
	})
	for _, cmd := range root.Commands() {
		if cmd.Name() == "update" {
			root.RemoveCommand(cmd)
		}
	}
	ops := updateOps{latest: latest}
	if len(installs) != 0 {
		ops = installs[0]
	}
	root.AddCommand(newUpdate(ops, current))
	return root
}

func TestUpdateComparisonAndUnknownCurrent(t *testing.T) {
	for _, tc := range []struct{ current, latest, want string }{
		{"v1.0.0", "v1.2.0", "Update available: v1.0.0 -> v1.2.0\n"},
		{"v1.2.0", "v1.2.0", "Already current: v1.2.0\n"},
		{"v2.0.0", "v1.2.0", "Installed version v2.0.0 is newer than latest v1.2.0; no downgrade.\n"},
		{"dev", "v1.2.0", "current version unknown"}, {"unknown", "v1.2.0", "current version unknown"}, {"private-current", "v1.2.0", "current version unknown"},
	} {
		cmd := updateRoot(func(context.Context) (update.Release, error) { return update.Release{Tag: tc.latest}, nil }, tc.current)
		out, stderr, err := execute(cmd, "update", "--check")
		if err != nil || stderr != "" || !strings.Contains(out, tc.want) || strings.Contains(out, "private-current") {
			t.Fatal("incorrect release output", out, err)
		}
	}
}

func TestUpdateTimeoutAndConfigurationIndependence(t *testing.T) {
	t.Setenv("TS_JEV_TIMEOUT", "invalid")
	t.Setenv("TS_JEV_PROVIDER", "private-provider")
	for _, timeout := range []string{"", "2s"} {
		want := 10 * time.Second
		if timeout != "" {
			want = 2 * time.Second
		}
		cmd := updateRoot(func(ctx context.Context) (update.Release, error) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > want || time.Until(deadline) < want-time.Second {
				t.Error("wrong lookup timeout")
			}
			return update.Release{Tag: "v1.0.0"}, nil
		}, "dev")
		args := []string{"update", "--check", "--config", "/missing", "--api-key", "", "--provider", "invalid"}
		if timeout != "" {
			args = append(args, "--timeout", timeout)
		}
		if _, _, err := execute(cmd, args...); err != nil {
			t.Fatal("decision config was loaded", err)
		}
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Hour))
	defer cancel()
	deadline, _ := ctx.Deadline()
	cmd := updateRoot(func(actual context.Context) (update.Release, error) {
		if got, _ := actual.Deadline(); !got.Equal(deadline) {
			t.Error("caller deadline changed")
		}
		return update.Release{}, update.ErrNoRelease
	}, "dev")
	cmd.SetArgs([]string{"update", "--check", "--timeout", "2h"})
	cmd.SetOut(io.Discard)
	if err := cmd.ExecuteContext(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateInvalidInputsAndHelpAvoidLookup(t *testing.T) {
	forbidden := func(context.Context) (update.Release, error) {
		t.Fatal("invalid/help command looked up release")
		return update.Release{}, nil
	}
	for _, args := range [][]string{{"update"}, {"update", "--check=false"}, {"update", "private-argument"}, {"update", "--check", "--timeout", "invalid"}, {"update", "--check", "--timeout", "0s"}, {"update", "--check", "--timeout", "-1s"}} {
		if out, _, err := execute(updateRoot(forbidden, "dev"), args...); err == nil || out != "" || strings.Contains(err.Error(), "private-argument") {
			t.Fatal("invalid command unsafe", err)
		}
	}
	if out, _, err := execute(updateRoot(forbidden, "dev"), "update", "--help", "--config", "/missing"); err != nil || !strings.Contains(out, "--check") {
		t.Fatal("help failed", err)
	}
	if _, _, err := execute(updateRoot(nil, "dev"), "update", "--check"); err == nil {
		t.Fatal("nil lookup accepted")
	}
	if out, _, err := execute(NewRoot(), "update"); err == nil || out != "" || !strings.Contains(err.Error(), "stable versioned build") {
		t.Fatal("default update fetched or did not explain limitation", err)
	}
}

func TestUpdateNoReleaseAndSafeErrors(t *testing.T) {
	cause := errors.New("private-lookup-content")
	for _, lookupError := range []error{update.ErrNoRelease, &decisionError{"private-lookup-content", update.ErrNoRelease}, cause} {
		cmd := updateRoot(func(context.Context) (update.Release, error) { return update.Release{}, lookupError }, "dev")
		out, _, err := execute(cmd, "update", "--check")
		if errors.Is(lookupError, update.ErrNoRelease) {
			if err != nil || out != "No stable Jevwise release is published yet.\n" {
				t.Fatal("missing release not informational", err)
			}
		} else if !errors.Is(err, cause) || out != "" || strings.Contains(err.Error(), "private-lookup-content") {
			t.Fatal("lookup failure leaked or lost cause", err)
		}
	}
	for _, tag := range []string{"", "private-tag", "v1.0.0-rc.1", "v01.0.0"} {
		cmd := updateRoot(func(context.Context) (update.Release, error) { return update.Release{Tag: tag}, nil }, "dev")
		if out, _, err := execute(cmd, "update", "--check"); err == nil || out != "" || strings.Contains(err.Error(), "private-tag") {
			t.Fatal("invalid latest accepted or leaked", err)
		}
	}
	for _, writer := range []versionWriter{{err: cause}, {short: true}} {
		cmd := updateRoot(func(context.Context) (update.Release, error) { return update.Release{Tag: "v1.0.0"}, nil }, "dev")
		cmd.SetOut(writer)
		cmd.SetArgs([]string{"update", "--check"})
		err := cmd.Execute()
		want := cause
		if writer.short {
			want = io.ErrShortWrite
		}
		if !errors.Is(err, want) || strings.Contains(err.Error(), "private-lookup-content") {
			t.Fatal("writer failure unsafe", err)
		}
	}
}

func TestUpdateCancellationBeforeAndAfterLookup(t *testing.T) {
	for _, after := range []bool{false, true} {
		for _, lookupError := range []error{nil, update.ErrNoRelease, errors.New("private-content")} {
			ctx, cancel := context.WithCancel(context.Background())
			if !after {
				cancel()
			}
			calls := 0
			cmd := updateRoot(func(context.Context) (update.Release, error) {
				calls++
				cancel()
				return update.Release{Tag: "v1.0.0"}, lookupError
			}, "dev")
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetArgs([]string{"update", "--check"})
			err := cmd.ExecuteContext(ctx)
			cancel()
			if !errors.Is(err, context.Canceled) || out.Len() != 0 || !after && calls != 0 || after && calls != 1 {
				t.Fatal("lookup cancellation ignored", err)
			}
		}
	}
}

func TestUpdateRegisteredHelpAndExpiredCaller(t *testing.T) {
	root := NewRootWithFactory(func(config.Config) (service.DecisionService, func(), error) {
		t.Fatal("registered update help created service")
		return nil, nil, nil
	})
	out, stderr, err := execute(root, "update", "--help", "--config", "/missing")
	if err != nil || stderr != "" || !strings.Contains(out, "--check") {
		t.Fatal("registered update help failed", err)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
	defer cancel()
	cmd := updateRoot(func(context.Context) (update.Release, error) {
		t.Fatal("expired caller dispatched lookup")
		return update.Release{}, nil
	}, "dev")
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"update", "--check"})
	if err := cmd.ExecuteContext(ctx); !errors.Is(err, context.DeadlineExceeded) || output.Len() != 0 {
		t.Fatal("expired deadline ignored", err)
	}
}
