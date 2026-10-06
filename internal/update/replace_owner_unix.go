//go:build linux || darwin

package update

import (
	"os"
	"syscall"
)

func replaceOwned(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink == 1 && stat.Uid == uint32(os.Getuid())
}
