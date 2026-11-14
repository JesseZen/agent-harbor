package main

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Magpie decides where its files live once, when it starts, from the
// executable it runs from and its own environment. Harbor repeats that rule
// from the same evidence instead of guessing from an address or display name.
const (
	configEvidenceInstalled      = "installed configuration"
	configEvidencePortableMarker = "portable marker"
	configEvidencePortableData   = "portable data folder"
)

const (
	configUnknown = "the gateway process did not provide a home folder, so its configuration location cannot be determined"

	// configSharedDataPassedOver is Magpie's own warning: a data folder in a
	// shared folder holding files that the installed folder holds too is not
	// taken as portable, so Magpie reads the installed folder instead.
	configSharedDataPassedOver = "the portable data folder beside magpie is in a shared folder and the installed folder also holds Magpie files, so Magpie reads the installed folder"
)

type configResolution struct {
	Location string
	Evidence string
	Warning  string
	Reason   string
}

// magpieConfigFiles are the files whose presence means a data folder (or an
// installed folder) already holds Magpie's own state. Magpie uses the same
// three names to tell its data folder from another application's.
var magpieConfigFiles = []string{"providers.json", "logins.json", "settings.json"}

func resolveMagpieConfig(executable string, variables map[string]string) configResolution {
	resolved := executable
	if real, err := filepath.EvalSymlinks(executable); err == nil {
		resolved = real
	}
	base := magpieBeside(resolved, variables)
	data := filepath.Join(base, "data")
	if info, err := os.Stat(filepath.Join(base, ".portable")); err == nil && !info.IsDir() {
		return configResolution{Location: data, Evidence: configEvidencePortableMarker}
	}
	if info, err := os.Stat(data); err == nil && info.IsDir() {
		if !sharedMagpieFolder(base, variables) {
			return configResolution{Location: data, Evidence: configEvidencePortableData}
		}
		if magpieFilesPresent(data) {
			installed, ok := installedMagpieConfig(variables)
			if !ok {
				return configResolution{Reason: configUnknown}
			}
			if magpieFilesPresent(installed) {
				// Magpie warns and passes the data folder over, then reads the
				// installed folder; it never refuses to choose.
				return configResolution{Location: installed, Evidence: configEvidenceInstalled, Warning: configSharedDataPassedOver}
			}
			return configResolution{Location: data, Evidence: configEvidencePortableData}
		}
	}
	installed, ok := installedMagpieConfig(variables)
	if !ok {
		return configResolution{Reason: configUnknown}
	}
	return configResolution{Location: installed, Evidence: configEvidenceInstalled}
}

func installedMagpieConfig(variables map[string]string) (string, bool) {
	if xdg := variables["XDG_CONFIG_HOME"]; filepath.IsAbs(xdg) {
		return filepath.Join(xdg, "magpie"), true
	}
	home := variables["HOME"]
	if !filepath.IsAbs(home) {
		return "", false
	}
	return filepath.Join(home, ".config", "magpie"), true
}

// magpieBeside is the folder Magpie's portable data folder sits in: the
// executable's own, the one holding the .app bundle on macOS, or the one
// holding the .AppImage on Linux.
func magpieBeside(executable string, variables map[string]string) string {
	if runtime.GOOS == "linux" && variables["APPIMAGE"] != "" && withinFolder(executable, variables["APPDIR"]) {
		return filepath.Dir(variables["APPIMAGE"])
	}
	dir := filepath.Dir(executable)
	if filepath.Base(dir) == "MacOS" && filepath.Base(filepath.Dir(dir)) == "Contents" {
		if app := filepath.Dir(filepath.Dir(dir)); strings.EqualFold(filepath.Ext(app), ".app") {
			return filepath.Dir(app)
		}
	}
	return dir
}

func withinFolder(path, dir string) bool {
	if !filepath.IsAbs(dir) {
		return false
	}
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// sharedMagpieFolder is whether dir is one every application or download
// shares, where a folder named data is as likely someone else's.
func sharedMagpieFolder(dir string, variables map[string]string) bool {
	home := variables["HOME"]
	if !filepath.IsAbs(home) {
		return false
	}
	folders := []string{home}
	for _, name := range []string{"Downloads", "Desktop", "Documents"} {
		folders = append(folders, filepath.Join(home, name))
	}
	if runtime.GOOS == "darwin" {
		folders = append(folders, "/Applications", filepath.Join(home, "Applications"))
	}
	if runtime.GOOS == "linux" {
		folders = append(folders, linuxUserFolders(home, variables)...)
	}
	for _, folder := range folders {
		if strings.EqualFold(filepath.Clean(dir), filepath.Clean(folder)) {
			return true
		}
	}
	return false
}

func magpieFilesPresent(dir string) bool {
	for _, name := range magpieConfigFiles {
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

// linuxUserFolders reads the folders a Linux desktop may have moved off the
// home folder, so a Downloads data folder is still recognized as shared.
func linuxUserFolders(home string, variables map[string]string) []string {
	config := variables["XDG_CONFIG_HOME"]
	if !filepath.IsAbs(config) {
		config = filepath.Join(home, ".config")
	}
	file, err := os.Open(filepath.Join(config, "user-dirs.dirs"))
	if err != nil {
		return nil
	}
	defer file.Close()
	var folders []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if !ok {
			continue
		}
		switch key {
		case "XDG_DOWNLOAD_DIR", "XDG_DESKTOP_DIR", "XDG_DOCUMENTS_DIR":
		default:
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"`)
		value = strings.ReplaceAll(value, "${HOME}", home)
		value = strings.ReplaceAll(value, "$HOME", home)
		if filepath.IsAbs(value) {
			folders = append(folders, filepath.Clean(value))
		}
	}
	return folders
}
