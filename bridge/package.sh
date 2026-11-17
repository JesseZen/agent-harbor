#!/bin/sh
set -eu

if [ "${AGENT_HARBOR_DEVELOPMENT_CANDIDATE:-}" != 1 ]; then
  printf '%s\n' 'This experimental contraction cannot produce a supported package. Use the preserved pre-contraction source for supported packaging; explicitly select development-candidate packaging for this branch.' >&2
  exit 1
fi
bridge_root=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
version=${AGENT_HARBOR_BRIDGE_VERSION:-dev-development-candidate}
case "$version" in
  ''|*[!a-zA-Z0-9._-]*) printf '%s\n' 'Invalid bridge version' >&2; exit 1 ;;
esac
output=${1:-"$bridge_root/dist"}
deps=${AGENT_HARBOR_DEPENDENCIES_DIR:?Set AGENT_HARBOR_DEPENDENCIES_DIR to the directory containing the Herdr and Magpie candidate assets}
mkdir -p "$output"
output=$(CDPATH='' cd -- "$output" && pwd)

# Every candidate target requires its independently built patched Herdr and digest.
# Stock release binaries lack the query and cannot be substituted.
for target in ${AGENT_HARBOR_CANDIDATE_TARGETS:-linux-amd64}; do
  case "$target" in
    linux-amd64|linux-arm64|darwin-amd64|darwin-arm64) ;;
    *) printf 'Unsupported candidate target: %s\n' "$target" >&2; exit 1 ;;
  esac
  herdr_asset="herdr-candidate-$target"
  herdr_sha=$(cat "$deps/herdr/$herdr_asset.sha256")
  test "$(cat "$deps/herdr/$herdr_asset.source")" = "60a5316204b0c538229defb2628e50660a469b85" || { printf '%s\n' 'Wrong Herdr candidate source' >&2; exit 1; }
  if command -v sha256sum >/dev/null 2>&1; then
    remote_patch_sha=$(sha256sum "$bridge_root/../docs/herdr-remote-executable.patch" | cut -d ' ' -f 1)
  else
    remote_patch_sha=$(shasum -a 256 "$bridge_root/../docs/herdr-remote-executable.patch" | cut -d ' ' -f 1)
  fi
  test "$(cat "$deps/herdr/$herdr_asset.remote-patch.sha256" 2>/dev/null || true)" = "$remote_patch_sha" || { printf '%s\n' 'Wrong or missing Herdr remote patch attestation' >&2; exit 1; }
  case "$herdr_sha" in ''|*[!a-f0-9]*) printf '%s\n' 'Invalid candidate digest' >&2; exit 1 ;; esac
  test "${#herdr_sha}" = 64 || exit 1
  magpie_asset="magpie-caller-candidate-$target"
  magpie_sha=$(cat "$deps/magpie/$magpie_asset.sha256")
  test "$(cat "$deps/magpie/$magpie_asset.source")" = "5426c43cca9b90be809d51edebc5c926929e9f6c" || { printf '%s\n' 'Wrong Magpie candidate source' >&2; exit 1; }
  if command -v sha256sum >/dev/null 2>&1; then
    profiles_patch_sha=$(sha256sum "$bridge_root/../docs/magpie-profile-isolation.patch" | cut -d ' ' -f 1)
  else
    profiles_patch_sha=$(shasum -a 256 "$bridge_root/../docs/magpie-profile-isolation.patch" | cut -d ' ' -f 1)
  fi
  test "$(cat "$deps/magpie/$magpie_asset.profiles-patch.sha256" 2>/dev/null || true)" = "$profiles_patch_sha" || { printf '%s\n' 'Wrong or missing Magpie Profile isolation patch attestation' >&2; exit 1; }
  case "$magpie_sha" in ''|*[!a-f0-9]*) printf '%s\n' 'Invalid Magpie candidate digest' >&2; exit 1 ;; esac
  test "${#magpie_sha}" = 64 || exit 1
  for entry in "herdr/$herdr_asset:$herdr_sha" "magpie/$magpie_asset:$magpie_sha"; do
    file=${entry%:*}
    expected=${entry#*:}
    if command -v sha256sum >/dev/null 2>&1; then
      actual=$(sha256sum "$deps/$file" | cut -d ' ' -f 1)
    else
      actual=$(shasum -a 256 "$deps/$file" | cut -d ' ' -f 1)
    fi
    test "$actual" = "$expected" || { printf 'Checksum mismatch: %s\n' "$file" >&2; exit 1; }
  done
  # codex-auth is the unmodified pinned upstream release, never a local rebuild.
  auth_entry=$(awk -v target="$target" '$1 == target {print $2 " " $3}' "$bridge_root/codex-auth-assets.txt")
  auth_archive=${auth_entry% *}
  auth_sha=${auth_entry#* }
  auth_path="$deps/codex-auth/$auth_archive"
  if command -v sha256sum >/dev/null 2>&1; then
    auth_actual=$(sha256sum "$auth_path" | cut -d ' ' -f 1)
  else
    auth_actual=$(shasum -a 256 "$auth_path" | cut -d ' ' -f 1)
  fi
  test "$auth_actual" = "$auth_sha" || { printf '%s\n' 'Wrong codex-auth v0.4.0 archive checksum' >&2; exit 1; }
  os=${target%-*}
  arch=${target#*-}
  stage="$output/$target"
  mkdir -p "$stage/licenses"
  (cd "$bridge_root" && GOWORK=off CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -buildvcs=false -ldflags "-X main.version=$version" -o "$stage/agent-harbor" .)
  install -m 755 "$deps/herdr/$herdr_asset" "$stage/herdr"
  install -m 755 "$deps/magpie/$magpie_asset" "$stage/magpie"
  auth_unpack=$(mktemp -d "$output/codex-auth-unpack.XXXXXX")
  tar -xzf "$auth_path" -C "$auth_unpack"
  install -m 755 "$auth_unpack/codex-auth" "$stage/codex-auth"
  # Leave extraction artifacts in the output directory for build inspection.
  printf '%s\n' 'codex-auth v0.4.0; unmodified upstream release; MIT' "Source: https://github.com/Loongphy/codex-auth/releases/tag/v0.4.0" "Archive: $auth_archive; SHA256: $auth_sha" >> "$stage/README.md"
  install -m 755 "$bridge_root/install.sh" "$stage/install.sh"
  install -m 755 "$bridge_root/../distribution/install.sh" "$stage/manage.sh"
  cp "$bridge_root/PACKAGE-README.md" "$stage/README.md"
  printf '%s\n' 'EXPERIMENTAL DEVELOPMENT CANDIDATE: Herdr query and Magpie caller-only patches are not accepted or released. Do not replace a supported installation.' 'Herdr source: 60a5316204b0c538229defb2628e50660a469b85; base ee5bfb5041bb3e4a49d591c72308aae3c81ecdde' "Herdr SHA256: $herdr_sha" 'Magpie base: 5426c43cca9b90be809d51edebc5c926929e9f6c with caller-only patch' "Magpie SHA256: $magpie_sha" >> "$stage/README.md"
  printf '%s\n' "Herdr remote-executable patch SHA256: $remote_patch_sha (local candidate, not upstream)" >> "$stage/README.md"
  printf '%s\n' "Magpie Profile isolation patch SHA256: $profiles_patch_sha (local candidate, not upstream)" >> "$stage/README.md"
  cp "$bridge_root"/licenses/* "$stage/licenses/"
  COPYFILE_DISABLE=1 tar -C "$stage" -czf "$output/agent-harbor-$version-$target.tar.gz.partial" agent-harbor herdr magpie codex-auth README.md install.sh manage.sh licenses
  mv "$output/agent-harbor-$version-$target.tar.gz.partial" "$output/agent-harbor-$version-$target.tar.gz"
  printf '%s\n' "$output/agent-harbor-$version-$target.tar.gz"
done
