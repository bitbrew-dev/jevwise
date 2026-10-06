//go:build linux || darwin || windows

package daemon

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureState() State {
	return State{Schema: 1, Address: "127.0.0.1:8080", Version: "dev", Instance: strings.Repeat("i", 32)}
}

func TestDecodeStateStrict(t *testing.T) {
	good, err := json.Marshal(fixtureState())
	if err != nil {
		t.Fatal(err)
	}
	if got, err := decodeState(good); err != nil || got != fixtureState() {
		t.Fatal("valid state rejected", err)
	}
	for _, version := range []string{"v0.0.1", "v1.2.3"} {
		state := fixtureState()
		state.Version = version
		data, _ := json.Marshal(state)
		if _, err := decodeState(data); err != nil {
			t.Fatal("valid version rejected", err)
		}
	}
	for _, input := range []string{
		"null", "[]", "{}", string(good) + "{}", string(good)[:len(good)-1],
		strings.Replace(string(good), `"schema":1`, `"schema":1,"schema":null`, 1),
		strings.Replace(string(good), `"schema":1`, `"schema":1,"schema":1`, 1),
		strings.Replace(string(good), `"schema":1`, `"Schema":1`, 1),
		strings.Replace(string(good), `"schema":1`, `"schema":"1"`, 1),
		strings.Replace(string(good), `"schema":1`, `"schema":1.0`, 1),
		strings.Replace(string(good), `"schema":1`, `"schema":2`, 1),
		strings.Replace(string(good), `"schema":1`, `"schema":1,"api_key":"secret"`, 1),
		strings.Replace(string(good), `"version":"dev"`, `"version":null`, 1),
		strings.Replace(string(good), `"version":"dev"`, `"version":"v01.2.3"`, 1),
		strings.Replace(string(good), `"version":"dev"`, `"version":"v1.2.3-beta"`, 1),
		strings.Replace(string(good), `"instance":"`+fixtureState().Instance+`"`, `"instance":"short"`, 1),
		strings.Repeat(" ", stateLimit+1), string([]byte{0xff}),
	} {
		if _, err := decodeState([]byte(input)); err == nil {
			t.Fatal("unsafe state accepted")
		}
	}
	for _, address := range []string{"0.0.0.0:8080", "localhost:8080", "127.0.0.1:0", "127.0.0.1:08080", "[::1%lo]:8080", "127.0.0.1:8080/mcp"} {
		state := fixtureState()
		state.Address = address
		data, _ := json.Marshal(state)
		if _, err := decodeState(data); err == nil {
			t.Fatal("unsafe address accepted")
		}
	}
	state := fixtureState()
	state.Address = "[::1]:8080"
	if state.validate() != nil {
		t.Fatal("IPv6 loopback rejected")
	}
}

func stateStore(t *testing.T) (*Store, string) {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "runtime")
	store, err := OpenStore(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, directory
}

func TestReadStateAbsentAndPartial(t *testing.T) {
	store, _ := stateStore(t)
	if _, _, err := Read(store); !errors.Is(err, ErrAbsent) {
		t.Fatal("missing state not distinguished", err)
	}
	if err := store.Create("control.key", []byte(strings.Repeat("k", 32))); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Read(store); err == nil || errors.Is(err, ErrAbsent) || err.Error() != "cannot access verified MCP instance state" {
		t.Fatal("partial state not safely rejected", err)
	}
	if _, _, err := Read(nil); err == nil || errors.Is(err, ErrAbsent) {
		t.Fatal("unavailable store treated as absent")
	}
}

func TestReadStateAndPrivateKey(t *testing.T) {
	for _, key := range []string{strings.Repeat("k", 32), strings.Repeat("-", 256), "short", strings.Repeat("k", 257), strings.Repeat("k", 32) + "\n"} {
		t.Run("key-length", func(t *testing.T) {
			store, _ := stateStore(t)
			data, _ := json.Marshal(fixtureState())
			if err := store.Create("state.json", data); err != nil {
				t.Fatal(err)
			}
			if err := store.Create("control.key", []byte(key)); err != nil {
				t.Fatal(err)
			}
			state, got, err := Read(store)
			if metadataID(key, 32, 256) {
				if err != nil || state != fixtureState() || got != key {
					t.Fatal("valid private state rejected", err)
				}
			} else if err == nil || state != (State{}) || got != "" || strings.Contains(err.Error(), key) {
				t.Fatal("unsafe key leaked or accepted")
			}
		})
	}
}

func TestReadStateBoundAndCorruption(t *testing.T) {
	for _, data := range [][]byte{[]byte(`{"api_key":"private-upstream-secret"}`), []byte(strings.Repeat("x", stateLimit+1))} {
		store, directory := stateStore(t)
		if err := store.Create("state.json", data); err != nil {
			t.Fatal(err)
		}
		if err := store.Create("control.key", []byte(strings.Repeat("k", 32))); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Read(store); err == nil || strings.Contains(err.Error(), "private-upstream-secret") {
			t.Fatal("corrupt metadata accepted or echoed")
		}
		if got, err := os.ReadFile(filepath.Join(directory, "state.json")); err != nil || string(got) != string(data) {
			t.Fatal("read modified unsafe metadata")
		}
	}
}
