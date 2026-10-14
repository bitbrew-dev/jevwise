package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/daemon"
	"github.com/bitbrew-dev/jevwise/internal/debuglog"
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
			token := mcpEnvironmentToken()
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
			if err := mcpserver.ValidateToken(token); err != nil && (token != "" || cmd.Flags().Changed("token-file")) {
				return errors.New("MCP bearer token is missing or invalid: set JEVWISE_MCP_TOKEN or use --token-file")
			}
			cfg, err := LoadConfig(cmd)
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if token == "" {
				if cfg.Provider != "jev" {
					return initializationFailure(ctx, "provider is not implemented", service.ErrNotImplemented)
				}
				if cfg.APIKey == "" {
					return initializationFailure(ctx, "cannot initialize MCP decision service", &initializationError{kind: "api_key_missing"})
				}
				path, err := managedTokenPath(runtimeDir)
				if err != nil {
					return err
				}
				// The default cache parent may not exist on a fresh user account.
				// Custom runtime paths retain the existing caller-prepared-parent rule.
				if !cmd.Flags().Changed("runtime-dir") {
					if err := os.MkdirAll(filepath.Dir(filepath.Dir(path)), 0700); err != nil {
						return &decisionError{"cannot prepare MCP cache directory", err}
					}
				}
				debuglog.Event(ctx, "mcp.token.auto_resolve")
				token, err = daemon.EnsureAgentToken(filepath.Dir(path))
				if err != nil {
					debuglog.Event(ctx, "mcp.token.auto_failed")
					return err
				}
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
			debuglog.Event(ctx, "mcp.service.initialize")
			svc, cleanup, err := factory(cfg)
			if cleanup != nil {
				defer cleanup()
			}
			if err != nil || !initializedMCPService(svc) {
				return initializationFailure(ctx, "cannot initialize MCP decision service", err)
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
	cmd.Flags().StringVar(&address, "listen", "127.0.0.1:8080", "Literal loopback IP:port for /mcp")
	cmd.Flags().BoolVar(&background, "background", false, "Start an independent MCP background process")
	cmd.PersistentFlags().StringVar(&tokenFile, "token-file", "", "Read an owner-private bearer token file instead of JEVWISE_MCP_TOKEN")
	cache, _ := os.UserCacheDir()
	if cache != "" {
		runtimeDir = filepath.Join(cache, "jevwise-mcp")
	}
	cmd.PersistentFlags().StringVar(&runtimeDir, "runtime-dir", runtimeDir, "Private per-user MCP background runtime directory")
	cmd.AddCommand(newMCPManagement("status", &runtimeDir), newMCPManagement("stop", &runtimeDir), newMCPTokenPath(&runtimeDir), newMCPInstall(runAgentCommand))
	return cmd
}

// Canonical token wins over compatibility aliases. A nonempty invalid token
// must fail validation, not silently authenticate using a lower-priority value.
func mcpEnvironmentToken() string {
	for _, name := range []string{"JEVWISE_MCP_TOKEN", "JEVWISE_MCP_AUTH_TOKEN", "JEV_MCP_TOKEN"} {
		if token := os.Getenv(name); token != "" {
			return token
		}
	}
	return ""
}
