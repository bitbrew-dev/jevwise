//go:build !linux && !darwin

package update

import "os"

func replaceOwned(os.FileInfo) bool { return false }
