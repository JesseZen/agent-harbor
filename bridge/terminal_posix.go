//go:build darwin || linux

package main

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"unsafe"

	"github.com/charmbracelet/x/term"
)

func startSessionClient(cmd *exec.Cmd) (func(), error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	input, ok := cmd.Stdin.(*os.File)
	if !ok || !term.IsTerminal(input.Fd()) {
		return func() {}, cmd.Start()
	}
	fd := input.Fd()
	previous, err := terminalForegroundGroup(fd)
	if errors.Is(err, syscall.ENOTTY) || (err == nil && previous <= 0) {
		return func() {}, cmd.Start()
	}
	if err != nil {
		return nil, err
	}
	cmd.SysProcAttr.Foreground = true
	cmd.SysProcAttr.Ctty = int(fd)
	restore := func() {
		signal.Ignore(syscall.SIGTTOU)
		defer signal.Reset(syscall.SIGTTOU)
		for {
			_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCSPGRP, uintptr(unsafe.Pointer(&previous)))
			if errno != syscall.EINTR {
				return
			}
		}
	}
	if err := cmd.Start(); err != nil {
		restore()
		return nil, err
	}
	return restore, nil
}

func terminalForegroundGroup(fd uintptr) (int32, error) {
	var group int32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCGPGRP, uintptr(unsafe.Pointer(&group)))
	if errno != 0 {
		return 0, errno
	}
	return group, nil
}
