// Package buildinfo holds metadata optionally supplied through Go linker flags.
package buildinfo

import (
	"fmt"
	"runtime"
)

// CommandName is the public executable and root-command identity.
const CommandName = "jevwise"

// Update stamps survive stripped, path-trimmed release builds for passive inspection.
const UpdateStampPrefix = "JEVWISE_UPDATE_V1["
const UpdateStampSuffix = "]JEVWISE_UPDATE_END"

var (
	Version     = "dev"
	Commit      = "unknown"
	Date        = "unknown"
	UpdateStamp = ""
)

// String formats the same deterministic human-readable output for all commands.
func String() string {
	version := Version
	if UpdateStamp != "" && UpdateStamp != UpdateStampPrefix+Version+UpdateStampSuffix {
		version = "unknown"
	}
	return fmt.Sprintf(CommandName+" %s\ncommit: %s\nbuilt: %s\ngo: %s\nplatform: %s/%s\n",
		version, Commit, Date, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
