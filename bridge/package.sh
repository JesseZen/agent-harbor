#!/bin/sh
set -eu

if [ "${AGENT_HARBOR_DEVELOPMENT_CANDIDATE:-}" != 1 ]; then
  printf '%s\n' 'This experimental contraction cannot produce a supported package. Use the preserved pre-contraction source for supported packaging; explicitly select development-candidate packaging for this branch.' >&2
  exit 1
fi
bridge_root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
version=${AGENT_HARBOR_BRIDGE_VERSION:-dev}-development-candidate
case "$version" in
  ''|*[!a-zA-Z0-9._-]*) printf '%s\n' 'Invalid bridge version' >&2; exit 1 ;;
esac
output=${1:-"$bridge_root/dist"}
deps=${AGENT_HARBOR_DEPENDENCIES_DIR:?Set AGENT_HARBOR_DEPENDENCIES_DIR to the directory containing the pinned Herdr and Magpie release assets}
mkdir -p "$output"
output=$(CDPATH= cd -- "$output" && pwd)

# Every candidate target requires its independently built patched Herdr and digest.
# Stock release binaries lack the query and cannot be substituted.
for target in ${AGENT_HARBOR_CANDIDATE_TARGETS:-linux-amd64}; do
  case "$target" in
    linux-amd64) herdr_asset=herdr-linux-x86_64; herdr_sha=14d9e8bc8b4a9e0118d77c32266f76d461f09b40342eb23752960bd89f59929d; magpie_sha=e7a5d4603e92ecdb8493b44f1ab101c8fb45bc5fc1d09f53bc2e7a5f30205996 ;;
    linux-arm64) herdr_asset=herdr-linux-aarch64; herdr_sha=17d7536f2da50859523680ef279dffcac8e4a235f4e46fe8da6c2ec44141897c; magpie_sha=f50da85247280df2647b60a718ae4465f3a18c8e6276e883aecc28a43b06d7cb ;;
    darwin-amd64) herdr_asset=herdr-macos-x86_64; herdr_sha=5fb0a908ec7f6745e3e245738b06bfad56c46391c391ab3c0955fbead01f1369; magpie_sha=79c28eff3e146f610c178708f3fe915c5a524914e725b3c24af73ddde166e689 ;;
    darwin-arm64) herdr_asset=herdr-macos-aarch64; herdr_sha=fb77c2232a05a8035421382c894392f4174e4e38a18e48af3dd0290157c3a7e2; magpie_sha=5afb5e14f77f0a9832df1b3987e9dc56e5049cc886632d35d6c099a7fb39a0b2 ;;
    *) printf 'Unsupported candidate target: %s\n' "$target" >&2; exit 1 ;;
  esac
  herdr_asset="herdr-candidate-$target"
  herdr_sha=$(cat "$deps/herdr/$herdr_asset.sha256")
  test "$(cat "$deps/herdr/$herdr_asset.source")" = "60a5316204b0c538229defb2628e50660a469b85" || { printf '%s\n' 'Wrong Herdr candidate source' >&2; exit 1; }
  case "$herdr_sha" in ''|*[!a-f0-9]*) printf '%s\n' 'Invalid candidate digest' >&2; exit 1 ;; esac
  test "${#herdr_sha}" = 64 || exit 1
  magpie_asset="magpie-cli-$target"
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
  os=${target%-*}
  arch=${target#*-}
  stage="$output/$target"
  mkdir -p "$stage/licenses"
  (cd "$bridge_root" && GOWORK=off CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -buildvcs=false -ldflags "-X main.version=$version" -o "$stage/agent-harbor" .)
  install -m 755 "$deps/herdr/$herdr_asset" "$stage/herdr"
  install -m 755 "$deps/magpie/$magpie_asset" "$stage/magpie"
  install -m 755 "$bridge_root/install.sh" "$stage/install.sh"
  cp "$bridge_root/PACKAGE-README.md" "$stage/README.md"
  printf '%s\n' 'EXPERIMENTAL DEVELOPMENT CANDIDATE: Herdr query is not accepted or released. Do not replace a supported installation.' 'Herdr source: 60a5316204b0c538229defb2628e50660a469b85; base ee5bfb5041bb3e4a49d591c72308aae3c81ecdde' "Herdr SHA256: $herdr_sha" >> "$stage/README.md"
  cp "$bridge_root"/licenses/* "$stage/licenses/"
  COPYFILE_DISABLE=1 tar -C "$stage" -czf "$output/agent-harbor-$version-$target.tar.gz.partial" agent-harbor herdr magpie README.md install.sh licenses
  mv "$output/agent-harbor-$version-$target.tar.gz.partial" "$output/agent-harbor-$version-$target.tar.gz"
  printf '%s\n' "$output/agent-harbor-$version-$target.tar.gz"
done
