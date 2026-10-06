//go:build linux || darwin || windows

package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func published(t *testing.T) (*Publication, *Store, string) {
	t.Helper()
	store, directory := stateStore(t)
	lease, err := store.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lease.Close() })
	publication, err := Publish(store, fixtureState(), strings.Repeat("k", 32))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = publication.Close() })
	return publication, store, directory
}

func TestPublicationCompleteAndCleanup(t *testing.T) {
	publication, store, directory := published(t)
	state, token, err := Read(store)
	if err != nil || state != fixtureState() || token != strings.Repeat("k", 32) {
		t.Fatal("published state unavailable", err)
	}
	if _, err := os.Lstat(filepath.Join(directory, stageName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("staging entry retained", err)
	}
	if err := publication.Close(); err != nil {
		t.Fatal(err)
	}
	if err := publication.Close(); err != nil {
		t.Fatal("close not idempotent", err)
	}
	if _, _, err := Read(store); !errors.Is(err, ErrAbsent) {
		t.Fatal("owned files retained", err)
	}
	if err := (*Publication)(nil).Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPublicationPreservesExistingEntries(t *testing.T) {
	for _, name := range []string{"state.json", "control.key", stageName} {
		store, directory := stateStore(t)
		foreign := []byte("foreign entry must remain")
		if err := store.Create(name, foreign); err != nil {
			t.Fatal(err)
		}
		publication, err := Publish(store, fixtureState(), strings.Repeat("k", 32))
		if err == nil || publication != nil || err.Error() != "cannot access verified MCP instance state" {
			t.Fatal("existing entry accepted", err)
		}
		if got, err := os.ReadFile(filepath.Join(directory, name)); err != nil || string(got) != string(foreign) {
			t.Fatal("foreign entry changed")
		}
		if name == stageName {
			if _, err := os.Lstat(filepath.Join(directory, "control.key")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("failed publication retained owned key")
			}
		}
	}
}

func TestPublicationCleanupRejectsChangedEntries(t *testing.T) {
	for _, name := range []string{"state.json", "control.key"} {
		for _, exchange := range []bool{false, true} {
			publication, _, directory := published(t)
			path := filepath.Join(directory, name)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if exchange {
				if err := os.Rename(path, path+".original"); err != nil {
					t.Fatal(err)
				}
			} else {
				data = []byte("foreign modified contents")
			}
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := publication.Close(); err == nil {
				t.Fatal("changed entry removed")
			}
			if got, err := os.ReadFile(path); err != nil || string(got) != string(data) {
				t.Fatal("unfamiliar entry changed")
			}
		}
	}
}

func TestPublicationWriteSyncCloseFailures(t *testing.T) {
	for _, phase := range []string{"write", "sync", "close"} {
		for _, failingFile := range []int{1, 2} {
			store, directory := stateStore(t)
			calls := 0
			failure := errors.New("private injected failure")
			ops := fileOps{}
			switch phase {
			case "write":
				ops.write = func(file *os.File, data []byte) (int, error) {
					calls++
					if calls == failingFile {
						return file.Write(data[:1])
					}
					return file.Write(data)
				}
			case "sync":
				ops.sync = func(file *os.File) error {
					calls++
					if calls == failingFile {
						return failure
					}
					return file.Sync()
				}
			case "close":
				ops.close = func(file *os.File) error {
					calls++
					if calls == failingFile {
						return failure
					}
					return file.Close()
				}
			}
			publication, err := publish(store, fixtureState(), strings.Repeat("k", 32), ops)
			if err == nil || publication != nil || strings.Contains(err.Error(), "private injected") {
				t.Fatal("failure accepted or leaked")
			}
			for _, name := range []string{"state.json", "control.key", stageName} {
				if _, err := os.Lstat(filepath.Join(directory, name)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("failed publication retained owned file", name)
				}
			}
		}
	}
}

func TestPublicationRacingDestinationNeverOverwritten(t *testing.T) {
	store, directory := stateStore(t)
	calls := 0
	ops := fileOps{close: func(file *os.File) error {
		calls++
		if calls == 2 {
			if err := store.Create("state.json", []byte("foreign destination")); err != nil {
				return err
			}
		}
		return file.Close()
	}}
	if publication, err := publish(store, fixtureState(), strings.Repeat("k", 32), ops); err == nil || publication != nil {
		t.Fatal("racing destination replaced")
	}
	if got, err := os.ReadFile(filepath.Join(directory, "state.json")); err != nil || string(got) != "foreign destination" {
		t.Fatal("racing foreign state changed")
	}
	for _, name := range []string{"control.key", stageName} {
		if _, err := os.Lstat(filepath.Join(directory, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("failed installation retained owned file")
		}
	}
}

func TestPublicationRollbackDropsRemovedAlias(t *testing.T) {
	store, directory := stateStore(t)
	metadata, err := createOwned(store, stageName, []byte("owned metadata"), fileOps{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.root.Link(stageName, "state.json"); err != nil {
		_ = metadata.file.Close()
		t.Fatal(err)
	}
	metadata.names = append(metadata.names, "state.json")
	publication := &Publication{files: []*ownedFile{metadata}}
	if err := publication.Close(); err != nil {
		t.Fatal("two-link rollback failed", err)
	}
	if err := publication.Close(); err != nil {
		t.Fatal("two-link rollback close not idempotent", err)
	}
	for _, name := range []string{stageName, "state.json"} {
		if _, err := os.Lstat(filepath.Join(directory, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("rollback retained owned alias")
		}
	}
}
