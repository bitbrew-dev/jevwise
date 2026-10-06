//go:build linux || darwin

package daemon

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestStorePrivateFilesAndBounds(t *testing.T) {
	store, path := testStore(t)
	data := bytes.Repeat([]byte("k"), fileLimit)
	if err := store.Create("control.key", data); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filepath.Join(path, "control.key"))
	if info.Mode().Perm() != 0o600 {
		t.Fatal("file is not private")
	}
	got, err := store.Read("control.key", fileLimit)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("bounded read failed", err)
	}
	if _, err := store.Read("control.key", fileLimit-1); err == nil {
		t.Fatal("oversized read accepted")
	}
	if err := store.Create("control.key", []byte("replacement")); !errors.Is(err, os.ErrExist) {
		t.Fatal("existing file overwritten", err)
	}
	for _, name := range []string{"", "../control.key", "/control.key", "sub/control.key", "unknown"} {
		if store.Create(name, []byte("secret")) == nil {
			t.Fatal("unsafe write name accepted")
		}
		if _, err := store.Read(name, 32); err == nil {
			t.Fatal("unsafe read name accepted")
		}
	}
	for _, size := range []int{0, fileLimit + 1} {
		if store.Create("state.json", make([]byte, size)) == nil {
			t.Fatal("invalid write bound accepted")
		}
	}
	for _, limit := range []int64{0, -1, fileLimit + 1} {
		if _, err := store.Read("control.key", limit); err == nil {
			t.Fatal("invalid read bound accepted")
		}
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if store.Create("state.json", []byte("secret")) == nil {
		t.Fatal("changed directory permissions accepted")
	}
}

func TestStoreRefusesUnsafeFiles(t *testing.T) {
	for _, kind := range []string{"public", "link", "hardlink", "directory", "fifo", "large"} {
		t.Run(kind, func(t *testing.T) {
			store, directory := testStore(t)
			path := filepath.Join(directory, "control.key")
			switch kind {
			case "public":
				_ = os.WriteFile(path, []byte("secret"), 0o644)
			case "link":
				_ = os.WriteFile(path+"-target", []byte("secret"), 0o600)
				_ = os.Symlink("control.key-target", path)
			case "hardlink":
				_ = os.WriteFile(path, []byte("secret"), 0o600)
				_ = os.Link(path, path+"-alias")
			case "directory":
				_ = os.Mkdir(path, 0o700)
			case "fifo":
				_ = syscall.Mkfifo(path, 0o600)
			case "large":
				_ = os.WriteFile(path, make([]byte, fileLimit+1), 0o600)
			}
			if _, err := store.Read("control.key", fileLimit); err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatal("unsafe file accepted or leaked", err)
			}
			if err := store.Create("control.key", []byte("replacement")); !errors.Is(err, os.ErrExist) {
				t.Fatal("existing entry replaced", err)
			}
		})
	}
}

func TestFileCreateFailuresCleanOnlyOwnedEntry(t *testing.T) {
	cause := errors.New("private path and credential cause")
	for _, tc := range []struct {
		name string
		ops  fileOps
		want error
	}{
		{"write", fileOps{write: func(f *os.File, p []byte) (int, error) { n, _ := f.Write(p[:1]); return n, cause }}, cause},
		{"short", fileOps{write: func(f *os.File, p []byte) (int, error) { return f.Write(p[:1]) }}, io.ErrShortWrite},
		{"sync", fileOps{sync: func(*os.File) error { return cause }}, cause},
		{"close", fileOps{close: func(*os.File) error { return cause }}, cause},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, directory := testStore(t)
			if err := store.Create("state.json", []byte("existing metadata")); err != nil {
				t.Fatal(err)
			}
			before, _ := os.Stat(filepath.Join(directory, "state.json"))
			err := store.create("control.key", []byte("new management key"), tc.ops)
			if !errors.Is(err, tc.want) || strings.Contains(err.Error(), "credential") {
				t.Fatal("failure cause lost or exposed", err)
			}
			if _, err := os.Stat(filepath.Join(directory, "control.key")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("failed file left behind", err)
			}
			after, _ := os.Stat(filepath.Join(directory, "state.json"))
			data, _ := os.ReadFile(filepath.Join(directory, "state.json"))
			if !os.SameFile(before, after) || string(data) != "existing metadata" {
				t.Fatal("existing state changed")
			}
		})
	}
}

func TestFileCreateFailurePreservesExchangedEntry(t *testing.T) {
	store, directory := testStore(t)
	path := filepath.Join(directory, "control.key")
	cause := errors.New("private close cause")
	var opened *os.File
	ops := fileOps{close: func(file *os.File) error {
		opened = file
		if err := os.Rename(path, path+"-original"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("foreign management key"), 0o600); err != nil {
			t.Fatal(err)
		}
		return cause // The helper must still close the descriptor.
	}}
	if err := store.create("control.key", []byte("own management key"), ops); !errors.Is(err, cause) {
		t.Fatal("close cause lost", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "foreign management key" {
		t.Fatal("exchanged entry removed or changed", err)
	}
	if _, err := opened.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("fault hook leaked descriptor", err)
	}
}

func TestFileReadCloseFailureWithholdsKey(t *testing.T) {
	store, _ := testStore(t)
	if err := store.Create("control.key", []byte("secret key")); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("private read-close credential cause")
	var opened *os.File
	data, err := store.read("control.key", 32, func(file *os.File) error { opened = file; return cause })
	if data != nil || !errors.Is(err, cause) || strings.Contains(err.Error(), "credential") {
		t.Fatal("read close failed unsafely", err)
	}
	if _, err := opened.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatal("read hook leaked descriptor", err)
	}
	if data, err := store.Read("control.key", 32); err != nil || string(data) != "secret key" {
		t.Fatal("read failure changed key", err)
	}
}

func TestFilesNilClosedMissingAndSpecialMetadata(t *testing.T) {
	store, path := testStore(t)
	if _, err := store.Read("control.key", 32); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing cause lost", err)
	}
	if err := store.Create("control.key", []byte("key")); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(filepath.Join(path, "control.key"))
	stat := *info.Sys().(*syscall.Stat_t)
	stat.Uid++
	if privateInfo(storageInfo{info, &stat}, false) {
		t.Fatal("foreign file owner accepted")
	}
	stat.Uid--
	stat.Nlink++
	if privateInfo(storageInfo{info, &stat}, false) {
		t.Fatal("hardlink metadata accepted")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	for _, closed := range []*Store{store, nil} {
		if closed.Create("control.key", []byte("secret")) == nil {
			t.Fatal("nil/closed write accepted")
		}
		if _, err := closed.Read("control.key", 32); err == nil {
			t.Fatal("nil/closed read accepted")
		}
	}
}
