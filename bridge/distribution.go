package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

func distributionCommand(args []string) error {
	if len(args) != 1 && !(args[0] == "update" && len(args) == 3 && args[1] == "--version") {
		return errors.New("usage: update [--version TAG] | uninstall")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return err
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(executable)))
	marker, err := os.ReadFile(filepath.Join(root, ".agent-harbor-distribution"))
	if err != nil || string(marker) != "agent-harbor-distribution-v1\n" {
		return errors.New("this executable is not a script installation; for Homebrew use brew upgrade agent-harbor or brew uninstall agent-harbor")
	}
	command := append([]string{filepath.Join(root, "manage.sh")}, args...)
	command = append(command, "--prefix", root)
	cmd := exec.Command("sh", command...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// Resolve command links so bundled dependencies and popup entry points stay in
// the versioned package, including on macOS where os.Executable can retain a link.
func resolvedExecutable() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(executable)
}
