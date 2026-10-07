package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"unicode/utf8"

	"github.com/bitbrew-dev/jevwise/internal/mcpserver"
	"github.com/bitbrew-dev/jevwise/internal/update"
)

const stateLimit = 4 << 10

// ErrAbsent means neither state nor management key exists. Partial state is unsafe.
var ErrAbsent = errors.New("MCP background instance is not running")

type stateError struct{ cause error }

func (e *stateError) Error() string { return "cannot access verified MCP instance state" }
func (e *stateError) Unwrap() error { return e.cause }

// State contains only public metadata, never credentials.
// PID is inspection-only, not proof of process ownership or liveness.
type State struct {
	Schema   int    `json:"schema"`
	Address  string `json:"address"`
	Version  string `json:"version"`
	Instance string `json:"instance"`
	PID      int    `json:"pid,omitempty"`
}

func metadataID(value string, min, max int) bool {
	if len(value) < min || len(value) > max {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func (s State) validate() error {
	address, err := mcpserver.ValidateAddress(s.Address)
	_, versionErr := update.Compare(s.Version, s.Version)
	schemaValid := (s.Schema == 1 && s.PID == 0) || (s.Schema == 2 && s.PID > 0 && uint64(s.PID) <= 1<<32-1)
	if !schemaValid || err != nil || address != s.Address || !metadataID(s.Instance, 16, 128) || len(s.Version) > 128 || (s.Version != "dev" && versionErr != nil) {
		return errors.New("invalid instance metadata")
	}
	return nil
}

// Read loads bounded, private metadata and its separate management key.
// Presence alone never establishes that an instance is alive or owned.
func Read(store *Store) (State, string, error) {
	data, stateErr := store.Read("state.json", stateLimit)
	key, keyErr := store.Read("control.key", stateLimit)
	if errors.Is(stateErr, os.ErrNotExist) && errors.Is(keyErr, os.ErrNotExist) {
		return State{}, "", ErrAbsent
	}
	if stateErr != nil || keyErr != nil {
		return State{}, "", &stateError{errors.Join(stateErr, keyErr)}
	}
	state, err := decodeState(data)
	if err != nil || !metadataID(string(key), 32, 256) {
		return State{}, "", &stateError{errors.Join(err, errors.New("invalid private instance state"))}
	}
	return state, string(key), nil
}

func decodeState(data []byte) (State, error) {
	fail := func() (State, error) { return State{}, errors.New("invalid instance metadata") }
	if len(data) == 0 || len(data) > stateLimit || !utf8.Valid(data) {
		return fail()
	}
	d := json.NewDecoder(bytes.NewReader(data))
	if token, err := d.Token(); err != nil || token != json.Delim('{') {
		return fail()
	}
	var state State
	seen := make(map[string]bool)
	for d.More() {
		token, err := d.Token()
		name, ok := token.(string)
		if err != nil || !ok || seen[name] {
			return fail()
		}
		seen[name] = true
		var raw json.RawMessage
		if d.Decode(&raw) != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fail()
		}
		var target any
		switch name {
		case "schema":
			target = &state.Schema
		case "address":
			target = &state.Address
		case "version":
			target = &state.Version
		case "instance":
			target = &state.Instance
		case "pid":
			target = &state.PID
		default:
			return fail()
		}
		if json.Unmarshal(raw, target) != nil {
			return fail()
		}
	}
	expectedFields := 4
	if state.Schema == 2 {
		expectedFields = 5
	}
	if token, err := d.Token(); err != nil || token != json.Delim('}') || len(seen) != expectedFields || seen["pid"] != (state.Schema == 2) {
		return fail()
	}
	if _, err := d.Token(); err != io.EOF || state.validate() != nil {
		return fail()
	}
	return state, nil
}
