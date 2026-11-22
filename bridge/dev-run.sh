#!/bin/sh
set -eu

bridge_root=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
output=${AGENT_HARBOR_DEV_OUTPUT_DIR:-${TMPDIR:-/tmp}/agent-harbor-dev}
companion_dir=${AGENT_HARBOR_DEV_COMPANION_DIR:-}

if [ -z "$companion_dir" ] && command -v brew >/dev/null 2>&1; then
  package_prefix=$(brew --prefix agent-harbor 2>/dev/null || true)
  if [ -n "$package_prefix" ]; then
    companion_dir="$package_prefix/libexec"
  fi
fi

[ -n "$companion_dir" ] || {
  printf '%s\n' 'Set AGENT_HARBOR_DEV_COMPANION_DIR to a directory containing herdr, magpie and codex-auth.' >&2
  exit 1
}

for name in agent-harbor herdr magpie codex-auth; do
  [ -x "$companion_dir/$name" ] || {
    printf 'Missing executable: %s/%s\n' "$companion_dir" "$name" >&2
    exit 1
  }
done

mkdir -p "$output"
(cd "$bridge_root" && GOWORK=off CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "-X main.version=dev-local" -o "$output/agent-harbor" .)
for name in herdr magpie codex-auth; do
  cp "$companion_dir/$name" "$output/$name"
done

export AGENT_HARBOR_DEV_IDENTITY_EXECUTABLE="$companion_dir/agent-harbor"
exec "$output/agent-harbor" "$@"
