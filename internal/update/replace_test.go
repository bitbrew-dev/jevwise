//go:build linux || darwin

package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
)

var replacementOnce sync.Once
var replacementBytes []byte
var replacementBuildError error

func replacementFixture(t *testing.T) ([]byte, *Binary) {
	t.Helper()
	replacementOnce.Do(func() {
		path := filepath.Join(t.TempDir(), "fixture")
		prefix := "github.com/bitbrew-dev/jevwise/internal/buildinfo."
		flags := "-s -w -X " + prefix + "Version=v1.2.3 -X " + prefix + "UpdateStamp=" + versionStamp("v1.2.3")
		cmd := exec.Command("go", "build", "-mod=readonly", "-trimpath", "-ldflags="+flags, "-o", path, "./cmd")
		cmd.Dir = "../.."
		cmd.Env = append(os.Environ(), "GOPROXY=off", "GOFLAGS=-p=4")
		if output, err := cmd.CombinedOutput(); err != nil {
			replacementBuildError = errors.New(string(output))
			return
		}
		replacementBytes, replacementBuildError = os.ReadFile(path)
	})
	if replacementBuildError != nil {
		t.Fatal("fixture build failed", replacementBuildError)
	}
	candidate := bytes.ReplaceAll(replacementBytes, []byte("v1.2.3"), []byte("v1.2.4"))
	return replacementBytes, &Binary{data: candidate, digest: sha256.Sum256(candidate), tag: "v1.2.4", goos: runtime.GOOS, goarch: runtime.GOARCH}
}

func replacementTarget(t *testing.T, data []byte) (string, string) {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "jev")
	if err := os.WriteFile(path, data, 0o751); err != nil {
		t.Fatal(err)
	}
	return directory, path
}

func TestReplaceSuccessPreservesOldInodeAndPermissions(t *testing.T) {
	original, binary := replacementFixture(t)
	directory, path := replacementTarget(t, original)
	old, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	before, _ := old.Stat()
	result, err := replaceAt(context.Background(), binary, "v1.2.3", directory, "jev", replaceHooks{})
	after, _ := os.Stat(path)
	data, _ := os.ReadFile(path)
	oldData, _ := io.ReadAll(old)
	entries, _ := os.ReadDir(directory)
	if err != nil || !result.Installed || os.SameFile(before, after) || after.Mode().Perm() != 0o751 || !bytes.Equal(data, binary.data) || !bytes.Equal(oldData, original) || len(entries) != 1 {
		t.Fatal("unsafe successful publication", result, err)
	}
}

func TestReplaceStageFailuresPreserveOriginal(t *testing.T) {
	original, binary := replacementFixture(t)
	cause := errors.New("private-stage-error")
	cases := []struct {
		name  string
		hooks replaceHooks
		want  error
	}{
		{"write", replaceHooks{write: func(*os.File, []byte) (int, error) { return 0, cause }}, cause},
		{"short write", replaceHooks{write: func(*os.File, []byte) (int, error) { return 0, nil }}, io.ErrShortWrite},
		{"chmod", replaceHooks{chmod: func(*os.File, os.FileMode) error { return cause }}, cause},
		{"sync", replaceHooks{sync: func(*os.File) error { return cause }}, cause},
		{"close", replaceHooks{close: func(f *os.File) error { _ = f.Close(); return cause }}, cause},
		{"publish", replaceHooks{publish: func(*os.Root, string, string) error { return cause }}, cause},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			directory, path := replacementTarget(t, original)
			before, _ := os.Stat(path)
			result, err := replaceAt(context.Background(), binary, "v1.2.3", directory, "jev", tc.hooks)
			after, _ := os.Stat(path)
			data, _ := os.ReadFile(path)
			entries, _ := os.ReadDir(directory)
			if result.Installed || !errors.Is(err, tc.want) || strings.Contains(err.Error(), "private") || !os.SameFile(before, after) || !bytes.Equal(data, original) || len(entries) != 1 {
				t.Fatal("failure changed executable or leaked", result, err)
			}
		})
	}
}

func TestReplaceUnsafeTargetsAndLocks(t *testing.T) {
	original, binary := replacementFixture(t)
	for _, kind := range []string{"link", "hardlink", "directory", "nonexec", "setuid", "setgid", "sticky", "empty", "lock", "stale"} {
		t.Run(kind, func(t *testing.T) {
			directory, path := replacementTarget(t, original)
			current := "v1.2.3"
			switch kind {
			case "link":
				_ = os.Rename(path, path+".original")
				_ = os.Symlink("jev.original", path)
			case "hardlink":
				_ = os.Link(path, path+".alias")
			case "directory":
				_ = os.Remove(path)
				_ = os.Mkdir(path, 0o700)
			case "nonexec":
				_ = os.Chmod(path, 0o600)
			case "setuid":
				_ = os.Chmod(path, os.ModeSetuid|0o751)
			case "setgid":
				_ = os.Chmod(path, os.ModeSetgid|0o751)
			case "sticky":
				_ = os.Chmod(path, os.ModeSticky|0o751)
			case "empty":
				_ = os.Truncate(path, 0)
			case "lock":
				_ = os.Mkdir(filepath.Join(directory, ".jev.jev-update-lock"), 0o700)
			case "stale":
				current = "v1.2.2"
			}
			before, _ := os.Lstat(path)
			if (kind == "setuid" && before.Mode()&os.ModeSetuid == 0) || (kind == "setgid" && before.Mode()&os.ModeSetgid == 0) {
				t.Skip("filesystem strips special mode bits; helpers tested separately")
			}
			entriesBefore, _ := os.ReadDir(directory)
			result, err := replaceAt(context.Background(), binary, current, directory, "jev", replaceHooks{})
			after, _ := os.Lstat(path)
			entriesAfter, _ := os.ReadDir(directory)
			if err == nil || result.Installed || !os.SameFile(before, after) || before.Mode() != after.Mode() || before.Size() != after.Size() || len(entriesAfter) != len(entriesBefore) {
				t.Fatal("unsafe target/lock changed", kind, err)
			}
		})
	}
}

func TestReplaceCancellationAndChangedTarget(t *testing.T) {
	original, binary := replacementFixture(t)
	for _, kind := range []string{"before", "staged", "changed", "published", "cleanup"} {
		t.Run(kind, func(t *testing.T) {
			directory, path := replacementTarget(t, original)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			hooks := replaceHooks{}
			switch kind {
			case "before":
				cancel()
			case "staged":
				hooks.sync = func(f *os.File) error { cancel(); return f.Sync() }
			case "changed":
				hooks.close = func(f *os.File) error { _ = os.Chmod(path, 0o750); return f.Close() }
			case "published", "cleanup":
				hooks.publish = func(r *os.Root, from, to string) error {
					if err := r.Rename(from, to); err != nil {
						return err
					}
					if kind == "published" {
						cancel()
					} else {
						f, err := r.OpenFile(".jev.jev-update-lock/obstruction", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
						if err != nil {
							return err
						}
						_ = f.Close()
					}
					return nil
				}
			}
			result, err := replaceAt(ctx, binary, "v1.2.3", directory, "jev", hooks)
			data, _ := os.ReadFile(path)
			installed := kind == "published" || kind == "cleanup"
			want := original
			if installed {
				want = binary.data
			}
			if err == nil || result.Installed != installed || !bytes.Equal(data, want) {
				t.Fatal("publication state lost", result, err)
			}
			if kind != "changed" && kind != "cleanup" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation cause lost", err)
			}
			if kind == "cleanup" && !errors.Is(err, syscall.ENOTEMPTY) {
				t.Fatal("cleanup cause lost", err)
			}
		})
	}
}

type replaceInfo struct {
	os.FileInfo
	sys  any
	mode os.FileMode
}

func (f replaceInfo) Sys() any          { return f.sys }
func (f replaceInfo) Mode() os.FileMode { return f.mode }

func TestReplaceOwnerCandidateAndBasenameGuards(t *testing.T) {
	original, binary := replacementFixture(t)
	directory, path := replacementTarget(t, original)
	info, _ := os.Stat(path)
	stat := *info.Sys().(*syscall.Stat_t)
	stat.Uid++
	if replaceOwned(replaceInfo{info, &stat, info.Mode()}) || replaceOwned(replaceInfo{info, nil, info.Mode()}) {
		t.Fatal("unsafe owner metadata accepted")
	}
	for _, mode := range []os.FileMode{os.ModeSetuid, os.ModeSetgid, os.ModeSticky} {
		if replaceSafe(replaceInfo{info, info.Sys(), info.Mode() | mode}) {
			t.Fatal("unsafe special mode accepted")
		}
	}
	for _, candidate := range []*Binary{nil, {}, {data: []byte("private-invalid")}} {
		if result, err := replaceAt(context.Background(), candidate, "v1.2.3", directory, "jev", replaceHooks{}); err == nil || result.Installed || strings.Contains(err.Error(), "private") {
			t.Fatal("invalid candidate accepted", err)
		}
	}
	if _, err := replaceAt(nil, binary, "v1.2.3", directory, "jev", replaceHooks{}); err == nil {
		t.Fatal("nil context accepted")
	}
	if _, err := replaceAt(context.Background(), binary, "v1.2.3", directory, "../jev", replaceHooks{}); err == nil {
		t.Fatal("invalid basename accepted")
	}
}
