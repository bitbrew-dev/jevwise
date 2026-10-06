package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/update"
	"github.com/spf13/cobra"
)

// newUpdate only checks stable release metadata. Installation remains unavailable
// until the separately reviewed download/verification/replacement implementation.
func newUpdate(latest func(context.Context) (update.Release, error), current string) *cobra.Command {
	var check bool
	cmd := &cobra.Command{Use: "update", Short: "Check the latest stable Jev release",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 0 {
				return errors.New("update does not accept positional arguments")
			}
			return nil
		}, RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if err := ctx.Err(); err != nil {
				return err
			}
			if !check {
				return errors.New("self-update is not available yet; use --check to inspect releases")
			}
			if latest == nil {
				return errors.New("release lookup is unavailable")
			}
			timeout := "10s"
			if flag := cmd.Flags().Lookup("timeout"); flag != nil {
				timeout = flag.Value.String()
			}
			duration, err := time.ParseDuration(timeout)
			if err != nil || duration <= 0 {
				return errors.New("update timeout must be a positive duration")
			}
			ctx, cancel := context.WithTimeout(ctx, duration)
			defer cancel()
			if err := ctx.Err(); err != nil {
				return err
			}
			release, err := latest(ctx)
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			var output string
			if errors.Is(err, update.ErrNoRelease) {
				output = "No stable Jev release is published yet.\n"
			} else if err != nil {
				return &decisionError{"cannot check Jev releases", err}
			} else {
				if _, err := update.Compare(release.Tag, release.Tag); err != nil {
					return &decisionError{"invalid latest release version", err}
				}
				comparison, err := update.Compare(current, release.Tag)
				switch {
				case err != nil:
					output = fmt.Sprintf("Latest release: %s; current version unknown (development or unversioned build).\n", release.Tag)
				case comparison < 0:
					output = fmt.Sprintf("Update available: %s -> %s\n", current, release.Tag)
				case comparison == 0:
					output = fmt.Sprintf("Already current: %s\n", current)
				default:
					output = fmt.Sprintf("Installed version %s is newer than latest %s; no downgrade.\n", current, release.Tag)
				}
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			n, err := io.WriteString(cmd.OutOrStdout(), output)
			if err == nil && n != len(output) {
				err = io.ErrShortWrite
			}
			if err != nil {
				return &decisionError{"cannot write release information", err}
			}
			return nil
		}}
	cmd.Flags().BoolVar(&check, "check", false, "Check metadata only; do not download or install")
	return cmd
}
