package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// The managed copy is replaced only while its instance is stopped, because
// Magpie re-executes itself when its executable file changes.

func installBundledMagpie(layout managedLayout, bundled string, runningPID int) (bool, error) {
	if runningPID > 1 {
		return false, errors.New("the managed Magpie is running; stop it before replacing its executable")
	}
	bundledDigest, err := fileDigest(bundled)
	if err != nil {
		return false, fmt.Errorf("the packaged Magpie is missing: %w", err)
	}
	if copiedDigest, err := fileDigest(layout.Executable); err == nil && copiedDigest == bundledDigest {
		if err := ensurePortableMarker(layout); err != nil {
			return false, err
		}
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(layout.Executable), 0700); err != nil {
		return false, err
	}
	temporary := layout.Executable + ".new"
	if err := copyExecutable(bundled, temporary); err != nil {
		return false, err
	}
	if err := os.Rename(temporary, layout.Executable); err != nil {
		os.Remove(temporary)
		return false, err
	}
	if err := ensurePortableMarker(layout); err != nil {
		return false, err
	}
	return true, nil
}

func copyExecutable(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0700)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err == nil {
		err = out.Sync()
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	return err
}

func fileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func ensurePortableMarker(layout managedLayout) error {
	if info, err := os.Stat(layout.Marker); err == nil && !info.IsDir() {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(layout.Marker), 0700); err != nil {
		return err
	}
	return writePrivate(layout.Marker, []byte("agent-harbor managed Magpie\n"))
}

// disableManagedExtras turns off telemetry and self-update through Magpie's
// own settings command, so the instance stays on the version Harbor shipped.
