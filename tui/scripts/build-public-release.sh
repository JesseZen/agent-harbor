#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT_DIR="${1:-}"
SUPPLIED_CORE="${2:-}"

if [[ -z "$OUTPUT_DIR" ]]; then
  echo "usage: $0 ABSOLUTE_OUTPUT_DIRECTORY [SEPARATELY_SUPPLIED_CORE_BINARY]" >&2
  exit 2
fi
if [[ "$OUTPUT_DIR" != /* ]]; then
  echo "output directory must be absolute" >&2
  exit 2
fi
if [[ -e "$OUTPUT_DIR" ]]; then
  echo "refusing to overwrite existing output: $OUTPUT_DIR" >&2
  exit 2
fi
if [[ -n "$SUPPLIED_CORE" && ! -x "$SUPPLIED_CORE" ]]; then
  echo "supplied Core binary is not executable: $SUPPLIED_CORE" >&2
  exit 2
fi

if (cd "$ROOT_DIR" && go list -deps ./cmd/agent-harbor-tui) | rg 'agent-harbor/(internal|pkg)'; then
  echo "public TUI dependency graph includes proprietary Core source" >&2
  exit 1
fi

mkdir -p "$OUTPUT_DIR/bin" "$OUTPUT_DIR/share/agent-harbor-tui/api/v3" "$OUTPUT_DIR/share/agent-harbor-tui/third_party"
(cd "$ROOT_DIR" && go build -trimpath -o "$OUTPUT_DIR/bin/agent-harbor-tui" ./cmd/agent-harbor-tui)
cp "$ROOT_DIR/LICENSE" "$ROOT_DIR/THIRD_PARTY_NOTICES.md" "$ROOT_DIR/PACKAGING.md" "$OUTPUT_DIR/share/agent-harbor-tui/"
cp "$ROOT_DIR/api/v3/admin.openapi.yaml" "$ROOT_DIR/api/v3/admin.openapi.sha256" "$OUTPUT_DIR/share/agent-harbor-tui/api/v3/"
cp -R "$ROOT_DIR/third_party/lazygit" "$ROOT_DIR/third_party/k9s" "$OUTPUT_DIR/share/agent-harbor-tui/third_party/"

if [[ -n "$SUPPLIED_CORE" ]]; then
  cp "$SUPPLIED_CORE" "$OUTPUT_DIR/bin/agent-harbor-core"
else
  touch "$OUTPUT_DIR/bin/PLACE_SEPARATELY_SUPPLIED_AGENT_HARBOR_CORE_HERE"
fi

MANIFEST_TMP="$OUTPUT_DIR/.SHA256SUMS.tmp"
(
  cd "$OUTPUT_DIR"
  find . -type f ! -name SHA256SUMS ! -name .SHA256SUMS.tmp -print0 | sort -z | xargs -0 shasum -a 256
) > "$MANIFEST_TMP"
mv "$MANIFEST_TMP" "$OUTPUT_DIR/SHA256SUMS"

echo "public release created at $OUTPUT_DIR"
