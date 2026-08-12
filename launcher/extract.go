package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"syscall"

	"golang.org/x/sys/unix"
)

const privateDirMode = 0o700

func ensureRuntime(manifest bundleManifest, paths productPaths) error {
	if err := ensurePrivatePath(paths.RuntimeBase); err != nil {
		return err
	}
	lockPath := filepath.Join(paths.RuntimeBase, ".unpack.lock")
	lock, err := openPrivateFile(lockPath)
	if err != nil {
		return fmt.Errorf("open runtime lock: %w", err)
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		return fmt.Errorf("lock runtime: %w", err)
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	if err := verifyRuntime(manifest, paths.Runtime); err == nil {
		return nil
	}
	if _, err := os.Lstat(paths.Runtime); err == nil {
		return fmt.Errorf("runtime %s exists but failed integrity verification; remove it with runtime gc after preserving any diagnostics", paths.Runtime)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	temporary, err := os.MkdirTemp(paths.RuntimeBase, ".unpack-")
	if err != nil {
		return fmt.Errorf("create unpack staging directory: %w", err)
	}
	if err := os.Chmod(temporary, privateDirMode); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(temporary)
		}
	}()
	for _, component := range manifest.Components {
		if err := extractComponent(component, temporary); err != nil {
			return err
		}
		if component.Role == "data.terminfo" {
			if err := extractTerminfoArchive(filepath.Join(temporary, filepath.FromSlash(component.Path)), filepath.Join(temporary, "share", "terminfo")); err != nil {
				return err
			}
		}
	}
	if err := syncDirectory(temporary); err != nil {
		return err
	}
	if err := os.Rename(temporary, paths.Runtime); err != nil {
		return fmt.Errorf("commit runtime: %w", err)
	}
	committed = true
	if err := syncDirectory(paths.RuntimeBase); err != nil {
		return err
	}
	return verifyRuntime(manifest, paths.Runtime)
}

func extractComponent(component bundleComponent, root string) error {
	destination := filepath.Join(root, filepath.FromSlash(component.Path))
	if !pathWithin(root, destination) {
		return fmt.Errorf("component escapes runtime: %s", component.Path)
	}
	if err := ensurePrivatePath(filepath.Dir(destination)); err != nil {
		return err
	}
	source, err := embeddedPayload.Open("payload/files/" + component.Path)
	if err != nil {
		return err
	}
	defer source.Close()
	mode := fs.FileMode(component.Mode)
	if component.Executable {
		mode = 0o700
	} else if mode.Perm() == 0 {
		mode = 0o600
	}
	fd, err := unix.Open(destination, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(mode.Perm()))
	if err != nil {
		return fmt.Errorf("create component %s: %w", component.Path, err)
	}
	file := os.NewFile(uintptr(fd), destination)
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(source, component.Size+1))
	syncErr := file.Sync()
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written != component.Size || hex.EncodeToString(hash.Sum(nil)) != component.SHA256 {
		return fmt.Errorf("embedded component integrity mismatch: %s", component.Path)
	}
	return nil
}

func verifyRuntime(manifest bundleManifest, root string) error {
	info, err := secureInfo(root, true)
	if err != nil || info.Mode().Perm() != privateDirMode {
		return fmt.Errorf("runtime root is not a private owned directory")
	}
	for _, component := range manifest.Components {
		name := filepath.Join(root, filepath.FromSlash(component.Path))
		info, err := secureInfo(name, false)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("invalid runtime component %s", component.Path)
		}
		if info.Size() != component.Size {
			return fmt.Errorf("runtime component length mismatch: %s", component.Path)
		}
		file, err := os.Open(name)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, copyErr := io.Copy(hash, io.LimitReader(file, component.Size+1))
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil || hex.EncodeToString(hash.Sum(nil)) != component.SHA256 {
			return fmt.Errorf("runtime component hash mismatch: %s", component.Path)
		}
	}
	if component, ok := manifest.componentByRole("data.terminfo"); ok {
		_ = component
		if info, err := secureInfo(filepath.Join(root, "share", "terminfo"), true); err != nil || info.Mode().Perm() != privateDirMode {
			return errors.New("invalid extracted terminfo directory")
		}
	}
	return nil
}

func extractTerminfoArchive(archivePath, destination string) error {
	if err := ensurePrivatePath(destination); err != nil {
		return err
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	var total int64
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		clean := filepath.Clean(filepath.FromSlash(header.Name))
		if clean == "." {
			continue
		}
		target := filepath.Join(destination, clean)
		if !pathWithin(destination, target) {
			return fmt.Errorf("terminfo archive path escapes destination: %s", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := ensurePrivatePath(target); err != nil {
				return err
			}
		case tar.TypeReg:
			total += header.Size
			if header.Size < 0 || header.Size > maxPortableFile || total > maxPortableTotal {
				return errors.New("terminfo archive exceeds size limit")
			}
			if err := ensurePrivatePath(filepath.Dir(target)); err != nil {
				return err
			}
			output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return err
			}
			written, copyErr := io.Copy(output, io.LimitReader(tarReader, header.Size+1))
			syncErr := output.Sync()
			closeErr := output.Close()
			if copyErr != nil || syncErr != nil || closeErr != nil || written != header.Size {
				return errors.New("failed to extract terminfo entry")
			}
		default:
			return fmt.Errorf("terminfo archive contains unsupported entry %s", header.Name)
		}
	}
	return nil
}

func ensurePrivatePath(path string) error {
	clean := filepath.Clean(path)
	volume := filepath.VolumeName(clean) + string(os.PathSeparator)
	relative, err := filepath.Rel(volume, clean)
	if err != nil {
		return err
	}
	current := volume
	for _, part := range splitPath(relative) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			if err := os.Mkdir(current, privateDirMode); err != nil && !errors.Is(err, fs.ErrExist) {
				return err
			}
			info, err = os.Lstat(current)
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe directory in private path: %s", current)
		}
		if sameUID(info) && current != volume {
			// Existing user-owned application directories are tightened; ancestors such as /Users are not.
			if filepath.Clean(current) == filepath.Clean(path) && info.Mode().Perm() != privateDirMode {
				if err := os.Chmod(current, privateDirMode); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func splitPath(path string) []string {
	var parts []string
	for path != "." && path != string(os.PathSeparator) && path != "" {
		dir, base := filepath.Split(path)
		if base != "" {
			parts = append([]string{base}, parts...)
		}
		path = filepath.Clean(dir)
	}
	return parts
}
func secureInfo(path string, directory bool) (fs.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !sameUID(info) {
		return nil, fmt.Errorf("path is a symlink or belongs to another UID: %s", path)
	}
	if directory != info.IsDir() {
		return nil, fmt.Errorf("unexpected path type: %s", path)
	}
	return info, nil
}
func sameUID(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}
func pathWithin(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !filepath.IsAbs(rel) && rel != "." && !startsDotDot(rel)
}
func startsDotDot(rel string) bool {
	return len(rel) >= 3 && rel[:2] == ".." && os.IsPathSeparator(rel[2])
}
func openPrivateFile(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil || !sameUID(info) || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		file.Close()
		return nil, fmt.Errorf("private file ownership/type check failed: %s", path)
	}
	return file, nil
}
func syncDirectory(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

func runtimeExecutable(manifest bundleManifest, paths productPaths, role string) (string, error) {
	component, ok := manifest.componentByRole(role)
	if !ok {
		return "", fmt.Errorf("bundle is missing required role %s", role)
	}
	return filepath.Join(paths.Runtime, filepath.FromSlash(component.Path)), nil
}

func noExecHint(path string, err error) error {
	if errors.Is(err, syscall.EACCES) && runtime.GOOS == "linux" {
		return fmt.Errorf("execute %s: %w (the runtime filesystem may be mounted noexec; set AGENT_HARBOR_RUNTIME_DIR to an executable private filesystem)", path, err)
	}
	return err
}
