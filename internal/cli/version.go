package cli

import (
	"errors"
	"io"

	"github.com/bitbrew-dev/jevwise/internal/buildinfo"
	"github.com/spf13/cobra"
)

func newVersion() *cobra.Command {
	return &cobra.Command{Use: "version", Short: "Print version and build metadata",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 0 {
				return errors.New("version does not accept positional arguments")
			}
			return nil
		}, RunE: func(cmd *cobra.Command, _ []string) error { return writeVersion(cmd) }}
}

func writeVersion(cmd *cobra.Command) error {
	if err := cmd.Context().Err(); err != nil {
		return err
	}
	output := buildinfo.String()
	n, err := io.WriteString(cmd.OutOrStdout(), output)
	if err == nil && n != len(output) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return &decisionError{"cannot write version information", err}
	}
	return nil
}
