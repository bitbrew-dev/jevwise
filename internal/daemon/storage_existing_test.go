//go:build linux || darwin

package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenExistingStoreAbsentHasNoEffects(t *testing.T) {
	base := t.TempDir()
	for _, path := range []string{filepath.Join(base, "runtime"), filepath.Join(base, "missing", "runtime")} {
		for range 3 {
			store, err := OpenExistingStore(path)
			if store != nil || !errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), path) {
				t.Fatalf("absence lost or disclosed: %v", err)
			}
		}
	}
	entries, err := os.ReadDir(base)
	if err != nil || len(entries) != 0 {
		t.Fatal("read-only inspection created runtime paths", err)
	}
}

func TestOpenExistingStoreBorrowedClosePreservesOwnership(t *testing.T) {
	owner, path := testStore(t)
	lease, err := owner.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if err := owner.Create("state.json", []byte("private-state")); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(filepath.Join(path, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	borrowed, err := OpenExistingStore(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := borrowed.Read("state.json", 100)
	if err != nil || string(data) != "private-state" {
		t.Fatal("existing state unavailable", err)
	}
	if err := borrowed.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Acquire(); !errors.Is(err, ErrLocked) {
		t.Fatal("closing borrowed store changed exclusive ownership", err)
	}
	after, err := os.Stat(filepath.Join(path, "state.json"))
	if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("existing state changed", err)
	}
	if _, err := owner.Read("state.json", 100); err != nil {
		t.Fatal("borrowed close invalidated owner's root", err)
	}
}

func TestOpenExistingStoreRejectsUnsafePathsWithoutRepair(t *testing.T) {
	for _, kind := range []string{"public", "file", "link", "relative", "unclean"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			path := filepath.Join(base, "runtime")
			switch kind {
			case "public":
				_ = os.Mkdir(path, 0o755)
			case "file":
				_ = os.WriteFile(path, []byte("private-secret"), 0o600)
			case "link":
				_ = os.Mkdir(path+"-target", 0o700)
				_ = os.Symlink(path+"-target", path)
			case "relative":
				path = "relative-runtime"
			case "unclean":
				path += string(os.PathSeparator)
			}
			before, _ := os.Lstat(path)
			store, err := OpenExistingStore(path)
			if err == nil || store != nil || strings.Contains(err.Error(), base) || strings.Contains(err.Error(), "private-secret") {
				t.Fatal("unsafe path accepted or disclosed", err)
			}
			after, _ := os.Lstat(path)
			if before != nil && (after == nil || !os.SameFile(before, after) || before.Mode() != after.Mode()) {
				t.Fatal("unsafe path repaired or replaced")
			}
		})
	}
}
