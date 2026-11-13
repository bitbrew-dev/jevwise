package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDebugFromConfigAndFlagOverride(t *testing.T) {
	configEnvironment(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	write := func(content string) {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("[general]\ndebug = true\n")
	for _, tt := range []struct {
		args    []string
		enabled bool
	}{
		{[]string{"version", "--config", path}, true},
		{[]string{"version", "--config", path, "--debug=false"}, false},
		{[]string{"version", "--config", path, "--verbose=false"}, false},
		{[]string{"version", "--config", "/missing"}, false},
	} {
		out, stderr, err := execute(NewRoot(), tt.args...)
		if err != nil || !strings.Contains(out, "commit:") || (stderr != "") != tt.enabled {
			t.Fatal("debug setting precedence changed", tt.args, err)
		}
	}
	write("[general]\ndebug = false\n")
	t.Setenv("TS_JEV_DEBUG", "true")
	if _, stderr, err := execute(NewRoot(), "version", "--config", path); err != nil || stderr == "" {
		t.Fatal("environment did not enable debug", err)
	}
	child := newMCPChild(nil)
	NewRoot().AddCommand(child)
	if debugEnabled(child) {
		t.Fatal("background child enabled debug from environment")
	}
	t.Setenv("TS_JEV_DEBUG", "")
	write("[general]\ndebug = 'maybe'\n")
	if out, stderr, err := execute(NewRoot(), "version", "--config", path); err != nil || stderr != "" || !strings.Contains(out, "commit:") {
		t.Fatal("invalid debug setting broke or logged version", err)
	}
}
