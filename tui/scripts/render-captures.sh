#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FONT_FILE="${AGENT_HARBOR_CAPTURE_FONT:-/System/Library/Fonts/Menlo.ttc}"

if ! command -v magick >/dev/null 2>&1; then
  echo "ImageMagick's magick command is required" >&2
  exit 2
fi
if [[ ! -f "$FONT_FILE" ]]; then
  echo "set AGENT_HARBOR_CAPTURE_FONT to a monospace font file" >&2
  exit 2
fi

for source in "$ROOT_DIR"/testdata/captures/*/side-by-side.ansi "$ROOT_DIR"/testdata/captures/*/dialog-side-by-side.ansi; do
  destination="${source%.ansi}.png"
  magick -background '#1a1b26' -fill '#c0caf5' -font "$FONT_FILE" \
    -pointsize 12 -interline-spacing 1 "label:@$source" "$destination"
  echo "rendered $destination"
done
