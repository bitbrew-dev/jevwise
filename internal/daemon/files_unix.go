//go:build linux || darwin

package daemon

import (
	"os"
	"syscall"
)

func privateReadFlags() int { return os.O_RDONLY | syscall.O_NOFOLLOW | syscall.O_NONBLOCK }

func privateHandle(file *os.File, directory, protected bool) bool {
	if file == nil {
		return false
	}
	info, err := file.Stat()
	return err == nil && privateInfo(info, directory) && noExtendedACL(file)
}

// Link counts do not confer ownership; callers must verify both owned names.
func privateHandleLinks(file *os.File, expected uint32) bool {
	if file == nil {
		return false
	}
	info, err := file.Stat()
	return err == nil && privateInfoLinks(info, false, expected) && noExtendedACL(file)
}
