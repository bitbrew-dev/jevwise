package config

import (
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
)

func TestAPIEnvironmentNamesPrecedence(t *testing.T) {
	cleanEnv(t)
	path := writeConfig(t, filepath.Join(t.TempDir(), "config.toml"), `api_key = "file-key"`)
	names := []string{"JEV_API_KEY", "JEVWISE_API_KEY", "TS_JEV_API_KEY", "TYPESAFE_API_KEY"}
	for i, name := range names {
		t.Setenv(name, name+"-fixture")
		for _, earlier := range names[:i] {
			t.Setenv(earlier, " \t ")
		}
		for _, later := range names[i+1:] {
			t.Setenv(later, "lower-priority-fixture")
		}
		cfg, err := Load(path, nil)
		if err != nil || cfg.APIKey != name+"-fixture" {
			t.Fatal("wrong API environment precedence", name, err)
		}
	}
	t.Setenv("JEV_API_KEY", "$JEVWISE_TEST_LITERAL")
	t.Setenv("JEVWISE_TEST_LITERAL", "must-not-expand")
	cfg, err := Load(path, nil)
	if err != nil || cfg.APIKey != "$JEVWISE_TEST_LITERAL" {
		t.Fatal("canonical environment key was expanded", err)
	}
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("api-key", "", "")
	for _, value := range []string{"flag-fixture", ""} {
		if err := flags.Set("api-key", value); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(path, flags)
		if err != nil || cfg.APIKey != value {
			t.Fatal("changed API flag did not win", err)
		}
	}
}

func TestCustomTOMLAPIReferenceNotMCPAuthentication(t *testing.T) {
	cleanEnv(t)
	t.Setenv("JEV_MCP_API_KEY", "upstream-fixture")
	t.Setenv("JEVWISE_MCP_TOKEN", "agent-fixture")
	for _, reference := range []string{"$JEV_MCP_API_KEY", "${JEV_MCP_API_KEY}"} {
		path := writeConfig(t, filepath.Join(t.TempDir(), "config.toml"), "api_key = '"+reference+"'")
		cfg, err := Load(path, nil)
		if err != nil || cfg.APIKey != "upstream-fixture" {
			t.Fatal("custom TOML API key did not expand", err)
		}
	}
	cfg, err := Load("", nil)
	if err != nil || cfg.APIKey != "" {
		t.Fatal("custom or MCP variable became an API override", err)
	}
}
