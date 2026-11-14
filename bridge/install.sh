#!/bin/sh
# Installs only into a dedicated prefix; never changes configuration or services.
set -eu
usage() { echo 'Usage: install.sh install|uninstall --prefix ABSOLUTE_PATH [--link]' >&2; exit 2; }
[ "$#" -eq 3 ] || [ "$#" -eq 4 ] || usage
link_requested=false
if [ "$#" -eq 4 ]; then
  [ "$4" = --link ] && [ "$1" = install ] || usage
  link_requested=true
fi
operation=$1
[ "$2" = --prefix ] || usage
prefix=$3
case "$prefix" in /*) ;; *) usage ;; esac
case "$prefix" in /|*/../*|*/..|*/./*|*/.) usage ;; esac
source_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
marker=$prefix/.agent-harbor-package
case "$operation" in
install)
  # Refuse populated or symlink destinations instead of overwriting other work.
  [ ! -L "$prefix" ] || { echo 'Prefix must not be a symlink' >&2; exit 1; }
  if [ -e "$prefix" ]; then
    [ -d "$prefix" ] && [ -z "$(ls -A "$prefix")" ] || { echo 'Choose an empty dedicated prefix' >&2; exit 1; }
  fi
  for file in agent-harbor herdr magpie README.md; do [ -f "$source_dir/$file" ] || exit 1; done
  link=$HOME/.local/bin/agent-harbor
  if "$link_requested"; then
    [ ! -e "$link" ] && [ ! -L "$link" ] || { echo "PATH link already exists" >&2; exit 1; }
  fi
  mkdir -p "$prefix"
  for file in agent-harbor herdr magpie README.md; do cp "$source_dir/$file" "$prefix/$file"; done
  cp -R "$source_dir/licenses" "$prefix/licenses"
  cp "$source_dir/install.sh" "$prefix/install.sh"
  printf '%s\n' 'agent-harbor-package-v1' > "$marker"
  if "$link_requested"; then
    mkdir -p "$HOME/.local/bin"
    ln -s "$prefix/agent-harbor" "$link"
  fi
  printf 'Installed. Run: %s/agent-harbor\n' "$prefix"
  ;;
uninstall)
  [ ! -L "$prefix" ] && [ -f "$marker" ] && [ "$(cat "$marker")" = agent-harbor-package-v1 ] || { echo 'Prefix is not an owned installation' >&2; exit 1; }
  # Remove only known package files, preserving any added files and all state.
  for file in agent-harbor herdr magpie README.md install.sh .agent-harbor-package; do
    [ ! -d "$prefix/$file" ] || { echo 'Package file was replaced by a directory' >&2; exit 1; }
  done
  link=$HOME/.local/bin/agent-harbor
  if [ -L "$link" ] && [ "$(readlink "$link")" = "$prefix/agent-harbor" ]; then
    unlink "$link"
  fi
  for file in agent-harbor herdr magpie README.md install.sh .agent-harbor-package; do unlink "$prefix/$file"; done
  echo 'Removed executables; licenses and any added files remain in the prefix.'
  ;;
*) usage ;;
esac
