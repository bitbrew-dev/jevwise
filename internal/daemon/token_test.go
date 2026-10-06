//go:build linux || darwin || windows

package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tokenFixture(t *testing.T, data string) string {
	t.Helper()
	parent := t.TempDir()
	root, err := os.OpenRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	file, err := privateCreate(root, "agent.token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(data); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(parent, "agent.token")
}

func TestReadAgentTokenTerminatorsAndLimits(t *testing.T) {
	for _, ending := range []string{"", "\n", "\r\n"} {
		for _, token := range []string{"private-agent-token", strings.Repeat("a", 4096)} {
			path := tokenFixture(t, token+ending)
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ReadAgentToken(path)
			if err != nil || got != token {
				t.Fatal("valid token file rejected", err)
			}
			after, err := os.Lstat(path)
			if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() {
				t.Fatal("token file changed", err)
			}
		}
	}
}

func TestReadAgentTokenInvalidDataAndPathsAreSafe(t *testing.T) {
	for _, data := range []string{"", "private token", "private\r", "private\n\n", "private-\xff", "private-秘密", strings.Repeat("p", 4097), strings.Repeat("p", 5000)} {
		path := tokenFixture(t, data)
		if token, err := ReadAgentToken(path); err == nil || token != "" || strings.Contains(err.Error(), "private") {
			t.Fatal("invalid token accepted or echoed", err)
		}
	}
	for _, path := range []string{"", "private-relative", t.TempDir(), filepath.Join(t.TempDir(), "private-missing")} {
		if token, err := ReadAgentToken(path); err == nil || token != "" || strings.Contains(err.Error(), "private") {
			t.Fatal("unsafe token path accepted or echoed", err)
		}
	}
}

func TestReadAgentTokenRejectsLinksAndClosedHandles(t *testing.T) {
	path := tokenFixture(t, "private-agent-token")
	hardlink := filepath.Join(filepath.Dir(path), "hardlink")
	if err := os.Link(path, hardlink); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{path, hardlink} {
		if token, err := ReadAgentToken(name); err == nil || token != "" {
			t.Fatal("multiply linked token accepted")
		}
	}
	if err := os.Remove(hardlink); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !privateHandle(file, false, true) {
		t.Fatal("private token handle rejected")
	}
	_ = file.Close()
	if privateHandle(file, false, true) {
		t.Fatal("closed token handle accepted")
	}
	link := filepath.Join(filepath.Dir(path), "symlink")
	if err := os.Symlink(path, link); err != nil {
		t.Skip("platform does not permit creating symlink fixtures")
	}
	if token, err := ReadAgentToken(link); err == nil || token != "" {
		t.Fatal("symlink token accepted")
	}
}

func TestReadAgentTokenCloseFailureDiscardsSecret(t *testing.T) {
	path := tokenFixture(t, "private-agent-token")
	cause := errors.New("private-close-error")
	token, err := readAgentToken(path, func(*os.File) error { return cause })
	if token != "" || err == nil || !errors.Is(err, cause) || strings.Contains(err.Error(), "private") {
		t.Fatal("close failure leaked secret or lost cause", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal("close failure left file handle open", err)
	}
}
