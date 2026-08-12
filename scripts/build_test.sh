#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
TEST_ROOT=$(mktemp -d /tmp/agent-harbor-build-test.XXXXXX)
TRASH=/tmp/agent-harbor-trash

cleanup() {
  mkdir -p "$TRASH"
  test ! -d "$TEST_ROOT" || mv "$TEST_ROOT" "$TRASH/build-test.$$"
}
trap cleanup EXIT HUP INT TERM

fail() {
  printf 'build_test: %s\n' "$1" >&2
  exit 1
}

TARGET="$(go env GOOS)-$(go env GOARCH)"
case "$TARGET" in
  darwin-arm64|darwin-amd64|linux-arm64|linux-amd64) ;;
  *) fail "test host is unsupported: $TARGET" ;;
esac

FIXTURE="$TEST_ROOT/core/$TARGET/agent-harbor-core"
mkdir -p "$(dirname "$FIXTURE")"
printf '#!/bin/sh\nexit 0\n' >"$FIXTURE"
chmod 755 "$FIXTURE"

AGENT_HARBOR_CORE_DIR="$TEST_ROOT/core" \
AGENT_HARBOR_OUTPUT_DIR="$TEST_ROOT/output" \
  "$SCRIPT_DIR/build.sh"

test -x "$TEST_ROOT/output/agent-harbor" ||
  fail "application entrypoint is missing"
test -x "$TEST_ROOT/output/agent-harbor-tui" ||
  fail "TUI output is missing"
test -x "$TEST_ROOT/output/agent-harbor-core" ||
  fail "Core output is missing"
cmp -s "$FIXTURE" "$TEST_ROOT/output/agent-harbor-core" ||
  fail "Core output differs from selected artifact"

if AGENT_HARBOR_TARGET=windows-amd64 \
  AGENT_HARBOR_CORE_DIR="$TEST_ROOT/core" \
  AGENT_HARBOR_OUTPUT_DIR="$TEST_ROOT/unsupported" \
  "$SCRIPT_DIR/build.sh" >/dev/null 2>&1; then
  fail "unsupported target succeeded"
fi

printf 'build_test: PASS\n'
