package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/config"
	"github.com/bitbrew-dev/jevwise/internal/debuglog"
	"github.com/spf13/cobra"
)

type configEditor func(context.Context, string, string, io.Reader, io.Writer, io.Writer) error

func runConfigEditor(ctx context.Context, editor, path string, in io.Reader, out, stderr io.Writer) (resultErr error) {
	finish := debuglog.Trace(ctx, "config.editor")
	defer func() { finish(resultErr) }()
	process := exec.CommandContext(ctx, editor, path) // No shell; absolute file is one argument.
	process.WaitDelay = 250 * time.Millisecond        // Bound inherited Go-copy pipe waits.
	process.Stdin, process.Stdout, process.Stderr = in, out, stderr
	err := process.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func newConfigEdit(run configEditor) *cobra.Command {
	var override string
	cmd := &cobra.Command{Use: "edit", Short: "Edit selected configuration (default: vim on PATH)", Args: configNoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := cmd.Context().Err(); err != nil {
				return err
			}
			path, err := configPathFlag(cmd)
			if err != nil {
				return err
			}
			path, _, err = config.ResolvePath(path)
			if err != nil {
				return err
			}
			debuglog.Event(cmd.Context(), "config.edit.inspect")
			info, err := os.Lstat(path)
			if err != nil || !info.Mode().IsRegular() {
				return errors.New("config edit requires an existing regular file, not a link; initialize it first")
			}
			editor := strings.TrimSpace(override)
			if cmd.Flags().Changed("editor") && editor == "" {
				return errors.New("editor executable must not be blank")
			}
			if editor == "" {
				editor, err = config.Editor(path)
				if err != nil {
					return err
				}
			}
			if err := run(cmd.Context(), editor, path, cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()); err != nil {
				return &decisionError{"cannot run editor; install vim or select one editor executable name/path", err}
			}
			return nil
		}}
	cmd.Flags().StringVar(&override, "editor", "", "Override editor executable for this edit (no shell or arguments)")
	return cmd
}
