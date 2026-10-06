package daemon

import (
	"os/exec"
	"syscall"
)

func detach(cmd *exec.Cmd) error {
	// DETACHED_PROCESS and CREATE_NEW_PROCESS_GROUP preserve inherited stdin.
	// Restrictive Windows Job Objects may still impose process lifetimes.
	const detachedProcess = 0x00000008
	const newProcessGroup = 0x00000200
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess | newProcessGroup, HideWindow: true}
	return nil
}
