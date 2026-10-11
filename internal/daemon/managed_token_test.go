//go:build linux || darwin || windows

package daemon

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestManagedTokenPersistsAndIsNotInstanceState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "runtime")
	token, err := EnsureAgentToken(dir)
	data, decodeErr := hex.DecodeString(token)
	if err != nil || decodeErr != nil || len(data) != 32 {
		t.Fatal("invalid generated token", err)
	}
	store, err := OpenExistingStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	lease, err := store.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	// Reuse is allowed even while the background instance holds its lease.
	if got, err := ensureAgentToken(dir, nil, fileOps{}); err != nil || got != token {
		t.Fatal("persisted token was not reused", err)
	}
	if _, _, err := Read(store); err != ErrAbsent {
		t.Fatal("agent token became background instance state", err)
	}
	if got, err := ReadAgentToken(filepath.Join(dir, AgentTokenName)); err != nil || got != token {
		t.Fatal("saved token is not owner-private", err)
	}
}

func TestManagedTokenFailuresDoNotOverwriteOrLeak(t *testing.T) {
	for _, kind := range []string{"invalid", "stage", "locked", "entropy", "write", "sync", "close", "collision"} {
		t.Run(kind, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "runtime")
			store, err := OpenStore(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			var ops fileOps
			random := io.Reader(bytes.NewReader(bytes.Repeat([]byte{1}, 32)))
			name := ""
			switch kind {
			case "invalid", "stage":
				name = AgentTokenName
				if kind == "stage" {
					name = agentStageName
				}
				file, err := privateCreate(store.root, name)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = file.WriteString("private invalid")
				_ = file.Close()
			case "locked":
				lease, err := store.Acquire()
				if err != nil {
					t.Fatal(err)
				}
				defer lease.Close()
			case "entropy":
				random = strings.NewReader("short")
			case "write":
				ops.write = func(*os.File, []byte) (int, error) { return 0, nil }
			case "sync":
				ops.sync = func(*os.File) error { return errors.New("private sync failure") }
			case "close":
				ops.close = func(*os.File) error { return errors.New("private close failure") }
			case "collision":
				name = AgentTokenName
				ops.sync = func(*os.File) error {
					file, err := privateCreate(store.root, name)
					if err != nil {
						return err
					}
					_, err = file.WriteString("private invalid")
					return errors.Join(err, file.Close())
				}
			}
			if token, err := ensureAgentToken(dir, random, ops); err == nil || token != "" || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), dir) {
				t.Fatal("failure accepted or exposed private input", err)
			}
			if name == "" && kind != "locked" {
				entries, _ := os.ReadDir(dir)
				if len(entries) != 0 {
					t.Fatal("failed generation left token, staging or lock entries")
				}
			}
			if name != "" {
				data, _ := store.root.ReadFile(name)
				if string(data) != "private invalid" {
					t.Fatal("existing entry changed")
				}
			}
		})
	}
}

func TestManagedTokenConcurrentInitializationNeverDisagrees(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "runtime")
	var group sync.WaitGroup
	tokens := make(chan string, 8)
	for range 8 {
		group.Go(func() {
			if token, err := EnsureAgentToken(dir); err == nil {
				tokens <- token
			}
		})
	}
	group.Wait()
	close(tokens)
	want, err := EnsureAgentToken(dir)
	if err != nil || len(tokens) == 0 {
		t.Fatal("concurrent initialization did not publish a token", err)
	}
	for token := range tokens {
		if token != want {
			t.Fatal("concurrent initializers disagreed")
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != AgentTokenName {
		t.Fatal("initializer left staging or lock entries")
	}
}
