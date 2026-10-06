//go:build linux || darwin

package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPublicationTransientPrivacyChangePreservesBothAliases(t *testing.T) {
	metadata, directory := transientMetadata(t)
	if err := os.Chmod(filepath.Join(directory, stageName), 0o644); err != nil {
		t.Fatal(err)
	}
	publication := &Publication{files: []*ownedFile{metadata}}
	if err := publication.Close(); err == nil {
		t.Fatal("two-link privacy change accepted")
	}
	for _, name := range []string{stageName, "state.json"} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Fatal("unsafe alias removed")
		}
	}
}
