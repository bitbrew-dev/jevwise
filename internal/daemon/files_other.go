//go:build !linux && !darwin

package daemon

import "os"

func privateReadFlags() int { return os.O_RDONLY }

func privateHandle(*os.File, bool, bool) bool { return false }
