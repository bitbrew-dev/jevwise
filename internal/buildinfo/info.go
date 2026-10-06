// Package buildinfo holds metadata optionally supplied through Go linker flags.
package buildinfo

import (
	"fmt"
	"runtime"
)

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// String formats the same deterministic human-readable output for all commands.
func String() string {
	return fmt.Sprintf("jev %s\ncommit: %s\nbuilt: %s\ngo: %s\nplatform: %s/%s\n",
		Version, Commit, Date, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
