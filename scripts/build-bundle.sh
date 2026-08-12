#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd -P)
OUTPUT=${AGENT_HARBOR_BUNDLE_OUTPUT_DIR:-"$ROOT/dist/bundles"}
CORE_DIR=${AGENT_HARBOR_CORE_DIR:-"$ROOT/core"}
TMUX_DIR=${AGENT_HARBOR_TMUX_DIR:-}
TERMINFO_DIR=${AGENT_HARBOR_TERMINFO_DIR:-}
TMUX_LICENSE_DIR=${AGENT_HARBOR_TMUX_LICENSE_DIR:-}
VERSION=${AGENT_HARBOR_VERSION:-dev}
TARGETS=${AGENT_HARBOR_BUNDLE_TARGETS:-"linux-amd64 linux-arm64 darwin-amd64 darwin-arm64"}

verify_target_binary() {
  binary=$1 target=$2 label=$3
  description=$(file "$binary")
  case "$target:$description" in
    linux-amd64:*ELF*64-bit*x86-64*) ;;
    linux-arm64:*ELF*64-bit*ARM*aarch64*) ;;
    darwin-amd64:*Mach-O*64-bit*x86_64*) ;;
    darwin-arm64:*Mach-O*64-bit*arm64*) ;;
    *) echo "$label payload does not match $target: $description" >&2; exit 1;;
  esac
}

case "$OUTPUT" in /*) ;; *) echo "bundle output directory must be absolute" >&2; exit 1;; esac
if test -z "$TMUX_DIR"; then echo "set AGENT_HARBOR_TMUX_DIR to a matrix containing linux/darwin tmux 3.6b binaries" >&2; exit 1; fi
if test -z "$TERMINFO_DIR"; then echo "set AGENT_HARBOR_TERMINFO_DIR to a matrix containing target terminfo directories" >&2; exit 1; fi
if test -z "$TMUX_LICENSE_DIR"; then echo "set AGENT_HARBOR_TMUX_LICENSE_DIR to tmux and static dependency license files" >&2; exit 1; fi
mkdir -p "$OUTPUT"

for target in $TARGETS; do
  case "$target" in linux-amd64|linux-arm64|darwin-amd64|darwin-arm64) ;; *) echo "unsupported bundle target: $target" >&2; exit 1;; esac
  os=${target%-*}; arch=${target#*-}
  core="$CORE_DIR/$target/agent-harbor-core"
  tmux="$TMUX_DIR/$target/tmux"
  terminfo="$TERMINFO_DIR/$target"
  test -f "$core" || { echo "missing Core payload: $core" >&2; exit 1; }
  test -x "$tmux" || { echo "missing executable tmux payload: $tmux" >&2; exit 1; }
  test -d "$terminfo" || { echo "missing terminfo payload: $terminfo" >&2; exit 1; }
  test "$(cat "$TMUX_DIR/$target/VERSION" 2>/dev/null || true)" = "tmux 3.6b" || { echo "tmux payload $target is not declared as tmux 3.6b" >&2; exit 1; }
	if test "${AGENT_HARBOR_ALLOW_FIXTURE_PAYLOADS:-0}" != 1; then
	  verify_target_binary "$core" "$target" Core
	  verify_target_binary "$tmux" "$target" tmux
	fi
  if test "$os" = linux && test "${AGENT_HARBOR_ALLOW_FIXTURE_PAYLOADS:-0}" != 1; then file "$tmux" | grep -q 'statically linked' || { echo "Linux tmux must be fully static: $tmux" >&2; exit 1; }; fi
  stage=$(mktemp -d "${TMPDIR:-/tmp}/agent-harbor-bundle.XXXXXX")
  cleanup() { rm -rf "$stage"; }
  trap cleanup EXIT HUP INT TERM
  cp -R "$ROOT/launcher/." "$stage/launcher"
  mkdir -p "$stage/launcher/payload/files/bin" "$stage/launcher/payload/files/share" "$stage/launcher/payload/licenses"
  cp "$core" "$stage/launcher/payload/files/bin/agent-harbor-core"
  GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -C "$ROOT/tui" -trimpath -buildvcs=false -o "$stage/launcher/payload/files/bin/agent-harbor-tui" ./cmd/agent-harbor-tui
  cp "$tmux" "$stage/launcher/payload/files/bin/tmux"
  (cd "$terminfo" && tar -czf "$stage/launcher/payload/files/share/terminfo.tar.gz" .)
  cp "$ROOT/launcher/LICENSE" "$stage/launcher/payload/licenses/launcher-MIT"
  cp "$ROOT/tui/LICENSE" "$stage/launcher/payload/licenses/tui-MIT"
  cp "$ROOT/core/LICENSE" "$stage/launcher/payload/licenses/core-NOTICE"
  : > "$stage/launcher/payload/licenses/tmux-licenses"
  find "$TMUX_LICENSE_DIR" -type f | sort | while IFS= read -r license_file; do
    printf '\n===== %s =====\n' "$(basename "$license_file")" >> "$stage/launcher/payload/licenses/tmux-licenses"
    cat "$license_file" >> "$stage/launcher/payload/licenses/tmux-licenses"
  done
  test -s "$stage/launcher/payload/licenses/tmux-licenses" || { echo "tmux license directory is empty" >&2; exit 1; }
  test "$(wc -c < "$stage/launcher/payload/licenses/tmux-licenses" | tr -d ' ')" -le 16777216 || { echo "tmux license payload exceeds 16 MiB" >&2; exit 1; }
  for file in bin/agent-harbor-core bin/agent-harbor-tui bin/tmux; do
    hash=$(shasum -a 256 "$stage/launcher/payload/files/$file" | awk '{print $1}')
    size=$(wc -c < "$stage/launcher/payload/files/$file" | tr -d ' ')
    case "$file" in bin/agent-harbor-core) role=runtime.core; license=core-NOTICE;; bin/agent-harbor-tui) role=frontend.tui; license=tui-MIT;; *) role=dependency.tmux; license=tmux-licenses;; esac
    printf '%s\n' "$role|$file|$hash|$size|448|$license" >> "$stage/components.txt"
  done
  hash=$(shasum -a 256 "$stage/launcher/payload/files/share/terminfo.tar.gz" | awk '{print $1}')
  size=$(wc -c < "$stage/launcher/payload/files/share/terminfo.tar.gz" | tr -d ' ')
  printf '%s\n' "data.terminfo|share/terminfo.tar.gz|$hash|$size|384|tmux-licenses" >> "$stage/components.txt"
  {
    printf '{"schema_version":1,"bundle_id":"%s-%s","version":"%s","target_os":"%s","target_arch":"%s","unsigned":%s,"components":[' "$target" "$VERSION" "$VERSION" "$os" "$arch" "$([ "$os" = darwin ] && echo true || echo false)"
    first=true
    while IFS='|' read -r role file hash size mode license; do
      $first || printf ','; first=false
      executable=false; case "$role" in runtime.core|frontend.tui|dependency.tmux) executable=true;; esac
      printf '{"path":"%s","role":"%s","sha256":"%s","size":%s,"mode":%s,"license":"%s","executable":%s}' "$file" "$role" "$hash" "$size" "$mode" "$license" "$executable"
    done < "$stage/components.txt"
    printf ']}\n'
  } > "$stage/launcher/payload/manifest.json"
  cp "$stage/launcher/payload/manifest.json" "$OUTPUT/agent-harbor-$target.manifest.json"
  (cd "$stage/launcher" && GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -trimpath -buildvcs=false -o "$OUTPUT/agent-harbor-$target" .)
  chmod 755 "$OUTPUT/agent-harbor-$target"
  (cd "$OUTPUT" && shasum -a 256 "agent-harbor-$target" > "agent-harbor-$target.sha256")
  rm -rf "$stage"; trap - EXIT HUP INT TERM
  echo "bundle: wrote $OUTPUT/agent-harbor-$target"
done

(
  cd "$OUTPUT"
  : > SHA256SUMS
  for target in $TARGETS; do
    shasum -a 256 "agent-harbor-$target" >> SHA256SUMS
  done
)
