//go:build !linux && !darwin && !windows

package daemon

import (
	"errors"
	"os"
)

func storageSupported() error {
	return errors.New("private MCP runtime storage is unsupported on this platform")
}
func mkdirPrivate(string) error          { return storageSupported() }
func privateInfo(os.FileInfo, bool) bool { return false }
