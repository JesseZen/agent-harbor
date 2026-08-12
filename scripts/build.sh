#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd -P)
TARGET=${AGENT_HARBOR_TARGET:-"$(go env GOOS)-$(go env GOARCH)"}
OUTPUT=${AGENT_HARBOR_OUTPUT_DIR:-"$ROOT/dist"}
CORE_DIR=${AGENT_HARBOR_CORE_DIR:-"$ROOT/core"}

case "$TARGET" in
  darwin-arm64|darwin-amd64|linux-arm64|linux-amd64) ;;
  *) printf 'build: unsupported target %s\n' "$TARGET" >&2; exit 1 ;;
esac

case "$OUTPUT" in
  /*) ;;
  *) printf 'build: output directory must be absolute\n' >&2; exit 1 ;;
esac

CORE="$CORE_DIR/$TARGET/agent-harbor-core"
test -x "$CORE" || {
  printf 'build: missing Core binary for %s\n' "$TARGET" >&2
  exit 1
}

mkdir -p "$OUTPUT" /tmp/agent-harbor-trash
STAGING=$(mktemp -d "$OUTPUT/.build.XXXXXX")
trap 'test ! -d "$STAGING" || mv "$STAGING" /tmp/agent-harbor-trash/build.$$' EXIT HUP INT TERM

go build -C "$ROOT/tui" -trimpath -buildvcs=false \
  -o "$STAGING/agent-harbor-tui" ./cmd/agent-harbor-tui
cp "$CORE" "$STAGING/agent-harbor-core"
cp "$SCRIPT_DIR/agent-harbor" "$STAGING/agent-harbor"
chmod 755 \
  "$STAGING/agent-harbor" \
  "$STAGING/agent-harbor-tui" \
  "$STAGING/agent-harbor-core"
mv "$STAGING/agent-harbor" "$OUTPUT/agent-harbor"
mv "$STAGING/agent-harbor-tui" "$OUTPUT/agent-harbor-tui"
mv "$STAGING/agent-harbor-core" "$OUTPUT/agent-harbor-core"
rmdir "$STAGING"
printf 'build: wrote %s\n' "$OUTPUT"
