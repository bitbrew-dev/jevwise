//go:build linux || darwin || windows

package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func linkedFixture(t *testing.T) (*Store, *os.File, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runtime")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	file, err := privateCreate(store.root, "stage")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	if _, err := file.WriteString("complete private metadata"); err != nil {
		t.Fatal(err)
	}
	if err := store.root.Link("stage", "state.json"); err != nil {
		t.Fatal(err)
	}
	return store, file, path
}

func assertLinkedFilesUnchanged(t *testing.T, store *Store, before os.FileInfo, names ...string) {
	t.Helper()
	for _, name := range names {
		after, err := store.root.Lstat(name)
		if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
			t.Fatal("privacy check changed linked file", name, err)
		}
		data, err := store.root.ReadFile(name)
		if err != nil || string(data) != "complete private metadata" {
			t.Fatal("linked content changed", name, err)
		}
	}
}

func TestPrivateHandleLinksRequiresExactOwnedCount(t *testing.T) {
	store, file, _ := linkedFixture(t)
	before, _ := file.Stat()
	if !privateHandleLinks(file, 2) || privateHandle(file, false, false) || privateHandleLinks(file, 1) || privateHandleLinks(file, 0) || privateHandleLinks(file, 3) {
		t.Fatal("two-link privacy count incorrect")
	}
	assertLinkedFilesUnchanged(t, store, before, "stage", "state.json")
	if err := store.root.Link("stage", "foreign-alias"); err != nil {
		t.Fatal(err)
	}
	if privateHandleLinks(file, 2) || privateHandleLinks(file, 3) {
		t.Fatal("unexpected extra hardlink accepted")
	}
	assertLinkedFilesUnchanged(t, store, before, "stage", "state.json", "foreign-alias")
	if err := store.root.Remove("foreign-alias"); err != nil {
		t.Fatal(err)
	}
	if err := store.root.Remove("stage"); err != nil {
		t.Fatal(err)
	}
	if !privateHandle(file, false, false) || !privateHandleLinks(file, 1) || privateHandleLinks(file, 2) {
		t.Fatal("one-link privacy transition incorrect")
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if privateHandleLinks(file, 1) || privateHandleLinks(nil, 2) {
		t.Fatal("unavailable handle accepted")
	}
}
