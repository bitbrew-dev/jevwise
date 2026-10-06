package daemon

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/bitbrew-dev/jevwise/internal/mcpserver"
)

type tokenFileError struct{ cause error }

func (e *tokenFileError) Error() string { return "cannot read protected MCP token file" }
func (e *tokenFileError) Unwrap() error { return e.cause }

// ReadAgentToken reads an owner-private regular file without repairing modes or
// ACLs. Its absolute, clean path has a caller-trusted parent. Static links and
// detected replacement are refused; hostile mounts/ancestor renaming are outside
// this boundary. Windows requires an explicit protected current-user-only DACL.
// One optional LF or CRLF is accepted. No other whitespace is stripped.
func ReadAgentToken(path string) (string, error) { return readAgentToken(path, nil) }

func readAgentToken(path string, closeFn func(*os.File) error) (token string, err error) {
	fail := func(cause error) (string, error) { return "", &tokenFileError{cause} }
	if storageSupported() != nil || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fail(errors.New("invalid token file path or unsupported platform"))
	}
	before, err := os.Lstat(path)
	if err != nil || !privateInfo(before, false) {
		return fail(errors.Join(err, errors.New("unsafe token file")))
	}
	file, err := os.OpenFile(path, privateReadFlags(), 0)
	if err != nil {
		return fail(err)
	}
	defer func() {
		if closeErr := closeRuntimeFile(file, closeFn); closeErr != nil {
			token, err = fail(errors.Join(err, closeErr))
		}
	}()
	opened, err := file.Stat()
	if err != nil || !privateHandle(file, false, true) || !os.SameFile(before, opened) || opened.Size() > 4098 {
		return fail(errors.Join(err, errors.New("token file changed or is not private")))
	}
	data, err := io.ReadAll(io.LimitReader(file, 4099))
	if err != nil || len(data) > 4098 {
		return fail(errors.Join(err, errors.New("token file exceeds limit")))
	}
	after, err := os.Lstat(path)
	if err != nil || !privateInfo(after, false) || !os.SameFile(opened, after) || !privateHandle(file, false, true) {
		return fail(errors.Join(err, errors.New("token file changed during read")))
	}
	token = strings.TrimSuffix(string(data), "\r\n")
	if len(token) == len(data) {
		token = strings.TrimSuffix(token, "\n")
	}
	if err := mcpserver.ValidateToken(token); err != nil {
		return fail(err)
	}
	return token, nil
}
