//go:build !darwin && !linux

package main

import (
	"errors"
	"os/exec"
)

func startSessionClient(*exec.Cmd) (func(), error) {
	return nil, errors.New("session terminal control is unsupported on this platform")
}
