//go:build linux || darwin || windows

package daemon

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestDecodeStateDiagnosticPID(t *testing.T) {
	state := fixtureState()
	state.Schema, state.PID = 2, 1234
	good, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := decodeState(good); err != nil || got != state {
		t.Fatal("PID state did not round-trip", err)
	}
	for _, pid := range []int{1, os.Getpid(), 1<<32 - 1} {
		state.PID = pid
		data, _ := json.Marshal(state)
		if got, err := decodeState(data); err != nil || got != state {
			t.Fatal("valid diagnostic PID rejected", err)
		}
	}
	for _, raw := range []string{"0", "-1", "4294967296", "9223372036854775808", "1.0", "1e2", `"1234"`, "true", "null", "{}", "[]"} {
		data := strings.Replace(string(good), `"pid":1234`, `"pid":`+raw, 1)
		if _, err := decodeState([]byte(data)); err == nil {
			t.Fatalf("unsafe PID accepted: %s", raw)
		}
	}
	for _, data := range []string{
		strings.Replace(string(good), `,"pid":1234`, "", 1),
		strings.Replace(string(good), `"pid":1234`, `"pid":1234,"pid":1234`, 1),
		strings.Replace(string(good), `"pid":1234`, `"PID":1234`, 1),
		strings.Replace(string(good), `"version":"dev",`, "", 1),
		strings.Replace(string(good), `"schema":2`, `"schema":1`, 1),
		strings.Replace(string(good), `"schema":2`, `"schema":3`, 1),
	} {
		if _, err := decodeState([]byte(data)); err == nil {
			t.Fatal("inconsistent PID schema accepted")
		}
	}
	legacy, _ := json.Marshal(fixtureState())
	if got, err := decodeState(legacy); err != nil || got.PID != 0 || strings.Contains(string(legacy), "pid") {
		t.Fatal("legacy state compatibility changed", err)
	}
	if _, err := decodeState([]byte(strings.Replace(string(legacy), `"schema":1`, `"schema":1,"pid":0`, 1))); err == nil {
		t.Fatal("legacy schema accepted an explicit PID field")
	}
}

func TestPIDStatePublicationAndOwnedCleanup(t *testing.T) {
	store, _ := stateStore(t)
	lease, err := store.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	state := fixtureState()
	state.Schema, state.PID = 2, os.Getpid()
	key := strings.Repeat("k", 32)
	publication, err := Publish(store, state, key)
	if err != nil {
		t.Fatal(err)
	}
	defer publication.Close()
	if got, gotKey, err := Read(store); err != nil || got != state || gotKey != key {
		t.Fatal("private PID state could not be read", err)
	}
	if err := publication.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Read(store); !errors.Is(err, ErrAbsent) {
		t.Fatal("owned cleanup left PID metadata", err)
	}
}
