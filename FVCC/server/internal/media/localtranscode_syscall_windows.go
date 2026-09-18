//go:build windows

package media

import (
	"os/exec"
	"syscall"
)

func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000,
	}
}

func getExitCode(status syscall.WaitStatus) int {
	return int(status.ExitCode)
}
