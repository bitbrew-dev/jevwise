package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bitbrew-dev/jevwise/internal/config"
	"github.com/bitbrew-dev/jevwise/internal/service"
)

func TestVerboseAliasAndExplicitFalse(t *testing.T) {
	configEnvironment(t)
	for _, tt := range []struct {
		flags   []string
		enabled bool
	}{
		{nil, false}, {[]string{"--debug"}, true}, {[]string{"--verbose"}, true},
		{[]string{"--debug=false", "--verbose"}, true}, {[]string{"--verbose", "--debug=false"}, true},
		{[]string{"--verbose=false", "--debug"}, true}, {[]string{"--debug", "--verbose=false"}, true},
		{[]string{"--debug=false", "--verbose=false"}, false},
	} {
		for _, before := range []bool{false, true} {
			cmd := NewRoot()
			args := append([]string{"version"}, tt.flags...)
			if before {
				args = append(append([]string{}, tt.flags...), "version")
			}
			out, stderr, err := execute(cmd, args...)
			if err != nil || !strings.Contains(out, "commit:") || (stderr != "") != tt.enabled || debugEnabled(cmd) != tt.enabled {
				t.Fatal("alias flag semantics changed", args, err)
			}
			baseline := NewRoot()
			_, _, _ = execute(baseline, "version", "--debug")
			if (Logger(cmd).Level == Logger(baseline).Level) != tt.enabled {
				t.Fatal("error logger disagrees with trace logger")
			}
		}
	}
	for _, args := range [][]string{{"--verbose", "--help"}, {"mcp", "--help", "--verbose"}, {"completion", "bash", "--verbose"}} {
		if _, stderr, err := execute(NewRoot(), args...); err != nil || stderr != "" {
			t.Fatal("lazy command logged or failed", err)
		}
	}
	cmd := NewRoot()
	_, _, _ = execute(cmd, "version", "--verbose")
	if _, stderr, err := execute(cmd, "version", "--verbose=false"); err != nil || stderr != "" {
		t.Fatal("alias leaked across invocations", err)
	}
	if _, stderr, err := execute(NewRoot(), "version"); err != nil || stderr != "" {
		t.Fatal("alias leaked to another root", err)
	}
}

func TestMCPInitializationDiagnosticsAndPrivacy(t *testing.T) {
	configEnvironment(t)
	t.Setenv("JEV_MCP_TOKEN", "private-agent-token")
	privateCause := errors.New("private-factory-error")
	cases := []struct {
		key, provider, event, message string
		factory                       ServiceFactory
		cause                         error
	}{
		{"", "jev", "api_key_missing", "API key is required", nil, nil},
		{"private key", "jev", "api_key_invalid", "printable ASCII", nil, nil},
		{"private-秘密", "jev", "api_key_invalid", "printable ASCII", nil, nil},
		{"private-key", "codex", "provider_unimplemented", "provider is not implemented", nil, service.ErrNotImplemented},
		{"private-key", "jev", "factory_failed", "cannot initialize MCP", func(config.Config) (service.DecisionService, func(), error) { return nil, nil, privateCause }, privateCause},
		{"private-key", "jev", "nil_service", "cannot initialize MCP", func(config.Config) (service.DecisionService, func(), error) {
			var svc *mcpFixture
			return svc, nil, nil
		}, nil},
	}
	for _, tt := range cases {
		for _, flag := range []string{"", "--debug", "--verbose"} {
			cmd := NewRootWithFactory(tt.factory)
			args := []string{"mcp", "--api-key", tt.key, "--provider", tt.provider}
			if flag != "" {
				args = append(args, flag)
			}
			out, stderr, err := execute(cmd, args...)
			if err == nil || out != "" || !strings.Contains(err.Error(), tt.message) || (tt.cause != nil && !errors.Is(err, tt.cause)) {
				t.Fatal("initialization failure unclear or cause lost", tt.event, err)
			}
			if strings.Contains(stderr+err.Error(), "private") {
				t.Fatal("initialization diagnostics leaked credentials/cause")
			}
			if flag == "" && stderr != "" {
				t.Fatal("default debugging enabled")
			}
			if flag != "" && (!strings.Contains(stderr, "service.initialize."+tt.event) || !strings.Contains(stderr, `"success":false`)) {
				t.Fatal("initialization category or failure trace missing", tt.event)
			}
		}
	}
	// A URL accepted by config but rejected by the SDK must still be sanitized.
	_, stderr, err := execute(NewRoot(), "mcp", "--verbose", "--api-key=private-key", "--base-url=https://private-host.example/?")
	if err == nil || !strings.Contains(stderr, "service.initialize.client_configuration") || strings.Contains(stderr+err.Error(), "private") {
		t.Fatal("client configuration failure leaked values or lacked diagnostics")
	}
	_, stderr, err = execute(NewRoot(), "decide", "--verbose", "--prompt=private-prompt", "--option=A", "--option=B")
	if err == nil || !strings.Contains(err.Error(), "API key is required") || !strings.Contains(stderr, "service.initialize.api_key_missing") || strings.Contains(stderr, "private") {
		t.Fatal("decision initialization did not share safe diagnostics")
	}
	// Context failures and arbitrary factories never expose raw errors.
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		cmd := NewRootWithFactory(func(config.Config) (service.DecisionService, func(), error) { return nil, nil, cause })
		_, _, err := execute(cmd, "mcp", "--verbose")
		if !errors.Is(err, cause) {
			t.Fatal("cancellation identity lost")
		}
	}
}
