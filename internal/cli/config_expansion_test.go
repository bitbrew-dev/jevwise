package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDecideRejectsMissingOrBlankExpandedKey(t *testing.T) {
	configEnvironment(t)
	const name = "JEVWISE_TEST_DECISION_KEY"
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("api_key = '${"+name+"}'"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", " \t\n "} {
		t.Setenv(name, value)
		out, stderr, err := execute(NewRoot(), "decide", "--config", path, "--prompt", "synthetic", "--option", "A", "--option", "B")
		if cause := errors.Unwrap(err); cause == nil || cause.Error() != "API key is required" || out != "" || stderr != "" {
			t.Fatal("missing expanded key not rejected at service initialization", err)
		}
	}
}
