// Package daemon owns private local runtime state, never upstream credentials.
package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
)

const lockName = ".instance-lock"

// ErrLocked requires inspection rather than automatic stale ownership removal.
var ErrLocked = errors.New("MCP runtime ownership already exists; inspect stale state manually")

type storageError struct{ cause error }

func (e *storageError) Error() string { return "cannot access private MCP runtime storage" }
func (e *storageError) Unwrap() error { return e.cause }

// Store holds a private root. Its parent path is a caller-trusted initial anchor.
// Confinement does not defend against hostile mounts or directory renaming.
type Store struct{ root *os.Root }

// OpenStore creates only the private directory leaf. It never repairs unsafe state.
func OpenStore(directory string) (*Store, error) { return openStore(directory, true) }

// OpenExistingStore opens validated existing storage without creating any path.
// A missing directory remains absent and is inspectable with os.ErrNotExist.
// Opening and closing never acquire ownership or remove runtime state.
func OpenExistingStore(directory string) (*Store, error) { return openStore(directory, false) }

func openStore(directory string, create bool) (*Store, error) {
	if err := storageSupported(); err != nil {
		return nil, &storageError{err}
	}
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return nil, &storageError{errors.New("runtime path must be absolute")}
	}
	if create {
		if err := mkdirPrivate(directory); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, &storageError{err}
		}
	}
	before, err := os.Lstat(directory)
	if err != nil || !privateInfo(before, true) {
		return nil, &storageError{errors.Join(err, errors.New("unsafe runtime directory"))}
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, &storageError{err}
	}
	store := &Store{root}
	opened, err := store.directoryInfo(true)
	if err != nil || !os.SameFile(before, opened) {
		_ = root.Close()
		return nil, &storageError{errors.Join(err, errors.New("runtime directory changed"))}
	}
	return store, nil
}

func (s *Store) directoryInfo(protected bool) (os.FileInfo, error) {
	if s == nil || s.root == nil {
		return nil, errors.New("runtime store unavailable")
	}
	file, err := s.root.Open(".")
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err == nil && !privateHandle(file, true, protected) {
		err = errors.New("runtime directory is no longer private")
	}
	err = errors.Join(err, file.Close())
	return info, err
}

// Close releases the root handle, not files, keys, locks or existing state.
func (s *Store) Close() error {
	if s == nil || s.root == nil {
		return nil
	}
	if err := s.root.Close(); err != nil {
		return &storageError{err}
	}
	return nil
}

// Lease owns one exclusively created lock directory, not a PID.
type Lease struct {
	store    *Store
	root     *os.Root
	info     os.FileInfo
	mu       sync.Mutex
	closed   bool
	closeErr error
}

func (s *Store) Acquire() (*Lease, error) {
	if _, err := s.directoryInfo(true); err != nil {
		return nil, &storageError{err}
	}
	if err := mkdirChildPrivate(s.root, lockName); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, ErrLocked
		}
		return nil, &storageError{err}
	}
	info, err := s.root.Lstat(lockName)
	if err != nil || !privateInfo(info, true) {
		return nil, &storageError{errors.Join(err, errors.New("unsafe created runtime lock"))}
	}
	root, err := s.root.OpenRoot(lockName)
	if err != nil {
		return nil, &storageError{err}
	}
	opened, err := (&Store{root}).directoryInfo(false)
	if err != nil || !os.SameFile(info, opened) {
		_ = root.Close()
		return nil, &storageError{errors.Join(err, errors.New("runtime lock changed"))}
	}
	return &Lease{store: s, root: root, info: opened}, nil
}

// Close removes only this lease's unchanged empty lock. Successful close is idempotent.
func (l *Lease) Close() (err error) {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return l.closeErr
	}
	l.closed = true
	defer func() {
		if l.root != nil {
			err = errors.Join(err, l.root.Close())
		}
		if err != nil {
			err = &storageError{err}
		}
		l.closeErr = err
	}()
	if _, err := l.store.directoryInfo(true); err != nil {
		return err
	}
	info, err := l.store.root.Lstat(lockName)
	if err != nil || !privateInfo(info, true) || !os.SameFile(l.info, info) {
		return errors.Join(err, errors.New("runtime lock identity changed"))
	}
	return l.store.root.Remove(lockName)
}
