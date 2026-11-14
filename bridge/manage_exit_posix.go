//go:build darwin || linux

package main

import (
	"os/exec"
	"syscall"
)

// interruptedExit reports whether the exit error is the user quitting a
// terminal UI rather than that UI failing. A terminal UI that dies of the
// interrupt reports the signal; one that catches it may follow the shell
// convention of exiting with 128+the signal. Other non-zero statuses are
// failures, including a UI's own usage exit status of 2.
func interruptedExit(exit *exec.ExitError) bool {
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok {
		return false
	}
	if status.Signaled() {
		return status.Signal() == syscall.SIGINT || status.Signal() == syscall.SIGTERM
	}
	return status.ExitStatus() == 128+int(syscall.SIGINT)
}
