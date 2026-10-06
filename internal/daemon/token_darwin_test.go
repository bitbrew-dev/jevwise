//go:build darwin

package daemon

import "testing"

func TestReadAgentTokenRefusesIndependentDarwinACL(t *testing.T) {
	path := tokenFixture(t, "private-agent-token")
	addACL(t, path, "read,readattr,readextattr,readsecurity")
	if token, err := ReadAgentToken(path); err == nil || token != "" {
		t.Fatal("ACL exposed token despite 0600 mode")
	}
}
