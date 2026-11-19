//go:build linux

package main

import (
	"os"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

func selectionPeerPID(fd int) int {
	cred, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || int(cred.Uid) != os.Getuid() {
		return 0
	}
	return int(cred.Pid)
}

// processFacts reads the executable and selected environment variables of a
// same-user process through /proc, which lists another user's process but
// denies its environment and descriptor.
func readProcessFacts(pid int) (processFacts, error) {
	if pid <= 1 {
		return processFacts{}, errProcessGone
	}
	executable, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
	if err != nil {
		return processFacts{}, classifyProcessError(err)
	}
	// A renamed or replaced executable is listed with a marker, and the path
	// before it is where the file is now, not where Magpie started from.
	// Magpie decides its data folder from the start-time path, so this case
	// cannot be resolved and must not be guessed.
	if strings.HasSuffix(executable, " (deleted)") {
		return processFacts{}, errProcessReplaced
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ")
	if err != nil {
		return processFacts{}, classifyProcessError(err)
	}
	var environ []string
	for _, entry := range strings.Split(string(data), "\x00") {
		if entry != "" {
			environ = append(environ, entry)
		}
	}
	return processFacts{PID: pid, Executable: executable, Variables: selectProcessVariables(environ)}, nil
}

func classifyProcessError(err error) error {
	switch {
	case os.IsPermission(err):
		return errProcessDifferentUser
	case os.IsNotExist(err):
		return errProcessGone
	}
	if errno, ok := err.(syscall.Errno); ok {
		switch errno {
		case syscall.ESRCH, syscall.ENOENT:
			return errProcessGone
		case syscall.EACCES, syscall.EPERM:
			return errProcessDifferentUser
		}
	}
	return err
}
