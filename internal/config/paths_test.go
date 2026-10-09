package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

func TestLocalDiscoveryPrecedenceAndNoMerge(t *testing.T) {
	cleanEnv(t)
	global, _ := GlobalPath()
	writeConfig(t, global, "model='global'\napi_key='global-secret'")
	writeConfig(t, LocalName, "model='local'")
	cfg, err := Load("", nil)
	if err != nil || cfg.Model != "local" || cfg.APIKey != "" {
		t.Fatal("local file did not replace global", err)
	}
	t.Setenv("TS_JEV_MODEL", "environment")
	cfg, _ = Load("", nil)
	if cfg.Model != "environment" {
		t.Fatal("environment lost precedence")
	}
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("model", "", "")
	_ = flags.Parse([]string{"--model=flag"})
	cfg, err = Load("", flags)
	if err != nil || cfg.Model != "flag" {
		t.Fatal("flag lost precedence", err)
	}
	t.Setenv("TS_JEV_MODEL", "")
	cfg, err = Load(global, nil)
	if err != nil || cfg.Model != "global" {
		t.Fatal("explicit path lost precedence", err)
	}
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	if _, err := Load("", nil); err != nil {
		t.Fatal("local configuration requires home", err)
	}
}

func TestLocalDiscoveryFailsClosedAndDoesNotSearchAncestors(t *testing.T) {
	for _, mode := range []string{"invalid", "oversize", "directory", "link"} {
		t.Run(mode, func(t *testing.T) {
			cleanEnv(t)
			global, _ := GlobalPath()
			writeConfig(t, global, "model='global'")
			switch mode {
			case "invalid":
				writeConfig(t, LocalName, "api_key='private-secret'\n[")
			case "oversize":
				writeConfig(t, LocalName, strings.Repeat("x", MaxFileBytes+1))
			case "directory":
				_ = os.Mkdir(LocalName, 0700)
			case "link":
				if runtime.GOOS == "windows" {
					t.Skip("symlink creation may require privileges")
				}
				if err := os.Symlink(global, LocalName); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Load("", nil); err == nil || strings.Contains(err.Error(), "private-secret") {
				t.Fatal("unsafe local configuration fell back or leaked", err)
			}
		})
	}
	cleanEnv(t)
	writeConfig(t, LocalName, "model='ancestor'")
	// A nested directory of the current project must not inherit its file.
	cwd, _ := os.Getwd()
	child := filepath.Join(cwd, "child")
	_ = os.Mkdir(child, 0700)
	t.Chdir(child)
	cfg, err := Load("", nil)
	if err != nil || cfg.Model != "jev-latest" {
		t.Fatal("ancestor configuration loaded", err)
	}
}
