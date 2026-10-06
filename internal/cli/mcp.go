package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/daemon"
	"github.com/bitbrew-dev/jevwise/internal/mcpserver"
	"github.com/bitbrew-dev/jevwise/internal/service"
	"github.com/spf13/cobra"
)

type mcpRunner func(context.Context, service.DecisionService, time.Duration, mcpserver.RunOptions) error

func newMCP(factory ServiceFactory, run mcpRunner) *cobra.Command {
	return newMCPWithBackground(factory, run, daemon.Start)
}

func newMCPWithBackground(factory ServiceFactory, run mcpRunner, start mcpStarter) *cobra.Command {
	var address, tokenFile, runtimeDir string
	var background bool
	cmd := &cobra.Command{Use: "mcp", Short: "Serve the decide tool over authenticated local MCP HTTP",
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 0 {
				return errors.New("mcp does not accept positional arguments")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if err := ctx.Err(); err != nil {
				return err
			}
			canonical, err := mcpserver.ValidateAddress(address)
			if err != nil {
				return err
			}
			token := os.Getenv("JEV_MCP_TOKEN")
			if cmd.Flags().Changed("token-file") {
				path, err := filepath.Abs(tokenFile)
				if err != nil || tokenFile == "" {
					return errors.New("invalid MCP token file path")
				}
				token, err = daemon.ReadAgentToken(path)
				if err != nil {
					return err
				}
			}
			if err := mcpserver.ValidateToken(token); err != nil {
				return errors.New("JEV_MCP_TOKEN must contain a valid MCP bearer token")
			}
			cfg, err := LoadConfig(cmd)
			if err != nil {
				return err
			}
			if cfg.APIKey != "" && token == cfg.APIKey {
				return errors.New("MCP bearer token must differ from the upstream API key")
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if background {
				return runMCPBackground(cmd, cfg, canonical, token, runtimeDir, start)
			}
			svc, cleanup, err := factory(cfg)
			if cleanup != nil {
				defer cleanup()
			}
			if errors.Is(err, service.ErrNotImplemented) {
				return &decisionError{"provider is not implemented", err}
			}
			if err != nil || svc == nil {
				return &decisionError{"cannot initialize MCP decision service", err}
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			err = run(ctx, svc, cfg.Timeout, mcpserver.RunOptions{Address: canonical, Token: token,
				Ready: func(lifetime context.Context, endpoint string) error {
					if err := lifetime.Err(); err != nil {
						return err
					}
					line := endpoint + "\n"
					n, err := io.WriteString(cmd.OutOrStdout(), line)
					if err == nil && n != len(line) {
						err = io.ErrShortWrite
					}
					if err != nil {
						return &decisionError{"cannot write MCP endpoint", err}
					}
					return nil
				}})
			if err != nil {
				return &decisionError{"MCP server failed", err}
			}
			return nil
		}}
	cmd.Flags().StringVar(&address, "listen", "127.0.0.1:8080", "Literal loopback IP:port for /mcp; token from JEV_MCP_TOKEN")
	cmd.Flags().BoolVar(&background, "background", false, "Start an independent MCP background process")
	cmd.PersistentFlags().StringVar(&tokenFile, "token-file", "", "Read an owner-private bearer token file instead of JEV_MCP_TOKEN")
	cache, _ := os.UserCacheDir()
	if cache != "" {
		runtimeDir = filepath.Join(cache, "jevwise-mcp")
	}
	cmd.PersistentFlags().StringVar(&runtimeDir, "runtime-dir", runtimeDir, "Private per-user MCP background runtime directory")
	cmd.AddCommand(newMCPManagement("status", &runtimeDir), newMCPManagement("stop", &runtimeDir))
	return cmd
}
