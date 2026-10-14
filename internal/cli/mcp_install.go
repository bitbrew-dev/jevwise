package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/debuglog"
	"github.com/bitbrew-dev/jevwise/internal/mcpserver"
	"github.com/spf13/cobra"
)

func newMCPInstall(run agentCommand) *cobra.Command {
	var address, scope string
	noArgs := func(_ *cobra.Command, args []string) error {
		if len(args) != 0 {
			return errors.New("MCP install accepts only codex or claude, without positional arguments")
		}
		return nil
	}
	cmd := &cobra.Command{Use: "install", Short: "Register Jevwise with an installed MCP client", Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	cmd.PersistentFlags().StringVar(&address, "listen", "127.0.0.1:8080", "Server's literal loopback IP:port (same as mcp --listen)")
	cmd.PersistentFlags().StringVar(&scope, "scope", "user", "Client settings scope: user; Claude also supports local or project")
	_ = cmd.RegisterFlagCompletionFunc("scope", func(cmd *cobra.Command, _ []string, prefix string) ([]string, cobra.ShellCompDirective) {
		choices := []string{"user"}
		if cmd.Name() != "codex" {
			choices = append(choices, "local", "project")
		}
		var matches []string
		for _, choice := range choices {
			if strings.HasPrefix(choice, prefix) {
				matches = append(matches, choice)
			}
		}
		return matches, cobra.ShellCompDirectiveNoFileComp
	})
	for _, client := range []string{"codex", "claude"} {
		cmd.AddCommand(&cobra.Command{Use: client, Short: "Register Jevwise with " + client, Args: noArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				if err := cmd.Context().Err(); err != nil {
					return err
				}
				canonical, err := mcpserver.ValidateAddress(address)
				if err != nil {
					return err
				}
				if scope != "user" && (client != "claude" || scope != "local" && scope != "project") {
					return errors.New("invalid MCP install scope; Codex supports user only")
				}
				duration, err := time.ParseDuration(cmd.Flags().Lookup("timeout").Value.String())
				if err != nil || duration <= 0 {
					return errors.New("MCP installation timeout must be a positive duration")
				}
				ctx, cancel := context.WithTimeout(cmd.Context(), duration)
				defer cancel()
				endpoint := "http://" + canonical + "/mcp"
				debuglog.Event(ctx, "mcp.install.register")
				if client == "codex" {
					data, inspectErr := run(ctx, client, "mcp", "list", "--json")
					var servers []struct{ Name string }
					if inspectErr != nil || len(data) > agentOutputLimit || json.Unmarshal(data, &servers) != nil || servers == nil {
						return &decisionError{"cannot inspect Codex MCP settings; check installed client and configuration", inspectErr}
					}
					for _, server := range servers {
						if server.Name == "" {
							return errors.New("invalid Codex MCP inventory")
						}
						if server.Name == "jevwise" {
							return errors.New("Codex MCP entry jevwise already exists; inspect it before changing configuration")
						}
					}
					if err := ctx.Err(); err != nil {
						return err
					}
					_, err = run(ctx, client, "mcp", "add", "jevwise", "--url", endpoint, "--bearer-token-env-var", "JEVWISE_MCP_TOKEN")
				} else {
					data, _ := json.Marshal(map[string]any{"type": "http", "url": endpoint, "headers": map[string]string{"Authorization": "Bearer ${JEVWISE_MCP_TOKEN}"}})
					_, err = run(ctx, client, "--bare", "mcp", "add-json", "--scope", scope, "jevwise", string(data))
				}
				if err != nil {
					return &decisionError{"cannot register MCP client; check client installation, settings and duplicate entry (diagnostics withheld)", err}
				}
				line := "Registered Jevwise MCP for " + client + ". Load JEVWISE_MCP_TOKEN in the client environment before launch.\n"
				n, err := io.WriteString(cmd.OutOrStdout(), line)
				if err == nil && n != len(line) {
					err = io.ErrShortWrite
				}
				if err != nil {
					return &decisionError{"MCP client registered but confirmation could not be written", err}
				}
				return ctx.Err()
			}})
	}
	return cmd
}
