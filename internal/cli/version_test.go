package cli

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bitbrew-dev/jevwise/internal/buildinfo"
	"github.com/bitbrew-dev/jevwise/internal/config"
	"github.com/bitbrew-dev/jevwise/internal/service"
)

func TestVersionFormsAreIdenticalAndIndependent(t *testing.T) {
	t.Setenv("TS_JEV_API_KEY", "private-secret")
	before := http.DefaultTransport
	http.DefaultTransport = forbiddenTransport{t}
	t.Cleanup(func() { http.DefaultTransport = before })
	for _, form := range []string{"version", "--version"} {
		cmd := NewRootWithFactory(func(config.Config) (service.DecisionService, func(), error) {
			t.Fatal("version created service")
			return nil, nil, nil
		})
		out, stderr, err := execute(cmd, form, "--config", "/missing", "--timeout", "invalid", "--provider", "invalid")
		if err != nil || stderr != "" || out != buildinfo.String() || !strings.HasPrefix(out, "jevwise dev\ncommit: unknown\nbuilt: unknown\n") {
			t.Fatalf("version output incorrect: %q %q %v", out, stderr, err)
		}
	}
}

type versionWriter struct {
	err   error
	short bool
}

func (w versionWriter) Write(data []byte) (int, error) {
	if w.short {
		return len(data) - 1, nil
	}
	return 0, w.err
}

func TestVersionSafeWriterArgumentsAndCancellation(t *testing.T) {
	cause := errors.New("private-writer-content")
	for _, form := range []string{"version", "--version"} {
		for _, writer := range []versionWriter{{err: cause}, {short: true}} {
			cmd := NewRoot()
			cmd.SetOut(writer)
			cmd.SetArgs([]string{form})
			err := cmd.Execute()
			want := cause
			if writer.short {
				want = io.ErrShortWrite
			}
			if !errors.Is(err, want) || strings.Contains(err.Error(), "private-writer-content") {
				t.Fatal("writer cause unsafe", err)
			}
		}
		cmd := NewRoot()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		cmd.SetArgs([]string{form})
		cmd.SetOut(versionWriter{err: cause})
		if err := cmd.ExecuteContext(ctx); !errors.Is(err, context.Canceled) {
			t.Fatal("cancelled version wrote output", err)
		}
		if out, _, err := execute(NewRoot(), form, "private-argument"); err == nil || out != "" || strings.Contains(err.Error(), "private-argument") {
			t.Fatal("argument leaked", err)
		}
	}
}
