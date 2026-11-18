#!/bin/sh
set -eu
target=$1
case "$target" in
  linux-amd64|linux-arm64|darwin-amd64|darwin-arm64) ;;
  *) exit 1 ;;
esac
root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
work=$(mktemp -d "${RUNNER_TEMP:?}/harbor-prebuilt.XXXXXX")
digest() { shasum -a 256 "$@" | cut -d ' ' -f 1; }
download() { curl -fsSL --retry 3 --connect-timeout 15 --max-time 180 "$1" -o "$2"; }

for dependency in herdr magpie; do
  entry=$(awk -v dep="$dependency" -v target="$target" '$1 == dep && $2 == target { print; count++ } END { if (count != 1) exit 1 }' "$root/distribution/dependencies.lock") || {
    printf 'Expected one lock entry for %s %s\n' "$dependency" "$target" >&2
    exit 1
  }
  IFS=' ' read -r name platform tag commit checksum patches extra <<EOF
$entry
EOF
  case "$tag" in *[!a-zA-Z0-9._-]*) exit 1 ;; harbor-v?*) ;; *) exit 1 ;; esac
  case "$commit$checksum" in *[!0-9a-f]*) exit 1 ;; esac
  test "${#commit}" = 40 && test "${#checksum}" = 64 && test -z "$extra" || exit 1
  mkdir -p "$work/$dependency"
  case "$dependency" in
    herdr)
      candidate="$work/herdr/herdr-candidate-$target"
      source=60a5316204b0c538229defb2628e50660a469b85
      first_patch="$root/docs/herdr-resume-command.patch"
      second_patch="$root/docs/herdr-remote-executable.patch"
      attestation=remote-patch ;;
    magpie)
      candidate="$work/magpie/magpie-caller-candidate-$target"
      source=5426c43cca9b90be809d51edebc5c926929e9f6c
      first_patch="$root/docs/magpie-caller-only.patch"
      second_patch="$root/docs/magpie-profile-isolation.patch"
      attestation=profiles-patch ;;
  esac
  patch_pair="$(digest "$first_patch"):$(digest "$second_patch")"
  test "$patches" = "$patch_pair" || { printf 'Patch lock mismatch: %s\n' "$dependency" >&2; exit 1; }
  base="https://github.com/JesseZen/$dependency/releases/download/$tag/$dependency-$target"
  download "$base.commit" "$candidate.commit"
  test "$(cat "$candidate.commit")" = "$commit" || { printf 'Fork commit mismatch: %s\n' "$dependency" >&2; exit 1; }
  download "$base" "$candidate"
  test "$(digest "$candidate")" = "$checksum" || { printf 'Checksum mismatch: %s %s\n' "$dependency" "$target" >&2; exit 1; }
  printf '%s\n' "$source" > "$candidate.source"
  printf '%s\n' "$checksum" > "$candidate.sha256"
  printf '%s\n' "${patch_pair#*:}" > "$candidate.$attestation.sha256"
done

mkdir -p "$work/codex-auth"
auth=$(awk -v target="$target" '$1 == target {print $2}' "$root/bridge/codex-auth-assets.txt")
download "https://github.com/Loongphy/codex-auth/releases/download/v0.4.0/$auth" "$work/codex-auth/$auth"
AGENT_HARBOR_DEVELOPMENT_CANDIDATE=1 AGENT_HARBOR_DEPENDENCIES_DIR="$work" AGENT_HARBOR_CANDIDATE_TARGETS="$target" sh "$root/bridge/package.sh" "$root/dist"
