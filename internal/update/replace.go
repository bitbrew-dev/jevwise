package update

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// InstallResult distinguishes publication from subsequent cleanup/cancellation.
// Installed binaries are never rolled back after a successful rename.
type InstallResult struct{ Installed bool }

type replaceError struct{ cause error }

func (e *replaceError) Error() string { return "cannot replace Jev executable" }
func (e *replaceError) Unwrap() error { return e.cause }

type replaceHooks struct {
	write   func(*os.File, []byte) (int, error)
	chmod   func(*os.File, os.FileMode) error
	sync    func(*os.File) error
	close   func(*os.File) error
	publish func(*os.Root, string, string) error
}

// Replace atomically replaces the os.Executable-returned path on Linux/macOS.
// The lock coordinates cooperating installers, not hostile filesystem writers.
// Only permission bits are preserved, not group ownership, ACLs or xattrs.
// File sync precedes publication; no directory-sync/power-loss guarantee is made.
func Replace(ctx context.Context, binary *Binary, current string) (InstallResult, error) {
	if err := replaceCandidate(ctx, binary, current); err != nil {
		return InstallResult{}, &replaceError{err}
	}
	path, err := os.Executable()
	if err != nil {
		return InstallResult{}, &replaceError{err}
	}
	return replaceAt(ctx, binary, current, filepath.Dir(path), filepath.Base(path), replaceHooks{})
}

func replaceCandidate(ctx context.Context, binary *Binary, current string) error {
	if ctx == nil {
		return errors.New("invalid replacement context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return errors.New("executable replacement is unsupported on this platform")
	}
	return validateBinary(ctx, binary, current, runtime.GOOS, runtime.GOARCH)
}

func replaceSafe(info os.FileInfo) bool {
	return info != nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 &&
		info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) == 0 &&
		info.Size() > 0 && info.Size() <= binaryLimit && replaceOwned(info)
}

func replaceSame(a, b os.FileInfo) bool {
	return replaceSafe(a) && replaceSafe(b) && os.SameFile(a, b) && a.Mode() == b.Mode() &&
		a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

func replaceAt(ctx context.Context, binary *Binary, current, directory, name string, hooks replaceHooks) (result InstallResult, err error) {
	defer func() {
		if err != nil {
			err = &replaceError{err}
		}
	}()
	if err = replaceCandidate(ctx, binary, current); err != nil {
		return result, err
	}
	if name == "" || name == "." || name == ".." || name != filepath.Base(name) {
		return result, errors.New("invalid executable basename")
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	lock := "." + name + ".jev-update-lock"
	if err = root.Mkdir(lock, 0o700); err != nil {
		return result, err // Never remove a preexisting or stale lock.
	}
	stageName := ""
	defer func() {
		if stageName != "" {
			err = errors.Join(err, root.Remove(stageName))
		}
		err = errors.Join(err, root.Remove(lock))
	}()
	baseline, err := root.Lstat(name)
	if err != nil || !replaceSafe(baseline) {
		return result, errors.Join(err, errors.New("unsafe executable target"))
	}
	installed, err := root.Open(name)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, installed.Close()) }()
	opened, err := installed.Stat()
	if err != nil || !replaceSame(baseline, opened) {
		return result, errors.Join(err, errors.New("executable changed before inspection"))
	}
	if err = readInstalled(ctx, installed, baseline.Size(), current, runtime.GOOS, runtime.GOARCH); err != nil {
		return result, err
	}
	inspected, err := installed.Stat()
	if err != nil || !replaceSame(baseline, inspected) {
		return result, errors.Join(err, errors.New("executable changed during inspection"))
	}
	candidateName := "." + name + ".jev-stage-" + rand.Text()
	stage, err := root.OpenFile(candidateName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return result, err
	}
	stageName = candidateName // Cleanup only names successfully created by us.
	defer func() {
		if closeErr := stage.Close(); !errors.Is(closeErr, os.ErrClosed) {
			err = errors.Join(err, closeErr)
		}
	}()
	if hooks.write == nil {
		hooks.write = func(f *os.File, b []byte) (int, error) { return f.Write(b) }
	}
	if hooks.chmod == nil {
		hooks.chmod = func(f *os.File, mode os.FileMode) error { return f.Chmod(mode) }
	}
	if hooks.sync == nil {
		hooks.sync = func(f *os.File) error { return f.Sync() }
	}
	if hooks.close == nil {
		hooks.close = func(f *os.File) error { return f.Close() }
	}
	if hooks.publish == nil {
		hooks.publish = func(r *os.Root, from, to string) error { return r.Rename(from, to) }
	}
	n, err := hooks.write(stage, binary.data)
	if err == nil && n != len(binary.data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return result, err
	}
	if err = hooks.chmod(stage, baseline.Mode().Perm()); err != nil {
		return result, err
	}
	if err = hooks.sync(stage); err != nil {
		return result, err
	}
	if err = hooks.close(stage); err != nil {
		return result, err
	}
	latest, err := root.Lstat(name)
	if err != nil || !replaceSame(baseline, latest) {
		return result, errors.Join(err, errors.New("executable changed before publication"))
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	if err = hooks.publish(root, stageName, name); err != nil {
		return result, err
	}
	result.Installed, stageName = true, ""
	return result, ctx.Err()
}
