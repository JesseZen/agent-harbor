//go:build darwin

package main

import (
	"encoding/binary"
	"errors"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

func selectionPeerPID(fd int) int {
	pid, _ := unix.GetsockoptInt(fd, unix.SOL_LOCAL, unix.LOCAL_PEERPID)
	return pid
}

// kern.procargs2 is the only unprivileged source of another process's
// environment on macOS. The syscall package exposes control names but not
// arguments, so the raw sysctl is called the way golang.org/x/sys does.
const (
	ctlKern        = 1
	kernProcargs2  = 49
	sysctlRetryMax = 4
)

// processFacts reads the executable and selected environment variables of a
// same-user process through kern.procargs2.
func readProcessFacts(pid int) (processFacts, error) {
	if pid <= 1 {
		return processFacts{}, errProcessGone
	}
	mib := [3]int32{ctlKern, kernProcargs2, int32(pid)}
	for range sysctlRetryMax {
		var size uintptr
		if err := rawSysctl(mib[:], nil, &size); err != nil {
			return processFacts{}, classifyProcessError(err)
		}
		if size == 0 {
			// macOS answers an empty listing both for a pid that is gone and
			// for one it will not let this user read, so liveness decides.
			return processFacts{}, classifyEmptyListing(pid)
		}
		buffer := make([]byte, size)
		if err := rawSysctl(mib[:], buffer, &size); err != nil {
			if errors.Is(err, syscall.ENOMEM) {
				continue // the process's arguments grew between the two calls
			}
			return processFacts{}, classifyProcessError(err)
		}
		facts, err := parseProcargs2(pid, buffer[:size])
		if err != nil {
			return processFacts{}, err
		}
		if resolved, err := filepath.EvalSymlinks(facts.Executable); err == nil {
			facts.Executable = resolved
		}
		return facts, nil
	}
	return processFacts{}, errProcessUnreadable
}

func rawSysctl(mib []int32, buffer []byte, size *uintptr) error {
	var oldp unsafe.Pointer
	if len(buffer) > 0 {
		oldp = unsafe.Pointer(&buffer[0])
	}
	_, _, errno := syscall.Syscall6(syscall.SYS___SYSCTL,
		uintptr(unsafe.Pointer(&mib[0])), uintptr(len(mib)),
		uintptr(oldp), uintptr(unsafe.Pointer(size)), 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

// parseProcargs2 follows the kernel layout: argc, the executable path, NUL
// padding, argc argument strings, then the environment.
func parseProcargs2(pid int, raw []byte) (processFacts, error) {
	if len(raw) < 4 {
		return processFacts{}, errProcessUnreadable
	}
	argc := int(binary.LittleEndian.Uint32(raw[:4]))
	rest := raw[4:]
	executable, rest, ok := procargsString(rest)
	if !ok || executable == "" {
		return processFacts{}, errProcessUnreadable
	}
	for len(rest) > 0 && rest[0] == 0 {
		rest = rest[1:]
	}
	for range argc {
		if _, rest, ok = procargsString(rest); !ok {
			return processFacts{}, errProcessUnreadable
		}
	}
	var environ []string
	for len(rest) > 0 {
		entry, remaining, ok := procargsString(rest)
		if !ok {
			break
		}
		rest = remaining
		if entry != "" {
			environ = append(environ, entry)
		}
	}
	return processFacts{PID: pid, Executable: executable, Variables: selectProcessVariables(environ)}, nil
}

func procargsString(data []byte) (string, []byte, bool) {
	for i, b := range data {
		if b == 0 {
			return string(data[:i]), data[i+1:], true
		}
	}
	return "", nil, false
}

// classifyEmptyListing distinguishes a pid that no longer exists from a
// process that exists but cannot be listed for this user.
func classifyEmptyListing(pid int) error {
	if err := syscall.Kill(pid, 0); err == nil || errors.Is(err, syscall.EPERM) {
		return errProcessDifferentUser
	}
	return errProcessGone
}

// classifyProcessError maps the kernel's answer for a pid. On macOS the
// kernel reports EINVAL, not ESRCH, for a pid that has exited or never existed, so
// both mean the process is gone; a retry after the sysctl races with exit
// would otherwise be misreported as an unreadable process.
func classifyProcessError(err error) error {
	switch err {
	case syscall.EPERM, syscall.EACCES:
		return errProcessDifferentUser
	case syscall.ESRCH, syscall.ENOENT, syscall.EINVAL:
		return errProcessGone
	}
	return err
}
