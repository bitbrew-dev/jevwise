package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	gobuildinfo "debug/buildinfo"
	"errors"
	"io"
	"runtime/debug"

	"github.com/bitbrew-dev/jevwise/internal/buildinfo"
	"github.com/bitbrew-dev/jevwise/internal/debuglog"
)

type guardError struct{ cause error }

func (e *guardError) Error() string { return "cannot validate Jev executable" }
func (e *guardError) Unwrap() error { return e.cause }

func guardFailure(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	return &guardError{err}
}

type contextReaderAt struct {
	ctx    context.Context
	reader io.ReaderAt
}

func (r contextReaderAt) ReadAt(p []byte, offset int64) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.ReadAt(p, offset)
	if canceled := r.ctx.Err(); canceled != nil {
		return n, canceled
	}
	return n, err
}

// readInstalled must be called under the installer lock using its opened file.
// It never closes the caller's reader or executes the inspected executable.
func readInstalled(ctx context.Context, reader io.ReaderAt, size int64, current, goos, goarch string) error {
	if ctx == nil || reader == nil || size <= 0 || size > binaryLimit {
		return guardFailure(ctx, errors.New("invalid executable input"))
	}
	if _, err := Compare(current, current); err != nil {
		return guardFailure(ctx, err)
	}
	data, err := io.ReadAll(io.NewSectionReader(contextReaderAt{ctx, reader}, 0, size))
	if err != nil {
		return guardFailure(ctx, err)
	}
	if int64(len(data)) != size {
		return guardFailure(ctx, io.ErrUnexpectedEOF)
	}
	tag, err := inspectExecutable(ctx, data, goos, goarch)
	if err != nil {
		return err
	}
	if tag != current {
		return guardFailure(ctx, errors.New("installed version changed"))
	}
	return nil
}

// validateBinary rechecks the opaque download before any filesystem publication.
func validateBinary(ctx context.Context, binary *Binary, current, goos, goarch string) (resultErr error) {
	finish := debuglog.Trace(ctx, "update.verify")
	defer func() { finish(resultErr) }()
	if ctx == nil || binary == nil || len(binary.data) == 0 || len(binary.data) > binaryLimit {
		return guardFailure(ctx, errors.New("invalid verified binary"))
	}
	if err := ctx.Err(); err != nil {
		return guardFailure(ctx, err)
	}
	order, err := Compare(current, binary.tag)
	if err != nil {
		return guardFailure(ctx, err)
	}
	if order >= 0 || binary.goos != goos || binary.goarch != goarch {
		return guardFailure(ctx, errors.New("candidate is not a newer matching release"))
	}
	digest := sha256.Sum256(binary.data)
	if err := ctx.Err(); err != nil {
		return guardFailure(ctx, err)
	}
	if subtle.ConstantTimeCompare(digest[:], binary.digest[:]) != 1 {
		return guardFailure(ctx, errors.New("verified binary changed"))
	}
	tag, err := inspectExecutable(ctx, binary.data, goos, goarch)
	if err != nil {
		return err
	}
	if tag != binary.tag {
		return guardFailure(ctx, errors.New("candidate version mismatch"))
	}
	return nil
}

func inspectExecutable(ctx context.Context, data []byte, goos, goarch string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", guardFailure(ctx, err)
	}
	info, err := gobuildinfo.Read(bytes.NewReader(data))
	if err != nil {
		return "", guardFailure(ctx, err)
	}
	if err := validateExecutableMetadata(info, goos, goarch); err != nil {
		return "", guardFailure(ctx, err)
	}
	tag, err := executableStamp(data)
	if err != nil {
		return "", guardFailure(ctx, err)
	}
	if err := ctx.Err(); err != nil {
		return "", guardFailure(ctx, err)
	}
	return tag, nil
}

func validateExecutableMetadata(info *debug.BuildInfo, goos, goarch string) error {
	if (goos != "linux" && goos != "darwin" && goos != "windows") || (goarch != "amd64" && goarch != "arm64") || info == nil || info.Path != "github.com/bitbrew-dev/jevwise/cmd" || info.Main.Path != "github.com/bitbrew-dev/jevwise" || info.Main.Replace != nil {
		return errors.New("executable identity mismatch")
	}
	seen := make(map[string]bool, 2)
	for _, setting := range info.Settings {
		if setting.Key != "GOOS" && setting.Key != "GOARCH" {
			continue
		}
		expected := goos
		if setting.Key == "GOARCH" {
			expected = goarch
		}
		if seen[setting.Key] || setting.Value != expected {
			return errors.New("executable platform mismatch")
		}
		seen[setting.Key] = true
	}
	if len(seen) != 2 {
		return errors.New("executable platform missing")
	}
	return nil
}

func executableStamp(data []byte) (string, error) {
	prefix, suffix := []byte(buildinfo.UpdateStampPrefix), []byte(buildinfo.UpdateStampSuffix)
	found := ""
	for {
		start := bytes.Index(data, prefix)
		if start < 0 {
			break
		}
		data = data[start+len(prefix):]
		window := data
		// Three uint64 components plus v and dots require at most 63 bytes.
		if len(window) > 63+len(suffix) {
			window = window[:63+len(suffix)]
		}
		end := bytes.Index(window, suffix)
		if end < 0 {
			continue
		}
		tag := string(window[:end])
		if _, err := Compare(tag, tag); err != nil {
			continue
		}
		if found != "" {
			return "", errors.New("executable version stamp duplicated")
		}
		found = tag
	}
	if found == "" {
		return "", errors.New("executable version stamp missing")
	}
	return found, nil
}
