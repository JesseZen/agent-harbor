//go:build !darwin && !linux

package main

import (
	"os/exec"
	"syscall"
)

// interruptedExit reports whether the exit error is the user quitting a
// terminal UI rather than that UI failing.
func interruptedExit(exit *exec.ExitError) bool {
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok {
		return false
	}
	return status.ExitStatus() == 128+int(syscall.SIGINT)
}
