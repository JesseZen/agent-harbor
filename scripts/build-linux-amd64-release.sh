#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd -P)
VERSION=${AGENT_HARBOR_VERSION:-dev}
RELEASE_ROOT=${AGENT_HARBOR_RELEASE_ROOT:-"$ROOT/dist/release-linux-amd64-$VERSION"}

case "$RELEASE_ROOT" in /*) ;; *) echo "AGENT_HARBOR_RELEASE_ROOT must be absolute" >&2; exit 1;; esac
CORE="$ROOT/core/linux-amd64/agent-harbor-core"
test -x "$CORE" || { echo "bundled Linux amd64 Core binary is missing" >&2; exit 1; }
command -v docker >/dev/null 2>&1 || { echo "Docker is required" >&2; exit 1; }
docker info >/dev/null 2>&1 || { echo "Docker daemon is not running" >&2; exit 1; }
test ! -e "$RELEASE_ROOT" || { echo "release directory already exists: $RELEASE_ROOT" >&2; exit 1; }

# Docker-outside-of-Docker environments often expose /workspace but not $HOME.
# Fail before creating a partial release when the daemon cannot see our paths.
docker run --rm --platform linux/amd64 \
  -v "$ROOT:/agent-harbor:ro" \
  alpine:3.22 sh -c 'test -f /agent-harbor/tui/go.mod && test -x /agent-harbor/core/linux-amd64/agent-harbor-core' || {
    echo "Docker cannot read the repository bind mount. Place it under a daemon-shared path such as /workspace." >&2
    exit 1
  }
mkdir -p "$RELEASE_ROOT"

echo "release: building static tmux 3.6b for linux/amd64"
docker run --rm --platform linux/amd64 \
  -e HOST_UID="$(id -u)" -e HOST_GID="$(id -g)" \
  -v "$RELEASE_ROOT:/release" \
  alpine:3.22 sh -lc '
set -eu
apk add --no-cache build-base curl libevent-dev libevent-static ncurses-dev ncurses-static bison file >/dev/null
mkdir -p /work /release/tmux/linux-amd64 /release/terminfo/linux-amd64 /release/licenses
cd /work
curl -fsSL --retry 5 --retry-all-errors -o tmux.tar.gz https://github.com/tmux/tmux/releases/download/3.6b/tmux-3.6b.tar.gz
tar -xzf tmux.tar.gz
cd tmux-3.6b
./configure --enable-static LDFLAGS=-static >/dev/null
make -j2 >/dev/null
strip tmux
file tmux | grep -q "ELF 64-bit.*x86-64.*statically linked"
cp tmux /release/tmux/linux-amd64/tmux
printf "tmux 3.6b\n" > /release/tmux/linux-amd64/VERSION
cp COPYING /release/licenses/tmux-COPYING
cd /work
curl -fsSL --retry 5 --retry-all-errors -o libevent.tar.gz https://github.com/libevent/libevent/releases/download/release-2.1.12-stable/libevent-2.1.12-stable.tar.gz
tar -xzf libevent.tar.gz libevent-2.1.12-stable/LICENSE
cp libevent-2.1.12-stable/LICENSE /release/licenses/libevent-LICENSE
curl -fsSL --retry 5 --retry-all-errors -o ncurses.tar.gz https://invisible-island.net/archives/ncurses/ncurses-6.5.tar.gz
tar -xzf ncurses.tar.gz ncurses-6.5/COPYING
cp ncurses-6.5/COPYING /release/licenses/ncurses-COPYING
for term in xterm xterm-256color screen screen-256color tmux tmux-256color; do
  if infocmp -x "$term" > "/tmp/$term.src" 2>/dev/null; then
    tic -x -o /release/terminfo/linux-amd64 "/tmp/$term.src"
  fi
done
chmod 755 /release/tmux/linux-amd64/tmux
chown -R "$HOST_UID:$HOST_GID" /release
'

echo "release: staging bundled Core for linux/amd64"
mkdir -p "$RELEASE_ROOT/core/linux-amd64"
cp "$CORE" "$RELEASE_ROOT/core/linux-amd64/agent-harbor-core"
chmod 755 "$RELEASE_ROOT/core/linux-amd64/agent-harbor-core"

echo "release: assembling portable single file"
docker run --rm --platform linux/amd64 \
  -e AGENT_HARBOR_BUNDLE_TARGETS=linux-amd64 \
  -e AGENT_HARBOR_CORE_DIR=/release/core \
  -e AGENT_HARBOR_TMUX_DIR=/release/tmux \
  -e AGENT_HARBOR_TERMINFO_DIR=/release/terminfo \
  -e AGENT_HARBOR_TMUX_LICENSE_DIR=/release/licenses \
  -e AGENT_HARBOR_BUNDLE_OUTPUT_DIR=/release/out \
  -e AGENT_HARBOR_VERSION="$VERSION" \
  -e HOST_UID="$(id -u)" -e HOST_GID="$(id -g)" \
  -v "$ROOT:/agent-harbor:ro" -v "$RELEASE_ROOT:/release" \
  -w /agent-harbor golang:1.26-alpine sh -c '
set -eu
apk add --no-cache file perl-utils tar >/dev/null
sh scripts/build-bundle.sh
sh scripts/verify-bundles.sh
chown -R "$HOST_UID:$HOST_GID" /release/out
'

echo "release: ready"
echo "$RELEASE_ROOT/out/agent-harbor-linux-amd64"
