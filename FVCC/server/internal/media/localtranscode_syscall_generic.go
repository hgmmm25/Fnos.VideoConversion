//go:build !windows

package media

import (
	"os/exec"
	"syscall"
)

func setSysProcAttr(cmd *exec.Cmd) {
}

func getExitCode(status syscall.WaitStatus) int {
	return status.ExitStatus()
}
