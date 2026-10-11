package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/config"
	"github.com/bitbrew-dev/jevwise/internal/mcpserver"
	"github.com/bitbrew-dev/jevwise/internal/service"
)

func TestMCPEnvironmentNamesPrecedence(t *testing.T) {
	decisionEnvironment(t)
	t.Setenv("JEV_API_KEY", "upstream-fixture")
	names := []string{"JEVWISE_MCP_TOKEN", "JEVWISE_MCP_AUTH_TOKEN", "JEV_MCP_TOKEN"}
	for i, name := range names {
		t.Setenv(name, name+"-fixture")
		for _, earlier := range names[:i] {
			t.Setenv(earlier, "")
		}
		for _, later := range names[i+1:] {
			t.Setenv(later, "lower-priority-fixture")
		}
		factory := func(cfg config.Config) (service.DecisionService, func(), error) {
			if cfg.APIKey != "upstream-fixture" {
				t.Error("API key did not resolve independently")
			}
			return mcpFixtureService(t), nil, nil
		}
		ran := false
		run := func(_ context.Context, _ service.DecisionService, _ time.Duration, options mcpserver.RunOptions) error {
			ran = true
			if options.Token != name+"-fixture" {
				t.Error("wrong MCP token precedence")
			}
			return nil
		}
		out, stderr, err := execute(mcpRoot(factory, run), "mcp")
		if err != nil || out != "" || stderr != "" || !ran {
			t.Fatal("MCP environment selection failed", name, err)
		}
	}
}

func TestMCPInvalidPreferredTokenNeverFallsBack(t *testing.T) {
	decisionEnvironment(t)
	t.Setenv("JEV_MCP_TOKEN", "valid-legacy-fixture")
	factory := func(config.Config) (service.DecisionService, func(), error) {
		t.Fatal("invalid or reused token initialized service")
		return nil, nil, nil
	}
	for _, name := range []string{"JEVWISE_MCP_TOKEN", "JEVWISE_MCP_AUTH_TOKEN"} {
		for _, token := range []string{" ", "private invalid", "private\nvalue", "private-秘密"} {
			t.Setenv(name, token)
			out, stderr, err := execute(NewRootWithFactory(factory), "mcp")
			if err == nil || !strings.Contains(err.Error(), "JEVWISE_MCP_TOKEN") || strings.Contains(err.Error(), "private") || out != "" || stderr != "" {
				t.Fatal("invalid preferred token fell back or leaked", err)
			}
		}
		t.Setenv(name, "")
	}
	t.Setenv("JEVWISE_MCP_TOKEN", "same-private-fixture")
	t.Setenv("JEV_API_KEY", "same-private-fixture")
	if _, _, err := execute(NewRootWithFactory(factory), "mcp"); err == nil || !strings.Contains(err.Error(), "must differ") || strings.Contains(err.Error(), "private") {
		t.Fatal("canonical credentials were allowed to match or leaked", err)
	}
}
