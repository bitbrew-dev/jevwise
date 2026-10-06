//go:build windows

package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadAgentTokenRefusesWindowsChmodOnlyProtection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.token")
	if err := os.WriteFile(path, []byte("private-agent-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	if token, err := ReadAgentToken(path); err == nil || token != "" {
		t.Fatal("inherited Windows ACL accepted merely because mode was 0600")
	}
}
