package main

import (
	"errors"
	"fmt"
	"html"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// The managed instance's page needs its web key. The key never goes into a
// command line or printed output; the browser opens a private redirect file.

func managedWebURL(layout managedLayout, record *managedRecord) (string, error) {
	if record == nil || record.PID <= 1 || record.WebAddress == "" {
		return "", errors.New("the managed Magpie is not running")
	}
	probe := probeGateway(record.Address)
	if managedStopAction(record, probe, probeFacts(probe)) != managedStopOwn {
		return "", errors.New("the gateway at the managed address is not the recorded managed instance")
	}
	if !managedReady(record.PID, probe) {
		return "", errors.New("the managed Magpie is not ready")
	}
	key, err := ensureWebKey(layout)
	if err != nil {
		return "", err
	}
	return record.WebAddress + "/?k=" + key, nil
}

// openManagedWebPage opens the page without writing its key anywhere but the
// browser opener's argument.

func openManagedWebPage() error {
	layout, err := managedInstanceState()
	if err != nil {
		return err
	}
	record, err := readManagedRecord(layout)
	if err != nil {
		return err
	}
	url, err := managedWebURL(layout, record)
	if err != nil {
		return err
	}
	// The page URL carries the web key. A command line is visible to other
	// processes, so the browser opens a private redirect file instead and the
	// key never leaves this 0600 file inside the 0700 instance state.
	target, err := webRedirectFile(layout, url)
	if err != nil {
		return err
	}
	opener, err := browserOpener()
	if err != nil {
		return err
	}
	cmd := exec.Command(opener, target)
	cmd.Stdout, cmd.Stderr = nil, nil
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("could not open the managed Magpie page: %w", err)
	}
	fmt.Printf("Opened the managed Magpie page at %s\n", record.WebAddress)
	return nil
}

// webRedirectFile writes the authenticated page target into a private file
// and returns the file URL to open. The key stays in the file, never in an
// argument list.

func webRedirectFile(layout managedLayout, target string) (string, error) {
	path := filepath.Join(layout.Root, "web.html")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return "", err
	}
	page := fmt.Sprintf("<!doctype html><meta http-equiv=\"refresh\" content=\"0; url=%s\">\n", html.EscapeString(target))
	if _, err := file.WriteString(page); err != nil {
		file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	if err := os.Chmod(path, 0600); err != nil {
		return "", err
	}
	// A path is not a URL: characters such as # or ? would change its meaning,
	// so the file URL is built from the path rather than concatenated.
	return (&url.URL{Scheme: "file", Path: path}).String(), nil
}

func browserOpener() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return "open", nil
	case "linux":
		return "xdg-open", nil
	default:
		return "", errors.New("opening a browser is not supported on this platform")
	}
}
