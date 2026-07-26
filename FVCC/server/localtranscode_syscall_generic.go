//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

func setSysProcAttr(cmd *exec.Cmd) {
}

func getExitCode(status syscall.WaitStatus) int {
	return status.ExitStatus()
}
