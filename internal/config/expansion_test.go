package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

func TestLoadExpandsOnlyFileAPIKeyAndTrimsResult(t *testing.T) {
	cleanEnv(t)
	const name = "JEVWISE_TEST_EXPANSION_KEY"
	path := filepath.Join(t.TempDir(), "config.toml")
	for _, test := range []struct {
		name, file, variable, want string
	}{
		{"dollar", "$" + name, "synthetic-key", "synthetic-key"},
		{"braces", "${" + name + "}", " \tsynthetic-key\n ", "synthetic-key"},
		{"embedded", "prefix-${" + name + "}-suffix", "part", "prefix-part-suffix"},
		{"missing", "$" + name, "", ""},
		{"whitespace", "$" + name, " \t\n ", ""},
		{"literal", "file-key", "ignored", "file-key"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(name, test.variable)
			if test.name == "missing" {
				if err := os.Unsetenv(name); err != nil {
					t.Fatal(err)
				}
			}
			writeConfig(t, path, "api_key = '"+test.file+"'")
			cfg, err := Load(path, nil)
			if err != nil || cfg.APIKey != test.want {
				t.Fatal("wrong file key resolution", err)
			}
		})
	}
	writeConfig(t, path, "api_key = '$"+name+"'")
	t.Setenv(name, "file-expanded-key")
	t.Setenv("TS_JEV_API_KEY", "  $"+name+"  ")
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("api-key", "unused", "")
	cfg, err := Load(path, flags)
	if err != nil || cfg.APIKey != "$"+name {
		t.Fatal("environment key was expanded or precedence changed", err)
	}
	if err := flags.Set("api-key", "  ${"+name+"}  "); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(path, flags)
	if err != nil || cfg.APIKey != "${"+name+"}" {
		t.Fatal("flag key was expanded or precedence changed", err)
	}
	if err := flags.Set("api-key", ""); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load(path, flags)
	if err != nil || cfg.APIKey != "" {
		t.Fatal("explicit empty flag fell back", err)
	}
	t.Setenv("TS_JEV_API_KEY", "")
	t.Setenv("TYPESAFE_API_KEY", "$"+name)
	cfg, err = Load(path, nil)
	if err != nil || cfg.APIKey != "$"+name {
		t.Fatal("upstream environment key was expanded", err)
	}
}

func TestViewTemplateAndReferenceIndependentOfEnvironment(t *testing.T) {
	cleanEnv(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := Init(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	if _, err := View(path); err != nil {
		t.Fatal("fresh template cannot be viewed", err)
	}
	const name = "JEVWISE_TEST_VIEW_KEY"
	writeConfig(t, path, "api_key = '${"+name+"}'")
	t.Setenv(name, "")
	before, err := View(path)
	if err != nil {
		t.Fatal("unresolved key prevented file view", err)
	}
	t.Setenv(name, "synthetic-secret")
	after, err := View(path)
	if err != nil || after != before || strings.Contains(after, "synthetic-secret") || !strings.Contains(after, "[redacted]") {
		t.Fatal("view depends on environment or exposes key", err)
	}
}
