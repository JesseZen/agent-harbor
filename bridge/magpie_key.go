package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
)

// The web key authenticates the managed instance's page. It is generated once,
// kept at 0600, passed to Magpie only through its environment, and never
// printed.

func ensureWebKey(layout managedLayout) (string, error) {
	data, err := os.ReadFile(layout.WebKey)
	if err == nil {
		key := string(data)
		if len(key) >= 16 && !hasUnsafeKeyByte(key) {
			return key, nil
		}
		return "", errors.New("the managed Magpie web key file is not usable; move it aside to generate a new one")
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(layout.Root, 0700); err != nil {
		return "", err
	}
	var raw [18]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	key := hex.EncodeToString(raw[:])
	if err := writePrivate(layout.WebKey, []byte(key)); err != nil {
		return "", err
	}
	return key, nil
}

func hasUnsafeKeyByte(key string) bool {
	for _, b := range []byte(key) {
		switch b {
		case '"', '\'', ' ', '\t', '\n', '\r', '\\':
			return true
		}
	}
	return false
}

// managedEnvironment builds the child environment from the current one,
// replacing only the paths the instance must own.
