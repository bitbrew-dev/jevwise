//go:build linux || darwin || windows

package cli

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/bitbrew-dev/jevwise/internal/config"
	"github.com/bitbrew-dev/jevwise/internal/daemon"
	"github.com/bitbrew-dev/jevwise/internal/service"
)

func TestMCPChildFailedInitializationAndPreACKEOFReleaseOwnership(t *testing.T) {
	for _, kind := range []string{"factory", "nil", "typed-nil", "EOF"} {
		t.Run(kind, func(t *testing.T) {
			boot := childBootstrap(t)
			var frame bytes.Buffer
			if err := daemon.WriteBootstrap(&frame, boot, daemon.ValidateBootstrap); err != nil {
				t.Fatal(err)
			}
			cleaned := 0
			factory := func(config.Config) (service.DecisionService, func(), error) {
				cleanup := func() { cleaned++ }
				if kind == "factory" {
					return nil, cleanup, errors.New("private-factory-error")
				}
				if kind == "typed-nil" {
					return (*childNilService)(nil), cleanup, nil
				}
				if kind == "nil" {
					return nil, cleanup, nil
				}
				return &childNilService{}, cleanup, nil
			}
			cmd := NewRootWithFactory(factory)
			cmd.SetIn(io.NopCloser(bytes.NewReader(frame.Bytes())))
			out, stderr, err := execute(cmd, daemon.ChildArgument)
			if err == nil || strings.Contains(err.Error(), "private") || out != "" || stderr != "" || cleaned != 1 {
				t.Fatal("child failure leaked or did not clean its service", err)
			}
			store, err := daemon.OpenStore(boot.RuntimeDir)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			if _, _, err := daemon.Read(store); !errors.Is(err, daemon.ErrAbsent) {
				t.Fatal("failed child left published state", err)
			}
			lease, err := store.Acquire()
			if err != nil {
				t.Fatal("failed child left ownership", err)
			}
			_ = lease.Close()
		})
	}
}
