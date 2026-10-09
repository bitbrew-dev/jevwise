package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bitbrew-dev/jevwise/internal/config"
	"github.com/bitbrew-dev/jevwise/internal/debuglog"
	"github.com/spf13/cobra"
)

func configNoArgs(_ *cobra.Command, args []string) error {
	if len(args) != 0 {
		return errors.New("config commands do not accept positional arguments")
	}
	return nil
}

func configPathFlag(cmd *cobra.Command) (string, error) {
	path, err := cmd.Flags().GetString("config")
	if err != nil || cmd.Flags().Changed("config") && strings.TrimSpace(path) == "" {
		return "", errors.New("configuration path must not be blank")
	}
	return path, nil
}

func newConfig() *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Manage TOML configuration", Args: configNoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	var local bool
	init := &cobra.Command{Use: "init", Short: "Create configuration without replacing an existing file", Args: configNoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := cmd.Context().Err(); err != nil {
				return err
			}
			path, err := configPathFlag(cmd)
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("local") && !local || local && cmd.Flags().Changed("config") {
				return errors.New("choose --local or --config, not both")
			}
			if local {
				path, err = filepath.Abs(config.LocalName)
			} else if path == "" {
				path, err = config.GlobalPath()
			} else {
				path, err = filepath.Abs(path)
			}
			if err != nil {
				return errors.New("cannot determine configuration path")
			}
			debuglog.Event(cmd.Context(), "config.init.publish")
			if err := config.Init(cmd.Context(), path); err != nil {
				if errors.Is(err, os.ErrExist) {
					return errors.New("configuration already exists; no file replaced")
				}
				return &decisionError{"cannot initialize configuration", err}
			}
			message := fmt.Sprintf("Created configuration: %q\n", path)
			if n, err := fmt.Fprint(cmd.OutOrStdout(), message); err != nil || n != len(message) {
				return errors.New("configuration created but output failed")
			}
			return nil
		}}
	init.Flags().BoolVar(&local, "local", false, "Create ./jevwise.toml instead of global configuration")
	view := &cobra.Command{Use: "view", Short: "Show file settings with API key redacted (ignores environment)", Args: configNoArgs,
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
			debuglog.Event(cmd.Context(), "config.view.read")
			text, err := config.View(path)
			if err != nil {
				return err
			}
			if err := cmd.Context().Err(); err != nil {
				return err
			}
			if n, err := fmt.Fprint(cmd.OutOrStdout(), text); err != nil || n != len(text) {
				return errors.New("cannot write configuration view")
			}
			return nil
		}}
	cmd.AddCommand(init, view, newConfigEdit(runConfigEditor))
	return cmd
}
