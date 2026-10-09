package config

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const LocalName = "jevwise.toml"
const MaxFileBytes = 1 << 20

// GlobalPath preserves the existing configuration namespace.
func GlobalPath() (string, error) {
	base := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.New("cannot discover configuration: home is unset")
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Abs(filepath.Join(base, "ts-jev", "config.toml"))
}

// ResolvePath returns the selected file and whether its presence is required.
// Only the current directory is searched; a local file replaces the global one.
func ResolvePath(explicit string) (string, bool, error) {
	if explicit != "" {
		path, err := filepath.Abs(explicit)
		return path, true, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", true, errors.New("cannot discover current-directory configuration")
	}
	local := filepath.Join(cwd, LocalName)
	info, err := os.Lstat(local)
	if err == nil {
		if !info.Mode().IsRegular() {
			return "", true, errors.New("local configuration must be a regular file, not a link")
		}
		return local, true, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", true, errors.New("cannot inspect local configuration")
	}
	path, err := GlobalPath()
	return path, false, err
}

// ReadFile bounds TOML input and refuses special files before opening them.
// Explicit/global read-only symlinks retain their existing behavior. Checks
// assume cooperative filesystem storage, not hostile concurrent path changes.
func ReadFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxFileBytes {
		return nil, errors.New("configuration must be a regular file of at most 1 MiB")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, errors.New("configuration changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
	if err == nil && len(data) > MaxFileBytes {
		err = errors.New("configuration exceeds 1 MiB")
	}
	return data, err
}
