//go:build linux || darwin

package daemon

import (
	"os"
	"syscall"
)

func privateReadFlags() int { return os.O_RDONLY | syscall.O_NOFOLLOW | syscall.O_NONBLOCK }

func privateHandle(file *os.File, directory, protected bool) bool {
	info, err := file.Stat()
	return err == nil && privateInfo(info, directory)
}
