package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/buildinfo"
	"github.com/bitbrew-dev/jevwise/internal/config"
	"github.com/bitbrew-dev/jevwise/internal/daemon"
	"github.com/bitbrew-dev/jevwise/internal/debuglog"
	"github.com/bitbrew-dev/jevwise/internal/mcpserver"
	"github.com/bitbrew-dev/jevwise/internal/service"
	"github.com/spf13/cobra"
)

type mcpStarter func(context.Context, string, daemon.Bootstrap, func(daemon.Bootstrap) error, func(context.Context, bool) error) (daemon.StartResult, error)

func runMCPBackground(cmd *cobra.Command, cfg config.Config, address, agentToken, runtimeDir string, start mcpStarter) error {
	if cfg.Provider != "jev" {
		return &decisionError{"provider is not implemented", service.ErrNotImplemented}
	}
	if cfg.Validate() != nil || cfg.Timeout > time.Duration(1<<63-1)-30*time.Second || mcpserver.ValidateToken(agentToken) != nil {
		return errors.New("invalid MCP background configuration")
	}
	if cfg.APIKey == "" {
		return errors.New("API key is required for MCP background mode")
	}
	path, err := filepath.Abs(runtimeDir)
	if err != nil || runtimeDir == "" {
		return errors.New("invalid MCP runtime directory")
	}
	instance, controlToken, err := daemon.NewIdentity()
	if err != nil {
		return err
	}
	boot := daemon.Bootstrap{Config: cfg, Address: address, AgentToken: agentToken, RuntimeDir: path, Instance: instance, ControlToken: controlToken}
	if err := daemon.ValidateBootstrap(boot); err != nil {
		return err
	}
	debuglog.Event(cmd.Context(), "mcp.background.inspect")
	state, token, err := readBackgroundInstance(path)
	if err == nil {
		controller, err := daemon.NewController(state.Address, state.Instance, token)
		if err != nil {
			return mcpInspectionError(err)
		}
		status, err := controller.Status(cmd.Context())
		if err != nil {
			return mcpInspectionError(err)
		}
		switch status.State {
		case "running":
			return writeBackgroundEndpoint(cmd, state.Address)
		case "prepared":
			return errors.New("MCP background instance is preparing; wait and check " + buildinfo.CommandName + " mcp status")
		case "stopping":
			return errors.New("MCP background instance is stopping; wait before starting again")
		default:
			return mcpInspectionError(errors.New("unrecognized management phase"))
		}
	}
	// Only this exact sentinel means every read and close completed safely.
	if err != daemon.ErrAbsent {
		return mcpInspectionError(err)
	}
	controller, err := daemon.NewController(boot.Address, boot.Instance, boot.ControlToken)
	if err != nil {
		return err
	}
	readiness := func(ctx context.Context, running bool) error {
		status, err := controller.Status(ctx)
		if err != nil {
			return err
		}
		expected := "prepared"
		if running {
			expected = "running"
		}
		if status.State != expected {
			return errors.New("background instance has not reached its expected phase")
		}
		state, token, err := readBackgroundInstance(path)
		if err != nil || state.Instance != boot.Instance || state.Address != boot.Address || token != boot.ControlToken {
			return errors.New("background instance publication is not verified")
		}
		return nil
	}
	result, err := start(cmd.Context(), "", boot, daemon.ValidateBootstrap, readiness)
	if result.Started && err != nil {
		return backgroundUncertain(err)
	}
	if err != nil || !result.Started {
		return &decisionError{"cannot start MCP background instance; inspect the runtime directory manually", err}
	}
	return writeBackgroundEndpoint(cmd, boot.Address)
}

func readBackgroundInstance(path string) (daemon.State, string, error) {
	store, err := daemon.OpenExistingStore(path)
	if errors.Is(err, os.ErrNotExist) {
		return daemon.State{}, "", daemon.ErrAbsent
	}
	if err != nil {
		return daemon.State{}, "", err
	}
	state, token, err := daemon.Read(store)
	if closeErr := store.Close(); closeErr != nil {
		return daemon.State{}, "", errors.Join(err, closeErr)
	}
	return state, token, err
}

func backgroundUncertain(cause error) error {
	return &decisionError{"MCP background may be running; use " + buildinfo.CommandName + " mcp status", cause}
}

func writeBackgroundEndpoint(cmd *cobra.Command, address string) error {
	line := "http://" + address + "/mcp\n"
	n, err := io.WriteString(cmd.OutOrStdout(), line)
	if err == nil && n != len(line) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return backgroundUncertain(err)
	}
	return nil
}
