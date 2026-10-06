//go:build linux || darwin

package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
)

func testStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runtime")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, path
}

func TestStoreRefusesUnsafeDirectoriesWithoutRepair(t *testing.T) {
	for _, kind := range []string{"public", "file", "link", "missing-parent", "relative", "unclean"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			path := filepath.Join(base, "runtime")
			switch kind {
			case "public":
				_ = os.Mkdir(path, 0o755)
			case "file":
				_ = os.WriteFile(path, []byte("secret"), 0o600)
			case "link":
				_ = os.Mkdir(path+"-target", 0o700)
				_ = os.Symlink(path+"-target", path)
			case "missing-parent":
				path = filepath.Join(path, "child")
			case "relative":
				path = "relative-runtime"
			case "unclean":
				path += string(os.PathSeparator)
			}
			before, _ := os.Lstat(path)
			store, err := OpenStore(path)
			if err == nil || store != nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), base) {
				t.Fatal("unsafe directory accepted or leaked", err)
			}
			after, _ := os.Lstat(path)
			if before != nil && (!os.SameFile(before, after) || before.Mode() != after.Mode()) {
				t.Fatal("unsafe existing path changed")
			}
		})
	}
}

func TestLeaseExclusiveAndIdentityCleanup(t *testing.T) {
	store, path := testStore(t)
	lease, err := store.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Acquire(); !errors.Is(err, ErrLocked) {
		t.Fatal("duplicate ownership accepted", err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal("close not idempotent", err)
	}
	lease, err = store.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(path, lockName), filepath.Join(path, "old-lock")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(path, lockName), 0o700); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(filepath.Join(path, lockName))
	if lease.Close() == nil {
		t.Fatal("exchanged lock removed")
	}
	if _, err := lease.root.Open("."); !errors.Is(err, os.ErrClosed) {
		t.Fatal("failed cleanup leaked pinned root", err)
	}
	if lease.Close() == nil {
		t.Fatal("repeated close lost failure")
	}
	after, _ := os.Stat(filepath.Join(path, lockName))
	if !os.SameFile(before, after) {
		t.Fatal("foreign lock changed")
	}
}

func TestConcurrentAcquireHasOneOwner(t *testing.T) {
	store, _ := testStore(t)
	var group sync.WaitGroup
	leases := make(chan *Lease, 8)
	for range 8 {
		group.Go(func() {
			lease, err := store.Acquire()
			if err != nil && !errors.Is(err, ErrLocked) {
				t.Error(err)
			}
			if lease != nil {
				leases <- lease
			}
		})
	}
	group.Wait()
	close(leases)
	count := 0
	for lease := range leases {
		count++
		_ = lease.Close()
	}
	if count != 1 {
		t.Fatalf("owners=%d", count)
	}
}

type storageInfo struct {
	os.FileInfo
	stat any
}

func (i storageInfo) Sys() any { return i.stat }

func TestStorageOwnerChecksAndClosedHandle(t *testing.T) {
	store, path := testStore(t)
	info, _ := os.Stat(path)
	stat := *info.Sys().(*syscall.Stat_t)
	stat.Uid++
	if privateInfo(storageInfo{info, &stat}, true) || privateInfo(storageInfo{info, nil}, true) {
		t.Fatal("foreign/unknown owner accepted")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Acquire(); err == nil {
		t.Fatal("closed store accepted lock")
	}
	if _, err := (*Store)(nil).Acquire(); err == nil {
		t.Fatal("nil store accepted lock")
	}
}

func TestLeasePreservesPreexistingAndNonemptyLocks(t *testing.T) {
	for _, kind := range []string{"directory", "file", "link"} {
		t.Run(kind, func(t *testing.T) {
			store, path := testStore(t)
			lock := filepath.Join(path, lockName)
			switch kind {
			case "directory":
				_ = os.Mkdir(lock, 0o700)
			case "file":
				_ = os.WriteFile(lock, []byte("private stale marker"), 0o600)
			case "link":
				_ = os.Symlink("unverified-target", lock)
			}
			before, _ := os.Lstat(lock)
			if _, err := store.Acquire(); !errors.Is(err, ErrLocked) {
				t.Fatal("preexisting ownership accepted", err)
			}
			after, _ := os.Lstat(lock)
			if !os.SameFile(before, after) || before.Mode() != after.Mode() {
				t.Fatal("preexisting lock changed")
			}
		})
	}
	store, path := testStore(t)
	lease, err := store.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(path, lockName, "foreign-marker")
	if err := os.WriteFile(marker, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := lease.Close(); !errors.Is(err, syscall.ENOTEMPTY) {
		t.Fatal("nonempty lock removed or cause lost", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("foreign marker removed", err)
	}
}

func TestStoreRevalidatesDirectoryPrivacy(t *testing.T) {
	store, path := testStore(t)
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Acquire(); err == nil {
		t.Fatal("changed directory privacy accepted")
	}
	if _, err := os.Stat(filepath.Join(path, lockName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unsafe directory mutated", err)
	}
}
