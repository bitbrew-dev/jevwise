//go:build linux || darwin

package daemon

import (
	"os"
	"testing"
)

func TestReadAgentTokenRefusesNonprivateUnixModes(t *testing.T) {
	for _, mode := range []os.FileMode{0o644, 0o660, 0o400} {
		path := tokenFixture(t, "private-agent-token")
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if token, err := ReadAgentToken(path); err == nil || token != "" {
			t.Fatal("nonprivate token mode accepted", mode)
		}
	}
}
