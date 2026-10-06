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

type updateOps struct {
	latest       func(context.Context) (update.Release, error)
	download     func(context.Context, update.Release, string, string) (*update.Binary, error)
	replace      func(context.Context, *update.Binary, string) (update.InstallResult, error)
	goos, goarch string
}

func updateFailure(installed bool, message string, cause error) error {
	if installed {
		message = "Jev update installed; restart required; " + message
	}
	return &decisionError{message, cause}
}

func newUpdate(ops updateOps, current string) *cobra.Command {
	var check bool
	cmd := &cobra.Command{Use: "update", Short: "Install or check the latest stable Jev release",
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
				if _, err := update.Compare(current, current); err != nil {
					return errors.New("self-update requires a stable versioned build; install manually or use --check")
				}
				if (ops.goos != "linux" && ops.goos != "darwin") || (ops.goarch != "amd64" && ops.goarch != "arm64") {
					return errors.New("self-update is unsupported on this platform; update manually or use --check")
				}
				if ops.download == nil || ops.replace == nil {
					return errors.New("self-update installation is unavailable")
				}
			}
			if ops.latest == nil {
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
			release, err := ops.latest(ctx)
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			var output string
			installed := false
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
					if !check {
						binary, err := ops.download(ctx, release, ops.goos, ops.goarch)
						if ctxErr := ctx.Err(); ctxErr != nil {
							return ctxErr
						}
						if err != nil {
							return updateFailure(false, "cannot download Jev update", err)
						}
						if binary == nil {
							return errors.New("download returned no Jev executable")
						}
						result, err := ops.replace(ctx, binary, current)
						installed = result.Installed
						err = errors.Join(err, ctx.Err())
						if err != nil {
							return updateFailure(installed, "cannot finish Jev update", err)
						}
						if !installed {
							return errors.New("Jev update was not installed")
						}
						output = fmt.Sprintf("Installed Jev %s -> %s; restart required\n", current, release.Tag)
					}
				case comparison == 0:
					output = fmt.Sprintf("Already current: %s\n", current)
				default:
					output = fmt.Sprintf("Installed version %s is newer than latest %s; no downgrade.\n", current, release.Tag)
				}
			}
			if err := ctx.Err(); err != nil {
				return updateFailure(installed, "cannot write release information", err)
			}
			n, err := io.WriteString(cmd.OutOrStdout(), output)
			if err == nil && n != len(output) {
				err = io.ErrShortWrite
			}
			err = errors.Join(err, ctx.Err())
			if err != nil {
				return updateFailure(installed, "cannot write release information", err)
			}
			return nil
		}}
	cmd.Flags().BoolVar(&check, "check", false, "Check metadata only; do not download or install")
	return cmd
}
