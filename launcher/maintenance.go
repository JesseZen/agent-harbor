package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

func commandDoctor(manifest bundleManifest, paths productPaths, args []string, stdout io.Writer) error {
	repairLaunchers := false
	if len(args) == 1 && args[0] == "--repair-launchers" {
		repairLaunchers = true
	} else if len(args) != 0 {
		return usageError("usage: agent-harbor doctor [--repair-launchers]")
	}
	failures := 0
	report := func(ok bool, name, detail string) {
		mark := "ok"
		if !ok {
			mark = "FAIL"
			failures++
		}
		fmt.Fprintf(stdout, "[%s] %s: %s\n", mark, name, detail)
	}
	if err := ensureRuntime(manifest, paths); err != nil {
		report(false, "portable runtime", err.Error())
	} else {
		report(true, "portable runtime", paths.Runtime)
	}
	if err := verifyRuntime(manifest, paths.Runtime); err != nil {
		report(false, "payload integrity", err.Error())
	} else {
		report(true, "payload integrity", "all declared SHA-256 hashes match")
	}
	for _, role := range []string{"runtime.core", "frontend.tui", "dependency.tmux"} {
		_, err := runtimeExecutable(manifest, paths, role)
		report(err == nil, role, firstError(err, "present"))
	}
	if root := terminfoRoot(manifest, paths); root == "" {
		report(false, "terminfo", "bundle has no data.terminfo role")
	} else {
		report(true, "terminfo", root)
	}
	for _, tool := range []string{"codex", "claude"} {
		detail, err := inspectExternalLauncher(tool, repairLaunchers)
		if err != nil {
			report(false, "external "+tool, err.Error())
		} else {
			report(true, "external "+tool, detail)
		}
	}
	config := filepath.Join(paths.ConfigRoot, "config.yaml")
	if info, err := secureInfo(config, false); err != nil || info.Mode().Perm() != 0o600 {
		report(false, "Core config", "missing or not an owned mode 0600 regular file")
	} else {
		report(true, "Core config", config)
	}
	requirements, _ := scanExternalRequirements(config)
	for _, requirement := range requirements {
		report(requirement.Available, "external secret "+requirement.Kind, requirement.Detail)
	}
	if failures != 0 {
		return fmt.Errorf("doctor found %d problem(s)", failures)
	}
	return nil
}

type launcherFile struct {
	Path string
	UID  uint32
}

func inspectExternalLauncher(tool string, repair bool) (string, error) {
	files, err := externalLauncherFiles(tool)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", fmt.Errorf("not found on PATH (install/login remains external to the portable bundle)")
		}
		return "", err
	}
	var repaired []string
	for _, file := range files {
		if trustedLauncherUID(file.UID, uint32(os.Geteuid())) {
			continue
		}
		if !repair {
			return "", fmt.Errorf("untrusted executable ownership: %s belongs to UID %d while Core runs as UID %d; run agent-harbor doctor --repair-launchers as root", file.Path, file.UID, os.Geteuid())
		}
		if os.Geteuid() != 0 {
			return "", fmt.Errorf("cannot repair %s as UID %d; run the repair as root", file.Path, os.Geteuid())
		}
		if err := os.Chown(file.Path, 0, 0); err != nil {
			return "", fmt.Errorf("repair launcher ownership %s: %w", file.Path, err)
		}
		repaired = append(repaired, file.Path)
	}
	detail := files[0].Path
	if len(files) > 1 {
		detail += " (interpreter " + files[1].Path + ")"
	}
	if len(repaired) > 0 {
		detail += "; repaired ownership: " + strings.Join(repaired, ", ")
	}
	return detail, nil
}

func externalLauncherFiles(tool string) ([]launcherFile, error) {
	launcher, contents, err := resolveLauncherFile(tool)
	if err != nil {
		return nil, err
	}
	files := []launcherFile{launcher}
	interpreter, err := externalLauncherInterpreter(contents)
	if err != nil {
		return nil, err
	}
	if interpreter != "" {
		resolved, _, err := resolveLauncherFile(interpreter)
		if err != nil {
			return nil, fmt.Errorf("launcher interpreter %q: %w", interpreter, err)
		}
		files = append(files, resolved)
	}
	return files, nil
}

func resolveLauncherFile(name string) (launcherFile, []byte, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return launcherFile{}, nil, err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return launcherFile{}, nil, fmt.Errorf("resolve launcher %s: %w", path, err)
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 || info.Mode().Perm()&0o022 != 0 {
		return launcherFile{}, nil, fmt.Errorf("launcher must be a non-writable executable regular file: %s", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return launcherFile{}, nil, fmt.Errorf("launcher ownership unavailable: %s", path)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return launcherFile{}, nil, fmt.Errorf("read launcher %s: %w", path, err)
	}
	return launcherFile{Path: path, UID: stat.Uid}, contents, nil
}

func trustedLauncherUID(owner, effective uint32) bool {
	return owner == 0 || owner == effective
}

func externalLauncherInterpreter(contents []byte) (string, error) {
	if len(contents) < 2 || contents[0] != '#' || contents[1] != '!' {
		return "", nil
	}
	line := strings.SplitN(string(contents[2:]), "\n", 2)[0]
	fields := strings.Fields(line)
	if len(fields) == 0 || len(fields) > 2 {
		return "", fmt.Errorf("invalid launcher shebang")
	}
	interpreter := filepath.Base(fields[0])
	if interpreter == "sh" || interpreter == "bash" || interpreter == "zsh" || interpreter == "fish" {
		return "", fmt.Errorf("shell launcher is not allowed")
	}
	if interpreter == "env" {
		if len(fields) != 2 || (fields[1] != "node" && fields[1] != "bun") {
			return "", fmt.Errorf("unsupported env launcher")
		}
		return fields[1], nil
	}
	if interpreter != "node" && interpreter != "bun" {
		return "", fmt.Errorf("unsupported launcher interpreter")
	}
	return fields[0], nil
}
func firstError(err error, fallback string) string {
	if err != nil {
		return err.Error()
	}
	return fallback
}

func commandLicenses(manifest bundleManifest, args []string, stdout io.Writer) error {
	if len(args) != 0 {
		return usageError("licenses accepts no arguments")
	}
	if len(manifest.Components) == 0 {
		fmt.Fprintln(stdout, "Agent Harbor launcher: MIT License (see launcher/LICENSE)")
		return nil
	}
	seen := map[string]bool{}
	for _, component := range manifest.Components {
		if component.License == "" || seen[component.License] {
			continue
		}
		seen[component.License] = true
		fmt.Fprintf(stdout, "===== %s (%s) =====\n", component.Role, component.License)
		if source, err := embeddedPayload.ReadFile("payload/licenses/" + component.License); err == nil {
			_, _ = stdout.Write(source)
			if len(source) == 0 || source[len(source)-1] != '\n' {
				fmt.Fprintln(stdout)
			}
		} else {
			fmt.Fprintf(stdout, "License identifier: %s\n", component.License)
		}
	}
	return nil
}

func commandRuntimeGC(manifest bundleManifest, paths productPaths, args []string, stdout io.Writer) error {
	if len(args) != 0 {
		return usageError("runtime gc accepts no arguments")
	}
	entries, err := os.ReadDir(paths.RuntimeBase)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var removed []string
	for _, entry := range entries {
		if entry.Name() == manifest.BundleID || strings.HasPrefix(entry.Name(), ".") || !entry.IsDir() {
			continue
		}
		target := filepath.Join(paths.RuntimeBase, entry.Name())
		info, err := secureInfo(target, true)
		if err != nil || info.Mode().Perm() != 0o700 {
			return fmt.Errorf("refusing to remove unsafe runtime directory %s", target)
		}
		if err := removeOwnedTree(target); err != nil {
			return err
		}
		removed = append(removed, entry.Name())
	}
	sort.Strings(removed)
	if len(removed) == 0 {
		fmt.Fprintln(stdout, "No old portable runtimes to remove.")
	} else {
		fmt.Fprintf(stdout, "Removed old portable runtimes: %s\n", strings.Join(removed, ", "))
	}
	return nil
}
func removeOwnedTree(root string) error {
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !sameUID(info) {
			return fmt.Errorf("unsafe entry in runtime tree: %s", path)
		}
		return nil
	}); err != nil {
		return err
	}
	return removeTree(root)
}
func removeTree(root string) error { return os.RemoveAll(root) }
