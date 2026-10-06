// Package cli builds isolated commands. Callers can inject Cobra's streams and context.
package cli

import (
	"errors"
	"runtime"

	"github.com/bitbrew-dev/jevwise/internal/buildinfo"
	"github.com/bitbrew-dev/jevwise/internal/config"
	"github.com/bitbrew-dev/jevwise/internal/mcpserver"
	"github.com/bitbrew-dev/jevwise/internal/update"
	"github.com/phuslu/log"
	"github.com/spf13/cobra"
)

// NewRoot returns a fresh command tree without reading configuration or credentials.
// Service commands should call LoadConfig in RunE, not in a persistent hook,
// so help and completion remain usable without a valid configuration.
func NewRoot() *cobra.Command { return NewRootWithFactory(defaultServiceFactory) }

// NewRootWithFactory injects service creation while preserving isolated command state.
// A nil factory uses the default SDK-backed factory. Creation is deferred until RunE.
func NewRootWithFactory(factory ServiceFactory) *cobra.Command {
	if factory == nil {
		factory = defaultServiceFactory
	}
	var showVersion bool
	cmd := &cobra.Command{
		Use: "jev", Short: "Make decisions with Jev",
		SilenceUsage: true, SilenceErrors: true,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 0 {
				return errors.New("root command does not accept positional arguments")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if showVersion {
				return writeVersion(cmd)
			}
			return cmd.Help()
		},
	}
	// Flag parser errors can include argument values. Do not expose credentials.
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, _ error) error {
		return errors.New("invalid command flags: see --help")
	})
	// Leave Command.Version empty: Cobra prints raw version-template writer errors.
	cmd.Flags().BoolVar(&showVersion, "version", false, "Print version and build metadata")
	flags := cmd.PersistentFlags()
	flags.String("config", "", "Path to TOML configuration")
	flags.String("provider", "jev", "Decision provider: jev, codex or claude")
	flags.String("api-key", "", "Jev API key (prefer environment or config)")
	flags.String("base-url", "https://api.typesafe.ai", "Jev API base URL")
	flags.String("model", "jev-latest", "Jev model")
	flags.String("timeout", "10s", "Operation timeout, for example 10s")
	cmd.AddCommand(newVersion())
	releaseClient := update.NewClient(nil)
	cmd.AddCommand(newUpdate(updateOps{releaseClient.Latest, releaseClient.Download, update.Replace, runtime.GOOS, runtime.GOARCH}, buildinfo.Version))
	cmd.AddCommand(newDecide(factory))
	cmd.AddCommand(newMCP(factory, mcpserver.Run))
	cmd.AddCommand(newSkill(newSkillFetch(nil), installSkill))
	return cmd
}

// LoadConfig resolves a command's inherited flags without changing global state.
func LoadConfig(cmd *cobra.Command) (config.Config, error) {
	path, err := cmd.Flags().GetString("config")
	if err != nil {
		return config.Config{}, errors.New("configuration flag is unavailable")
	}
	return config.Load(path, cmd.Flags())
}

// Logger writes only to the command's error stream. Log status and safe metadata,
// never credentials, prompt bodies, option content, or raw request/response bodies.
func Logger(cmd *cobra.Command) log.Logger {
	return log.Logger{Level: log.InfoLevel, Writer: log.IOWriter{Writer: cmd.ErrOrStderr()}}
}
