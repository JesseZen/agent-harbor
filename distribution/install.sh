#!/bin/sh
# shellcheck disable=SC2015,SC2016
# User-owned, versioned installation. No sudo, shell edits or client state writes.
set -eu
fail() { echo "agent-harbor: $*" >&2; exit 1; }
operation=install
case "${1:-}" in install|update|uninstall) operation=$1; shift ;; esac
prefix=${AGENT_HARBOR_INSTALL_ROOT:-"$HOME/.local/share/agent-harbor"}
bin_dir=${AGENT_HARBOR_BIN_DIR:-"$HOME/.local/bin"}
release=${AGENT_HARBOR_VERSION:-}
while [ "$#" -gt 0 ]; do
  case "$1" in
    --prefix) [ "$#" -ge 2 ] || fail 'missing prefix'; prefix=$2; shift 2 ;;
    --bin-dir) [ "$#" -ge 2 ] || fail 'missing bin directory'; bin_dir=$2; shift 2 ;;
    --version) [ "$#" -ge 2 ] || fail 'missing version'; release=$2; shift 2 ;;
    *) fail 'Usage: install.sh [install|update|uninstall] [--version TAG] [--prefix PATH] [--bin-dir PATH]' ;;
  esac
done
for path in "$prefix" "$bin_dir"; do
  case "$path" in /*) ;; *) fail 'paths must be absolute' ;; esac
  case "$path" in /|*/../*|*/..|*/./*|*/.|*'
'*) fail 'unsafe path' ;; esac
done
prefix=${prefix%/}
bin_dir=${bin_dir%/}
[ ! -L "$prefix" ] || fail 'installation root must not be a symlink'
marker=$prefix/.agent-harbor-distribution
owned=false
if [ -f "$marker" ] && [ "$(cat "$marker")" = agent-harbor-distribution-v1 ]; then
  owned=true
  bin_dir=$(cat "$prefix/bin-dir")
elif [ -e "$prefix" ]; then
  [ -d "$prefix" ] && [ -z "$(ls -A "$prefix")" ] || fail 'installation root is not an owned installation or empty directory'
fi
if [ "$operation" = uninstall ]; then
  "$owned" && [ -L "$prefix/current" ] || fail 'not an installed distribution'
  mkdir "$prefix/.install-lock" 2>/dev/null || fail 'another installation is running'
  trap 'rmdir "$prefix/.install-lock"' EXIT HUP INT TERM
  for name in ah agent-harbor; do
    link=$bin_dir/$name
    if [ -L "$link" ] && [ "$(readlink "$link")" = "$prefix/current/agent-harbor" ]; then unlink "$link"; fi
  done
  for package in "$prefix"/releases/*; do
    [ ! -L "$package" ] || fail 'unexpected symlink in releases'
    [ -f "$package/.agent-harbor-package" ] || continue
    sh "$package/install.sh" uninstall --prefix "$package"
    [ ! -f "$package/manage.sh" ] || unlink "$package/manage.sh"
  done
  [ ! -L "$prefix/current" ] || unlink "$prefix/current"
  # Retain ownership metadata and the manager so reinstall can preserve added files.
  echo 'Uninstalled commands and executables. Accounts, configuration, histories and licenses were kept.'
  exit
fi
[ "$operation" != update ] || "$owned" || fail 'update requires an owned installation'
for name in ah agent-harbor; do
  link=$bin_dir/$name
  if [ -e "$link" ] || [ -L "$link" ]; then
    [ -L "$link" ] && [ "$(readlink "$link")" = "$prefix/current/agent-harbor" ] && "$owned" || fail "command already exists: $link"
  fi
done
case "$(uname -s)" in Darwin) platform=darwin ;; Linux) platform=linux ;; *) fail 'supported systems: macOS and Linux' ;; esac
case "$(uname -m)" in arm64|aarch64) arch=arm64 ;; x86_64|amd64) arch=amd64 ;; *) fail 'supported architectures: arm64 and amd64' ;; esac
# Overrides are also used by the isolated local HTTP acceptance fixture.
raw=${AGENT_HARBOR_CHANNEL_URL:-https://raw.githubusercontent.com/JesseZen/agent-harbor/main/distribution/latest}
base=${AGENT_HARBOR_DOWNLOAD_BASE:-https://github.com/JesseZen/agent-harbor/releases/download}
fetch() { curl --fail --location --silent --show-error --retry 3 --connect-timeout 15 "$1" -o "$2"; }
mkdir -p "$prefix"
mkdir "$prefix/.install-lock" 2>/dev/null || fail 'another installation is running'
stage=$(mktemp -d "$prefix/.download.XXXXXX")
cleanup() { rm -rf "$stage"; rmdir "$prefix/.install-lock"; }
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
if [ -z "$release" ]; then fetch "$raw" "$stage/latest"; release=$(cat "$stage/latest"); fi
case "$release" in ''|*[!a-zA-Z0-9._-]*|.|..) fail 'invalid release tag' ;; esac
asset=agent-harbor-$release-$platform-$arch.tar.gz
fetch "$base/$release/SHA256SUMS" "$stage/SHA256SUMS"
fetch "$base/$release/$asset" "$stage/$asset"
expected=$(awk -v name="$asset" '$2 == name {print $1}' "$stage/SHA256SUMS")
case "$expected" in ''|*[!a-f0-9]*) fail 'missing or invalid checksum' ;; esac
[ "${#expected}" = 64 ] || fail 'invalid checksum length'
if command -v sha256sum >/dev/null 2>&1; then actual=$(sha256sum "$stage/$asset" | cut -d ' ' -f 1); else actual=$(shasum -a 256 "$stage/$asset" | cut -d ' ' -f 1); fi
[ "$actual" = "$expected" ] || fail 'archive checksum mismatch; installation unchanged'
# Trusted release archive, with an explicit path allowlist before extraction.
tar -tzf "$stage/$asset" > "$stage/entries"
while IFS= read -r entry; do
  case "$entry" in agent-harbor|herdr|magpie|codex-auth|README.md|install.sh|manage.sh|licenses|licenses/|licenses/*) ;; *) fail 'unexpected archive entry' ;; esac
  case "$entry" in *../*|*/..|/*) fail 'unsafe archive path' ;; esac
done < "$stage/entries"
mkdir "$stage/package"
tar -xzf "$stage/$asset" -C "$stage/package"
for name in agent-harbor herdr magpie codex-auth install.sh manage.sh README.md; do
  [ -f "$stage/package/$name" ] && [ ! -L "$stage/package/$name" ] || fail "missing regular file: $name"
done
mkdir -p "$prefix/releases" "$bin_dir"
destination=$prefix/releases/$release
if [ -d "$destination" ] && [ ! -f "$destination/agent-harbor" ]; then
  destination=$(mktemp -d "$prefix/releases/$release.XXXXXX")
fi
if [ ! -f "$destination/agent-harbor" ]; then
  sh "$stage/package/install.sh" install --prefix "$destination"
  cp "$stage/package/manage.sh" "$destination/manage.sh"
else
  [ ! -L "$destination" ] && [ -f "$destination/.agent-harbor-package" ] || fail 'release directory is not owned'
  cmp "$stage/package/agent-harbor" "$destination/agent-harbor" >/dev/null || fail 'release tag changed; refusing replacement'
fi
cp "$stage/package/manage.sh" "$prefix/manage.sh.next"
mv -f "$prefix/manage.sh.next" "$prefix/manage.sh"
printf '%s\n' "$bin_dir" > "$prefix/bin-dir"
printf '%s\n' agent-harbor-distribution-v1 > "$marker"
ln -s "releases/${destination##*/}" "$stage/current"
case "$platform" in darwin) mv -fh "$stage/current" "$prefix/current" ;; *) mv -fT "$stage/current" "$prefix/current" ;; esac
for name in ah agent-harbor; do
  if [ ! -L "$bin_dir/$name" ]; then ln -s "$prefix/current/agent-harbor" "$bin_dir/$name"; fi
done
echo "Installed $release. Run: ah (or agent-harbor)"
case ":$PATH:" in *":$bin_dir:"*) ;; *) printf 'Add to your shell profile: export PATH="%s:$PATH"\n' "$bin_dir" ;; esac
echo 'Update: ah update   Uninstall: ah uninstall'
echo 'Run ah, then close and reopen the popup to use the updated Harbor. Restart the workspace explicitly to replace its Herdr server.'
