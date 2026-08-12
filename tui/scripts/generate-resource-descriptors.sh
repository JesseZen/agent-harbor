#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd -P)
TEMP_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/agent-harbor-descriptors.XXXXXX")

trash_temp_root() {
	[ -d "$TEMP_ROOT" ] || return 0
	TRASH_ROOT=${TMPDIR:-/tmp}/agent-harbor-trash
	mkdir -p "$TRASH_ROOT"
	mv -- "$TEMP_ROOT" "$TRASH_ROOT/descriptors-generate.$$"
}
trap trash_temp_root EXIT HUP INT TERM

cd "$REPO_ROOT"
sha256sum -c api/v3/admin.openapi.sha256

bunx --bun @redocly/cli@2.39.0 bundle api/v3/admin.openapi.yaml \
	--output "$TEMP_ROOT/admin.bundle.yaml"
go run ./internal/resourcepage/gendesc \
	-input "$TEMP_ROOT/admin.bundle.yaml" \
	-output internal/resourcepage/generated/descriptors.go
gofmt -w internal/resourcepage/generated/descriptors.go
