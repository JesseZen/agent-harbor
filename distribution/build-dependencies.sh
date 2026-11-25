#!/bin/sh
# Run on a fresh native GitHub runner with Rust, Zig 0.16, C toolchain and Go.
set -eu
target=$1
root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
work=${RUNNER_TEMP:?}/harbor-dependencies
if [ ! -f "$work/assets/.complete" ]; then
mkdir -p "$work/assets/herdr" "$work/assets/magpie" "$work/assets/codex-auth"
git clone https://github.com/herdrdev/herdr.git "$work/herdr"
git -C "$work/herdr" checkout ee5bfb5041bb3e4a49d591c72308aae3c81ecdde
git -C "$work/herdr" apply "$root/docs/herdr-resume-command.patch" "$root/docs/herdr-remote-executable.patch"
(cd "$work/herdr" && cargo build --release --locked)
herdr=$work/assets/herdr/herdr-candidate-$target
cp "$work/herdr/target/release/herdr" "$herdr"
printf '%s\n' 60a5316204b0c538229defb2628e50660a469b85 > "$herdr.source"
shasum -a 256 "$root/docs/herdr-remote-executable.patch" | cut -d ' ' -f 1 > "$herdr.remote-patch.sha256"
shasum -a 256 "$herdr" | cut -d ' ' -f 1 > "$herdr.sha256"
git clone https://github.com/yetone/magpie.git "$work/magpie"
git -C "$work/magpie" checkout 58cf2ad413d0175810629dd20127361e5277582c
git -C "$work/magpie" apply "$root/docs/magpie-caller-only.patch"
git -C "$work/magpie" apply "$root/docs/magpie-profile-isolation.patch"
git -C "$work/magpie" apply "$root/docs/magpie-responses-lite.patch"
magpie=$work/assets/magpie/magpie-caller-candidate-$target
(cd "$work/magpie" && CGO_ENABLED=0 go build -tags nogui -trimpath -ldflags '-s -w -X main.version=0.1.4786-harbor-dev' -o "$magpie" .)
printf '%s\n' 58cf2ad413d0175810629dd20127361e5277582c > "$magpie.source"
shasum -a 256 "$root/docs/magpie-profile-isolation.patch" | cut -d ' ' -f 1 > "$magpie.profiles-patch.sha256"
shasum -a 256 "$root/docs/magpie-responses-lite.patch" | cut -d ' ' -f 1 > "$magpie.responses-lite-patch.sha256"
shasum -a 256 "$magpie" | cut -d ' ' -f 1 > "$magpie.sha256"
auth=$(awk -v target="$target" '$1 == target {print $2}' "$root/bridge/codex-auth-assets.txt")
curl -fL --retry 3 "https://github.com/Loongphy/codex-auth/releases/download/v0.4.0/$auth" -o "$work/assets/codex-auth/$auth"
touch "$work/assets/.complete"
fi
AGENT_HARBOR_DEVELOPMENT_CANDIDATE=1 AGENT_HARBOR_DEPENDENCIES_DIR="$work/assets" AGENT_HARBOR_CANDIDATE_TARGETS="$target" sh "$root/bridge/package.sh" "$root/dist"
