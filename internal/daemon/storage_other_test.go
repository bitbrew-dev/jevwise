//go:build !linux && !darwin

package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestUnsupportedStoreHasNoFilesystemEffects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private-runtime")
	if store, err := OpenStore(path); err == nil || store != nil {
		t.Fatal("unsupported storage accepted")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unsupported platform created runtime state", err)
	}
}
