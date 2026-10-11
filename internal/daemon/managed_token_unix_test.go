//go:build linux || darwin

package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManagedTokenRefusesUnsafeExistingEntries(t *testing.T) {
	for _, kind := range []string{"public-directory", "public-file", "symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "runtime")
			if _, err := EnsureAgentToken(dir); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, AgentTokenName)
			switch kind {
			case "public-directory":
				_ = os.Chmod(dir, 0755)
			case "public-file":
				_ = os.Chmod(path, 0644)
			case "symlink":
				if err := os.Rename(path, path+"-original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path+"-original", path); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(path, path+"-extra"); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := os.Lstat(path)
			if token, err := EnsureAgentToken(dir); err == nil || token != "" {
				t.Fatal("unsafe entry accepted")
			}
			after, _ := os.Lstat(path)
			if !os.SameFile(before, after) || before.Mode() != after.Mode() {
				t.Fatal("unsafe entry repaired or replaced")
			}
		})
	}
}
