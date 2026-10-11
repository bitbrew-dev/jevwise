package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// AgentTokenName is persistent authentication, not per-instance management state.
const AgentTokenName = "agent.token"
const agentStageName = ".agent-token-stage"

type managedTokenError struct{ cause error }

func (e *managedTokenError) Error() string {
	return "cannot initialize protected MCP token; inspect runtime storage or use --token-file"
}
func (e *managedTokenError) Unwrap() error { return e.cause }

// EnsureAgentToken reuses a protected token or exclusively publishes 256 random
// bits. It never replaces existing entries or repairs privacy. Runtime parents
// must be trusted; unsupported hard links and concurrent initialization fail closed.
func EnsureAgentToken(directory string) (string, error) {
	return ensureAgentToken(directory, rand.Reader, fileOps{})
}

func ensureAgentToken(directory string, random io.Reader, ops fileOps) (token string, err error) {
	defer func() {
		if err != nil {
			token, err = "", &managedTokenError{err}
		}
	}()
	store, err := OpenStore(directory)
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, store.Close()) }()
	path := filepath.Join(directory, AgentTokenName)
	if token, err = ReadAgentToken(path); err == nil || !errors.Is(err, os.ErrNotExist) {
		return token, err
	}
	lease, err := store.Acquire()
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, lease.Close()) }()
	// Another cooperative initializer may have completed before acquiring the lease.
	if token, err = ReadAgentToken(path); err == nil || !errors.Is(err, os.ErrNotExist) {
		return token, err
	}
	var entropy [32]byte
	if random == nil {
		return "", errors.New("random source unavailable")
	}
	if _, err := io.ReadFull(random, entropy[:]); err != nil {
		return "", err
	}
	token = hex.EncodeToString(entropy[:])
	stage, err := createOwned(store, agentStageName, []byte(token), ops)
	if stage != nil {
		defer func() {
			for _, name := range stage.names {
				if name == agentStageName {
					err = errors.Join(err, stage.remove(name))
				}
			}
			err = errors.Join(err, stage.file.Close())
		}()
	}
	if err != nil {
		return "", err
	}
	if err := stage.verify(agentStageName); err != nil {
		return "", err
	}
	if err := store.root.Link(agentStageName, AgentTokenName); err != nil {
		return "", err
	}
	stage.names = append(stage.names, AgentTokenName)
	if err := stage.remove(agentStageName); err != nil {
		return "", err
	}
	return token, stage.verify(AgentTokenName)
}
