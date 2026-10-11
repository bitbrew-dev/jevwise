package cli

import (
	"errors"
	"io"
	"path/filepath"

	"github.com/bitbrew-dev/jevwise/internal/daemon"
	"github.com/spf13/cobra"
)

func managedTokenPath(directory string) (string, error) {
	if directory == "" {
		return "", errors.New("invalid MCP runtime directory")
	}
	path, err := filepath.Abs(directory)
	if err != nil {
		return "", errors.New("invalid MCP runtime directory")
	}
	return filepath.Join(path, daemon.AgentTokenName), nil
}

func newMCPTokenPath(directory *string) *cobra.Command {
	return &cobra.Command{Use: "token-path", Short: "Show the automatic token file path, never its contents",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 0 {
				return errors.New("token-path does not accept positional arguments")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := cmd.Context().Err(); err != nil {
				return err
			}
			path, err := managedTokenPath(*directory)
			if err != nil {
				return err
			}
			line := path + "\n"
			n, err := io.WriteString(cmd.OutOrStdout(), line)
			if err == nil && n != len(line) {
				err = io.ErrShortWrite
			}
			if err != nil {
				return &decisionError{"cannot write MCP token path", err}
			}
			return nil
		}}
}
