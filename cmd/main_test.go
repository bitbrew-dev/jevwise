package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunExitStatusAndStreams(t *testing.T) {
	for _, tt := range []struct {
		args []string
		code int
	}{{[]string{"--help"}, 0}, {[]string{"--unknown=private-secret"}, 1}} {
		var out, stderr bytes.Buffer
		code := run(context.Background(), tt.args, strings.NewReader(""), &out, &stderr)
		if code != tt.code {
			t.Fatalf("exit code = %d, want %d", code, tt.code)
		}
		if code == 0 && (out.Len() == 0 || stderr.Len() != 0) {
			t.Fatal("help streams incorrect")
		}
		if code != 0 && (out.Len() != 0 || stderr.Len() == 0 || strings.Contains(stderr.String(), "private-secret")) {
			t.Fatal("error streams incorrect or secret exposed")
		}
	}
}

func TestRunMCPInitializationLogging(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("JEV_MCP_TOKEN", "private-agent-token")
	for _, name := range []string{"JEV_API_KEY", "JEVWISE_API_KEY", "JEVWISE_MCP_TOKEN", "JEVWISE_MCP_AUTH_TOKEN", "TS_JEV_API_KEY", "TYPESAFE_API_KEY", "TS_JEV_BASE_URL", "TYPESAFE_BASE_URL", "TS_JEV_MODEL", "TYPESAFE_DEFAULT_MODEL", "TS_JEV_PROVIDER", "TS_JEV_TIMEOUT"} {
		t.Setenv(name, "")
	}
	for _, flag := range []string{"", "--debug", "--verbose"} {
		args := []string{"mcp"}
		if flag != "" {
			args = append(args, flag)
		}
		var out, stderr bytes.Buffer
		if code := run(context.Background(), args, strings.NewReader(""), &out, &stderr); code != 1 || out.Len() != 0 {
			t.Fatal("startup failure exit/stdout changed")
		}
		logs := stderr.String()
		if !strings.Contains(logs, "API key is required") || !strings.Contains(logs, "docs/configuration.md") || !strings.Contains(logs, `"level":"error"`) || strings.Contains(logs, "private") {
			t.Fatal("startup error lacked safe guidance")
		}
		if strings.Contains(logs, "service.initialize.api_key_missing") != (flag != "") {
			t.Fatal("debug startup category not controlled by alias flags")
		}
	}
}
