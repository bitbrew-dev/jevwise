//go:build !linux && !darwin && !windows

package daemon

import (
	"errors"
	"os/exec"
)

func detach(*exec.Cmd) error { return errors.New("background mode is unsupported on this platform") }
