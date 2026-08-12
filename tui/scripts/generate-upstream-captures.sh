#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
UPSTREAM_COMMIT="2eedbc1ff60bcc23dd3f97848517b571e5f74ab9"
ARCHIVE_DIR="$(mktemp -d /tmp/agent-deck-upstream-capture.XXXXXX)"

RECORDED_COMMIT="$(tr -d '\n' < "$ROOT_DIR/testdata/captures/UPSTREAM_COMMIT")"
if [[ "$RECORDED_COMMIT" != "$UPSTREAM_COMMIT" ]]; then
  echo "capture commit marker does not match generator: $RECORDED_COMMIT" >&2
  exit 1
fi

cleanup() {
  rm -rf "$ARCHIVE_DIR"
}
trap cleanup EXIT INT TERM

git -C "$ROOT_DIR" cat-file -e "$UPSTREAM_COMMIT^{commit}"
git -C "$ROOT_DIR" archive "$UPSTREAM_COMMIT" | tar -x -C "$ARCHIVE_DIR"
cp "$ROOT_DIR/testdata/upstream_capture_test.go" "$ARCHIVE_DIR/internal/ui/agent_harbor_upstream_capture_test.go"

(
  cd "$ARCHIVE_DIR"
  CAPTURE_OUTPUT_DIR="$ROOT_DIR/testdata/captures" \
    go test ./internal/ui -run '^TestGenerateAgentHarborUpstreamCaptures$' -count=1
)

echo "generated unmodified Agent Deck captures from $UPSTREAM_COMMIT"
