package cli

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"reflect"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/buildinfo"
	"github.com/bitbrew-dev/jevwise/internal/daemon"
	"github.com/bitbrew-dev/jevwise/internal/mcpserver"
	"github.com/bitbrew-dev/jevwise/internal/service"
	"github.com/spf13/cobra"
)

func newMCPChild(factory ServiceFactory) *cobra.Command {
	return &cobra.Command{Use: daemon.ChildArgument, Hidden: true,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 0 {
				return errors.New("background child does not accept positional arguments")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			input, ok := cmd.InOrStdin().(io.ReadCloser)
			if !ok {
				return errors.New("background child requires an owned bootstrap pipe")
			}
			boot, startup, err := daemon.ReadBootstrap(cmd.Context(), input, 20*time.Second, daemon.ValidateBootstrap)
			if err != nil {
				return err
			}
			defer startup.Close()
			return runMCPChild(startup, boot, factory)
		}}
}

func runMCPChild(startup *daemon.Startup, boot daemon.Bootstrap, factory ServiceFactory) (err error) {
	lifetime, cancel := context.WithCancel(startup.Context())
	stopInputCancellation := context.AfterFunc(lifetime, startup.Close)
	defer stopInputCancellation()
	var store *daemon.Store
	var lease *daemon.Lease
	var publication *daemon.Publication
	var cleanup func()
	var listener net.Listener
	defer func() {
		// Cancellation always precedes releasing service or ownership resources.
		cancel()
		if listener != nil {
			_ = listener.Close()
		}
		if cleanup != nil {
			cleanup()
		}
		err = errors.Join(err, publication.Close(), lease.Close(), store.Close())
		if err != nil {
			err = &decisionError{"MCP background child failed", err}
		}
	}()
	store, err = daemon.OpenStore(boot.RuntimeDir)
	if err != nil {
		return err
	}
	lease, err = store.Acquire()
	if err != nil {
		return err
	}
	if _, _, readErr := daemon.Read(store); !errors.Is(readErr, daemon.ErrAbsent) {
		return errors.New("existing MCP state requires manual inspection")
	}
	var lc net.ListenConfig
	listener, err = lc.Listen(lifetime, "tcp", boot.Address)
	if err != nil {
		return err
	}
	management, err := daemon.NewManagement(listener.Addr().String(), boot.Instance, boot.ControlToken, cancel)
	if err != nil {
		return err
	}
	svc, cleanup, err := factory(boot.Config)
	if err != nil || !initializedMCPService(svc) {
		return errors.Join(err, errors.New("cannot initialize background decision service"))
	}
	if err := lifetime.Err(); err != nil {
		return err
	}
	return mcpserver.Serve(lifetime, listener, readyMCPService{svc, management}, boot.Config.Timeout,
		mcpserver.RunOptions{Token: boot.AgentToken, Management: management.Handler(),
			Ready: func(ctx context.Context, _ string) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				publication, err = daemon.Publish(store, daemon.State{Schema: 2, Address: listener.Addr().String(), Version: buildinfo.Version, Instance: boot.Instance, PID: os.Getpid()}, boot.ControlToken)
				if err != nil {
					return err
				}
				if err := startup.AwaitAcknowledgement(); err != nil {
					return err
				}
				return management.MarkRunning()
			}})
}

func initializedMCPService(svc service.DecisionService) bool {
	if svc == nil {
		return false
	}
	value := reflect.ValueOf(svc)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return !value.IsNil()
	default:
		return true
	}
}

type readyMCPService struct {
	service    service.DecisionService
	management *daemon.Management
}

func (s readyMCPService) Decide(ctx context.Context, request service.Request) (service.Response, error) {
	if err := ctx.Err(); err != nil {
		return service.Response{}, err
	}
	if !s.management.IsRunning() {
		return service.Response{}, errors.New("background decision service is not ready")
	}
	return s.service.Decide(ctx, request)
}
