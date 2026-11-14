//go:build !darwin && !linux

package main

import "errors"

func foregroundSession(int) (func(), error) {
	return nil, errors.New("session terminal control is unsupported on this platform")
}
