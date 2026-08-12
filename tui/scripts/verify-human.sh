#!/usr/bin/env bash
set -euo pipefail

if [[ ! -t 0 || ! -t 1 ]]; then
  echo "human verification requires an interactive terminal" >&2
  exit 2
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERIFY_DIR="$(mktemp -d /tmp/agent-harbor-tui-human.XXXXXX)"
SOCKET_PATH="$VERIFY_DIR/admin.sock"
INSTANCE_ID="ins_0123456789abcdef0123456789abcdef"
FAKE_PID=""

cleanup() {
  if [[ -n "$FAKE_PID" ]]; then
    kill "$FAKE_PID" 2>/dev/null || true
    wait "$FAKE_PID" 2>/dev/null || true
  fi
  rm -rf "$VERIFY_DIR"
}
trap cleanup EXIT INT TERM

echo "Building the public TUI without proprietary Core source..."
(cd "$ROOT_DIR" && go build -trimpath -o "$VERIFY_DIR/agent-harbor-tui" ./cmd/agent-harbor-tui)
(cd "$ROOT_DIR" && go build -trimpath -o "$VERIFY_DIR/agent-harbor-core" ./cmd/agent-harbor-fake-core)

"$VERIFY_DIR/agent-harbor-core" --socket "$SOCKET_PATH" --instance-id "$INSTANCE_ID" >"$VERIFY_DIR/fake-core.log" 2>&1 &
FAKE_PID=$!
for _ in $(seq 1 100); do
  [[ -S "$SOCKET_PATH" ]] && break
  sleep 0.05
done
if [[ ! -S "$SOCKET_PATH" ]]; then
  echo "fake Core did not create its Unix socket" >&2
  exit 1
fi

cat <<'CHECKLIST'

Human acceptance checklist

1. Sessions is recognizably Agent Deck: original header, group/session tree,
   preview, contextual footer, hotkeys, and New Session dialog (`n`).
2. Ctrl+Left/Ctrl+Right changes the one-row global tab host without losing the
   Sessions cursor/dialog state.
3. Routes, Targets, Quotas, and Observations use selectable resource tables.
   Try j/k, space, /, [, ], and s. Press ? for the command overlay.
4. Resize the terminal around 160x45, 120x30, 90x30, and 70x30. At narrow
   width, Agent Deck stacks Sessions and Preview instead of replacing them.
5. Compare against testdata/captures/*/side-by-side.ansi after exiting.

Press q to exit the TUI when the review is complete.
CHECKLIST
read -r -p "Press Enter to launch the isolated verification TUI..." _

"$VERIFY_DIR/agent-harbor-tui" \
  --admin-socket "$SOCKET_PATH" \
  --instance-id "$INSTANCE_ID" \
  --core-version "0.1.0" \
  --core-binary "$VERIFY_DIR/agent-harbor-core"

echo
read -r -p "Type ACCEPT only if every checklist item passed: " verdict
if [[ "$verdict" != "ACCEPT" ]]; then
  echo "human acceptance not granted" >&2
  exit 1
fi
echo "human acceptance granted for this isolated run"
