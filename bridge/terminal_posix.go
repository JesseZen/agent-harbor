//go:build darwin || linux

package main

import (
	"errors"
	"os"
	"os/signal"
	"syscall"
	"unsafe"

	"github.com/charmbracelet/x/term"
)

// Give the dedicated client process group the pane's terminal while it runs.
func foregroundSession(group int) (func(), error) {
	fd := os.Stdin.Fd()
	// A successful TIOCGPGRP does not establish that stdin is a terminal:
	// macOS can return group zero for a pipe, then reject TIOCSPGRP with EPERM.
	if !term.IsTerminal(fd) {
		return func() {}, nil
	}
	var previous int32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCGPGRP, uintptr(unsafe.Pointer(&previous)))
	if errno == syscall.ENOTTY {
		return func() {}, nil
	}
	if errno != 0 {
		return nil, errno
	}
	signal.Ignore(syscall.SIGTTOU)
	set := func(group int32) error {
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCSPGRP, uintptr(unsafe.Pointer(&group)))
		if errno != 0 {
			return errno
		}
		return nil
	}
	if err := set(int32(group)); err != nil {
		signal.Reset(syscall.SIGTTOU)
		return nil, err
	}
	if err := syscall.Kill(-group, syscall.SIGCONT); err != nil && !errors.Is(err, syscall.ESRCH) {
		_ = set(previous)
		signal.Reset(syscall.SIGTTOU)
		return nil, err
	}
	return func() { _ = set(previous); signal.Reset(syscall.SIGTTOU) }, nil
}
