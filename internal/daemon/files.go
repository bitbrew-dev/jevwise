package daemon

import (
	"errors"
	"io"
	"os"
)

const fileLimit = 64 << 10

func storageName(name string) bool {
	return name == "control.key" || name == "state.json" || name == "daemon.log" || name == ".state-stage"
}

func closeRuntimeFile(file *os.File, closeFn func(*os.File) error) error {
	if closeFn == nil {
		return file.Close()
	}
	err := closeFn(file)
	if closeErr := file.Close(); !errors.Is(closeErr, os.ErrClosed) {
		err = errors.Join(err, closeErr)
	}
	return err
}

// Read refuses links, nonregular files, unsafe permissions and oversized data.
func (s *Store) Read(name string, limit int64) ([]byte, error) { return s.read(name, limit, nil) }

func (s *Store) read(name string, limit int64, closeFn func(*os.File) error) (data []byte, err error) {
	if !storageName(name) || limit <= 0 || limit > fileLimit {
		return nil, &storageError{errors.New("invalid runtime read")}
	}
	if _, err := s.directoryInfo(true); err != nil {
		return nil, &storageError{err}
	}
	before, err := s.root.Lstat(name)
	if err != nil || !privateInfo(before, false) {
		return nil, &storageError{errors.Join(err, errors.New("unsafe runtime file"))}
	}
	file, err := s.root.OpenFile(name, privateReadFlags(), 0)
	if err != nil {
		return nil, &storageError{err}
	}
	defer func() {
		if closeErr := closeRuntimeFile(file, closeFn); closeErr != nil {
			data, err = nil, &storageError{errors.Join(err, closeErr)}
		}
	}()
	opened, err := file.Stat()
	if err != nil || !privateHandle(file, false, false) || !os.SameFile(before, opened) || opened.Size() > limit {
		return nil, &storageError{errors.Join(err, errors.New("runtime file changed or too large"))}
	}
	data, err = io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, &storageError{errors.Join(err, errors.New("runtime read failed or too large"))}
	}
	return data, nil
}

type fileOps struct {
	write func(*os.File, []byte) (int, error)
	sync  func(*os.File) error
	close func(*os.File) error
}

// Create exclusively writes a bounded private file. Existing entries are untouched.
// This is not atomic publication; later state publication must use a staging file.
func (s *Store) Create(name string, data []byte) error { return s.create(name, data, fileOps{}) }

func (s *Store) create(name string, data []byte, ops fileOps) (err error) {
	if !storageName(name) || len(data) == 0 || len(data) > fileLimit {
		return &storageError{errors.New("invalid runtime write")}
	}
	if _, err := s.directoryInfo(true); err != nil {
		return &storageError{err}
	}
	file, err := privateCreate(s.root, name)
	if err != nil {
		return &storageError{err}
	}
	before, err := file.Stat()
	defer func() {
		err = errors.Join(err, closeRuntimeFile(file, ops.close))
		if err != nil {
			if current, statErr := s.root.Lstat(name); statErr == nil && before != nil && os.SameFile(before, current) {
				err = errors.Join(err, s.root.Remove(name))
			}
			err = &storageError{err}
		}
	}()
	if err != nil || !privateHandle(file, false, false) {
		return errors.Join(err, errors.New("unsafe created runtime file"))
	}
	if ops.write == nil {
		ops.write = func(f *os.File, p []byte) (int, error) { return f.Write(p) }
	}
	if ops.sync == nil {
		ops.sync = func(f *os.File) error { return f.Sync() }
	}
	n, err := ops.write(file, data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = ops.sync(file)
	}
	return err
}
