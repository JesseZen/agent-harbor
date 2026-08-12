#!/bin/sh
set -eu

repo=${AGENT_HARBOR_REPOSITORY:-JesseZen/agent-harbor}
tag=${AGENT_HARBOR_RELEASE_TAG:-v3-latest}
install_dir=${AGENT_HARBOR_INSTALL_DIR:-/usr/local/bin}
base_url="https://github.com/$repo/releases/download/$tag"

case "$(uname -s)/$(uname -m)" in
  Linux/x86_64|Linux/amd64) ;;
  *)
    echo "agent-harbor: this installer supports Linux amd64 only" >&2
    exit 1
    ;;
esac

for command in curl sha256sum install mktemp; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "agent-harbor: required command not found: $command" >&2
    exit 1
  fi
done

if [ "$(id -u)" -ne 0 ] && [ ! -w "$install_dir" ]; then
  echo "agent-harbor: run this installer as root or set AGENT_HARBOR_INSTALL_DIR to a writable directory" >&2
  exit 1
fi

work_dir=$(mktemp -d "${TMPDIR:-/tmp}/agent-harbor-install.XXXXXX")
cleanup() {
  rm -f "$work_dir/agent-harbor-linux-amd64" \
    "$work_dir/agent-harbor-linux-amd64.manifest.json" \
    "$work_dir/SHA256SUMS"
  rmdir "$work_dir" 2>/dev/null || true
}
trap cleanup EXIT HUP INT TERM

echo "agent-harbor: downloading $repo $tag"
curl --fail --location --proto '=https' --tlsv1.2 \
  --output "$work_dir/agent-harbor-linux-amd64" \
  "$base_url/agent-harbor-linux-amd64"
curl --fail --location --proto '=https' --tlsv1.2 \
  --output "$work_dir/SHA256SUMS" \
  "$base_url/SHA256SUMS"
curl --fail --location --proto '=https' --tlsv1.2 \
  --output "$work_dir/agent-harbor-linux-amd64.manifest.json" \
  "$base_url/agent-harbor-linux-amd64.manifest.json"

(
  cd "$work_dir"
  sha256sum --check SHA256SUMS
)

mkdir -p "$install_dir"
if command -v agent-harbor >/dev/null 2>&1; then
  agent-harbor stop >/dev/null 2>&1 || true
fi
install -m 0755 "$work_dir/agent-harbor-linux-amd64" "$install_dir/agent-harbor"

echo "agent-harbor: installed $install_dir/agent-harbor"
"$install_dir/agent-harbor" version
echo "agent-harbor: run 'agent-harbor' to initialize and open the TUI"
