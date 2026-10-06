//go:build linux || darwin || windows

package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicationInvalidInputDoesNotCreateState(t *testing.T) {
	store, directory := stateStore(t)
	for _, token := range []string{"", "upstream-secret\n"} {
		if publication, err := Publish(store, fixtureState(), token); err == nil || publication != nil {
			t.Fatal("invalid token accepted")
		}
	}
	state := fixtureState()
	state.Address = "0.0.0.0:8080"
	if publication, err := Publish(store, state, strings.Repeat("k", 32)); err == nil || publication != nil {
		t.Fatal("invalid state accepted")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatal("invalid input changed storage")
	}
}

func TestPublicationUnexpectedHardLinkIsPreserved(t *testing.T) {
	publication, _, directory := published(t)
	state := filepath.Join(directory, "state.json")
	alias := filepath.Join(directory, "foreign-link")
	if err := os.Link(state, alias); err != nil {
		t.Fatal(err)
	}
	if err := publication.Close(); err == nil {
		t.Fatal("unexpected link accepted")
	}
	for _, name := range []string{state, alias} {
		if _, err := os.Stat(name); err != nil {
			t.Fatal("unexpected linked metadata removed")
		}
	}
}

func transientMetadata(t *testing.T) (*ownedFile, string) {
	t.Helper()
	store, directory := stateStore(t)
	metadata, err := createOwned(store, stageName, []byte("owned metadata"), fileOps{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = metadata.file.Close() })
	if err := store.root.Link(stageName, "state.json"); err != nil {
		t.Fatal(err)
	}
	metadata.names = append(metadata.names, "state.json")
	return metadata, directory
}

func TestPublicationTransientExchangedAliasPreservesAllEntries(t *testing.T) {
	metadata, directory := transientMetadata(t)
	stage := filepath.Join(directory, stageName)
	unknown := filepath.Join(directory, "unknown-alias")
	if err := os.Rename(stage, unknown); err != nil {
		t.Fatal(err)
	}
	if err := metadata.store.Create(stageName, []byte("foreign staging file")); err != nil {
		t.Fatal(err)
	}
	publication := &Publication{files: []*ownedFile{metadata}}
	if err := publication.Close(); err == nil {
		t.Fatal("exchanged alias with two links accepted")
	}
	for _, name := range []string{stageName, "state.json", "unknown-alias"} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Fatal("unverified alias removed")
		}
	}
}
