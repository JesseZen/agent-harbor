#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
OUTPUT=${AGENT_HARBOR_BUNDLE_OUTPUT_DIR:-"$ROOT/dist/bundles"}
TARGETS=${AGENT_HARBOR_BUNDLE_TARGETS:-"linux-amd64 linux-arm64 darwin-amd64 darwin-arm64"}
for target in $TARGETS; do
  case "$target" in linux-amd64|linux-arm64|darwin-amd64|darwin-arm64) ;; *) echo "unsupported bundle target: $target" >&2; exit 1;; esac
  binary="$OUTPUT/agent-harbor-$target"; manifest="$binary.manifest.json"
  test -x "$binary" || { echo "missing bundle: $binary" >&2; exit 1; }
  test -f "$manifest" || { echo "missing manifest: $manifest" >&2; exit 1; }
  (cd "$OUTPUT" && shasum -a 256 -c "agent-harbor-$target.sha256" >/dev/null)
  expected_manifest=$(tr -d '\r\n' < "$manifest")
  embedded_manifest=$(strings "$binary" | grep -o '{"schema_version":1,"bundle_id".*}' | head -n 1 || true)
  test "$embedded_manifest" = "$expected_manifest" || { echo "embedded manifest mismatch: $binary" >&2; exit 1; }
  grep -q '"schema_version":1' "$manifest"
  grep -q '"role":"runtime.core"' "$manifest"
  grep -q '"role":"frontend.tui"' "$manifest"
  grep -q '"role":"dependency.tmux"' "$manifest"
  grep -q '"role":"data.terminfo"' "$manifest"
  case "$target" in
    linux-*) file "$binary" | grep -q 'ELF' || { echo "wrong file header: $binary" >&2; exit 1; } ;;
    darwin-*) file "$binary" | grep -q 'Mach-O' || { echo "wrong file header: $binary" >&2; exit 1; } ;;
  esac
done
(cd "$OUTPUT" && shasum -a 256 -c SHA256SUMS >/dev/null)
echo "bundle verification passed: $OUTPUT"
