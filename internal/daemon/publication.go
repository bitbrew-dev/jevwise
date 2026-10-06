package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
)

const stageName = ".state-stage"

type ownedFile struct {
	store *Store
	file  *os.File
	info  os.FileInfo
	data  []byte
	names []string
}

// Publication owns only the exact state and key files it created.
// The caller must hold its Store lease throughout publication and cleanup.
type Publication struct {
	mu       sync.Mutex
	files    []*ownedFile
	closed   bool
	closeErr error
}

// Publish installs complete metadata without overwriting any existing entry.
// Filesystems without hard links fail safely. No readiness may precede success.
func Publish(store *Store, state State, controlToken string) (*Publication, error) {
	return publish(store, state, controlToken, fileOps{})
}

func publish(store *Store, state State, token string, ops fileOps) (publication *Publication, err error) {
	if state.validate() != nil || !metadataID(token, 32, 256) {
		return nil, &stateError{errors.New("invalid instance publication")}
	}
	if _, err := store.directoryInfo(true); err != nil {
		return nil, &stateError{err}
	}
	if _, err := store.root.Lstat("state.json"); !errors.Is(err, os.ErrNotExist) {
		return nil, &stateError{errors.Join(err, errors.New("instance state already exists"))}
	}
	publication = &Publication{}
	defer func() {
		if err != nil {
			err = &stateError{errors.Join(err, publication.Close())}
			publication = nil
		}
	}()
	key, err := createOwned(store, "control.key", []byte(token), ops)
	if key != nil {
		publication.files = append(publication.files, key)
	}
	if err != nil {
		return publication, err
	}
	if err := key.verify("control.key"); err != nil {
		return publication, err
	}
	data, err := json.Marshal(state)
	if err != nil || len(data) > stateLimit {
		return publication, errors.Join(err, errors.New("invalid instance publication"))
	}
	metadata, err := createOwned(store, stageName, data, ops)
	if metadata != nil {
		publication.files = append(publication.files, metadata)
	}
	if err != nil {
		return publication, err
	}
	if err := metadata.verify(stageName); err != nil {
		return publication, err
	}
	if err := store.root.Link(stageName, "state.json"); err != nil {
		return publication, err
	}
	metadata.names = append(metadata.names, "state.json")
	if err := metadata.remove(stageName); err != nil {
		return publication, err
	}
	if err := metadata.verify("state.json"); err != nil {
		return publication, err
	}
	if err := key.verify("control.key"); err != nil {
		return publication, err
	}
	return publication, nil
}

func createOwned(store *Store, name string, data []byte, ops fileOps) (owned *ownedFile, err error) {
	file, err := privateCreate(store.root, name)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	owned = &ownedFile{store: store, file: file, info: info, names: []string{name}}
	if err != nil || !privateHandle(file, false, false) {
		return owned, errors.Join(err, errors.New("unsafe created instance file"))
	}
	if ops.write == nil {
		ops.write = func(f *os.File, p []byte) (int, error) { return f.Write(p) }
	}
	if ops.sync == nil {
		ops.sync = func(f *os.File) error { return f.Sync() }
	}
	n, err := ops.write(file, data)
	if n < 0 || n > len(data) {
		return owned, errors.New("invalid instance write result")
	}
	owned.data = bytes.Clone(data[:n])
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = ops.sync(file)
	}
	if err != nil {
		return owned, err
	}
	// Keep a verified read handle pinned before closing the durable writer.
	pin, err := store.root.OpenFile(name, privateReadFlags(), 0)
	if err != nil {
		return owned, err
	}
	if opened, statErr := pin.Stat(); statErr != nil || !privateHandle(pin, false, false) || !os.SameFile(info, opened) {
		_ = pin.Close()
		return owned, errors.Join(statErr, errors.New("instance file changed"))
	}
	owned.file = pin
	return owned, closeRuntimeFile(file, ops.close)
}

func (o *ownedFile) verify(name string) error {
	if _, err := o.store.directoryInfo(true); err != nil {
		return err
	}
	current, err := o.store.root.Lstat(name)
	if err != nil || o.info == nil || !os.SameFile(o.info, current) || !current.Mode().IsRegular() {
		return errors.Join(err, errors.New("instance file identity changed"))
	}
	for _, alias := range o.names {
		entry, err := o.store.root.Lstat(alias)
		if err != nil || !os.SameFile(o.info, entry) {
			return errors.Join(err, errors.New("instance alias identity changed"))
		}
	}
	if !privateHandleLinks(o.file, uint32(len(o.names))) {
		return errors.New("instance file privacy changed")
	}
	if _, err := o.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(o.file, stateLimit+1))
	if err != nil || !bytes.Equal(data, o.data) {
		return errors.Join(err, errors.New("instance file content changed"))
	}
	return nil
}

func (o *ownedFile) remove(name string) error {
	if err := o.verify(name); err != nil {
		return err
	}
	if err := o.store.root.Remove(name); err != nil {
		return err
	}
	for i, alias := range o.names {
		if alias == name {
			o.names = append(o.names[:i], o.names[i+1:]...)
			break
		}
	}
	return nil
}

// Close never removes exchanged files, modified contents or unfamiliar entries.
func (p *Publication) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return p.closeErr
	}
	p.closed = true
	var err error
	for i := len(p.files) - 1; i >= 0; i-- {
		file := p.files[i]
		for _, name := range append([]string(nil), file.names...) {
			err = errors.Join(err, file.remove(name))
		}
		err = errors.Join(err, file.file.Close())
	}
	if err != nil {
		p.closeErr = &stateError{err}
	}
	return p.closeErr
}
