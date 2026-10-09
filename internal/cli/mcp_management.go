package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/bitbrew-dev/jevwise/internal/daemon"
	"github.com/bitbrew-dev/jevwise/internal/debuglog"
	"github.com/spf13/cobra"
)

func newMCPManagement(action string, runtimeDir *string) *cobra.Command {
	cmd := &cobra.Command{Use: action, Short: "Inspect the authenticated MCP background instance"}
	if action == "stop" {
		cmd.Short = "Stop the authenticated MCP background instance"
	}
	cmd.Args = func(_ *cobra.Command, args []string) error {
		if len(args) != 0 {
			return errors.New("MCP management does not accept positional arguments")
		}
		return nil
	}
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		if err := cmd.Context().Err(); err != nil {
			return err
		}
		path, err := filepath.Abs(*runtimeDir)
		if err != nil || *runtimeDir == "" {
			return errors.New("invalid MCP runtime directory")
		}
		debuglog.Event(cmd.Context(), "mcp.management.inspect")
		store, err := daemon.OpenExistingStore(filepath.Clean(path))
		if errors.Is(err, os.ErrNotExist) {
			return writeMCPManagement(cmd, "MCP background instance is not running", false)
		}
		if err != nil {
			return mcpInspectionError(err)
		}
		state, token, readErr := daemon.Read(store)
		if closeErr := store.Close(); closeErr != nil {
			return mcpInspectionError(errors.Join(readErr, closeErr))
		}
		if errors.Is(readErr, daemon.ErrAbsent) {
			return writeMCPManagement(cmd, "MCP background instance is not running", false)
		}
		if readErr != nil {
			return mcpInspectionError(readErr)
		}
		controller, err := daemon.NewController(state.Address, state.Instance, token)
		if err != nil {
			return mcpInspectionError(err)
		}
		var status daemon.ManagementStatus
		if action == "stop" {
			status, err = controller.Stop(cmd.Context())
		} else {
			status, err = controller.Status(cmd.Context())
		}
		if err != nil {
			return mcpInspectionError(err)
		}
		if action == "stop" {
			return writeMCPManagement(cmd, "MCP background stop acknowledged", true)
		}
		message := "MCP background instance is " + status.State
		if state.PID > 0 {
			message += "\nPID: " + strconv.Itoa(state.PID) + " (inspection only)"
		}
		return writeMCPManagement(cmd, message, false)
	}
	return cmd
}

func mcpInspectionError(cause error) error {
	return &decisionError{"cannot verify MCP background instance; inspect the runtime directory manually", cause}
}

func writeMCPManagement(cmd *cobra.Command, message string, stopped bool) error {
	line := message + "\n"
	n, err := io.WriteString(cmd.OutOrStdout(), line)
	if err == nil && n != len(line) {
		err = io.ErrShortWrite
	}
	if err != nil {
		message := "cannot write MCP background status"
		if stopped {
			message = "MCP stop acknowledged but output failed"
		}
		return &decisionError{message, err}
	}
	return nil
}
