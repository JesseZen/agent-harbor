package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type packageEntry struct {
	selector string
	versions string
	suffix   string
	marker   bool
}

var packageVersionName = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

func (p packageEntry) ownsVersion(path string) bool {
	relative, err := filepath.Rel(p.versions, path)
	if p.selector == "" || err != nil || filepath.Clean(path) != path {
		return false
	}
	version, _, _ := strings.Cut(relative, string(filepath.Separator))
	return version != "." && version != ".." && packageVersionName.MatchString(version) && relative == filepath.Join(version, p.suffix)
}

func regularContent(path, content string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(content)) {
		return false
	}
	data, err := os.ReadFile(path)
	return err == nil && string(data) == content
}

func packageEntryFor(executable string) (packageEntry, error) {
	dir := filepath.Dir(executable)
	versions := filepath.Dir(dir)
	root := filepath.Dir(versions)
	var p packageEntry
	switch {
	case filepath.Base(versions) == "releases" && regularContent(filepath.Join(root, ".agent-harbor-distribution"), "agent-harbor-distribution-v1\n"):
		p = packageEntry{filepath.Join(root, "current", "agent-harbor"), versions, "agent-harbor", true}
	case filepath.Base(dir) == "libexec" && filepath.Base(root) == "agent-harbor" && filepath.Base(filepath.Dir(root)) == "Cellar":
		prefix := filepath.Dir(filepath.Dir(root))
		p = packageEntry{filepath.Join(prefix, "opt", "agent-harbor", "libexec", "agent-harbor"), root, filepath.Join("libexec", "agent-harbor"), false}
	default:
		return p, nil
	}
	if err := p.verify(executable); err != nil {
		return packageEntry{}, err
	}
	return p, nil
}

func (p packageEntry) verify(executable string) error {
	selectorDir := filepath.Dir(p.selector)
	if !p.marker {
		selectorDir = filepath.Dir(selectorDir)
	}
	if _, err := os.Readlink(selectorDir); err != nil {
		return errors.New("package current or opt must be an installation symlink")
	}
	selectedDir, err := filepath.EvalSymlinks(selectorDir)
	if err != nil || filepath.Dir(selectedDir) != p.versions || !p.ownsVersion(executable) || executable != filepath.Join(selectedDir, p.suffix) {
		return errors.New("package current or opt does not select this Harbor installation")
	}
	if p.marker && !regularContent(filepath.Join(selectedDir, ".agent-harbor-package"), "agent-harbor-package-v1\n") {
		return errors.New("package current lacks the Harbor package ownership marker")
	}
	leafDir := filepath.Dir(executable)
	if real, err := filepath.EvalSymlinks(leafDir); err != nil || real != leafDir {
		return errors.New("package executable directory is redirected")
	}
	for _, name := range []string{"agent-harbor", "herdr", "magpie", "codex-auth"} {
		info, err := os.Lstat(filepath.Join(leafDir, name))
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			return fmt.Errorf("package companion %s is missing or redirected", name)
		}
	}
	return nil
}

func workspacePackageEntry(root string) string {
	return filepath.Join(root, "herdr", "bin", "agent-harbor")
}

func (p packageEntry) installWorkspaceEntry(path string) error {
	contents := p.workspaceScript()
	if regularContent(path, contents) {
		return workspaceEntryMode(path)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		return errors.New("workspace Harbor entry is owned by another installation or was modified")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return writeOwnedFile(path, []byte(contents), 0700)
}

func workspaceEntryMode(path string) error {
	if info, err := os.Lstat(path); err == nil && info.Mode().Perm() == 0700 {
		return nil
	}
	return errors.New("workspace Harbor entry must be executable and private")
}

func (p packageEntry) workspaceScript() string {
	selector := filepath.Dir(p.selector)
	herdrSuffix := filepath.Join(filepath.Dir(p.suffix), "herdr")
	checks := `test ! -L "$release/libexec" || fail`
	if p.marker {
		root := filepath.Dir(p.versions)
		checks = fmt.Sprintf(`test ! -L %s && test "$(/bin/cat %s)" = agent-harbor-distribution-v1 || fail
test ! -L "$release/.agent-harbor-package" && test "$(/bin/cat "$release/.agent-harbor-package")" = agent-harbor-package-v1 || fail`, shellQuote(filepath.Join(root, ".agent-harbor-distribution")), shellQuote(filepath.Join(root, ".agent-harbor-distribution")))
	} else {
		selector = filepath.Dir(selector)
	}
	return fmt.Sprintf(`#!/bin/sh
fail() { printf 'agent-harbor: workspace package selector is invalid\n' >&2; exit 1; }
selector=%s
versions=%s
test -L "$selector" || fail
release=$(CDPATH= cd -P -- "$selector" 2>/dev/null && pwd -P) || fail
test "${release%%/*}" != "$release" || fail
test "${release%%/*}" = "$versions" || fail
case "${release##*/}" in ''|.|..|*[!a-zA-Z0-9._-]*) fail;; esac
%s
bin="$release"/%s
for name in agent-harbor herdr magpie codex-auth; do
    test -f "${bin%%/*}/$name" && test -x "${bin%%/*}/$name" && test ! -L "${bin%%/*}/$name" || fail
done
case "${HERDR_BIN_PATH:-}" in
    "$versions"/*/%s) HERDR_BIN_PATH="${bin%%/*}/herdr"; export HERDR_BIN_PATH;;
esac
if test "${1:-}" = _workspace-herdr; then
    shift
    exec "${bin%%/*}/herdr" "$@"
fi
exec "$bin" "$@"
`, shellQuote(selector), shellQuote(p.versions), checks, shellQuote(p.suffix), shellQuote(herdrSuffix))
}
