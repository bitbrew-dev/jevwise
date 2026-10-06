//go:build !linux && !darwin

package daemon

import (
	"errors"
	"os"
)

// Windows remains unsupported until explicit user-only ACL creation/validation lands.
func storageSupported() error {
	return errors.New("private MCP runtime storage is unsupported on this platform")
}
func privateInfo(os.FileInfo, bool) bool { return false }
