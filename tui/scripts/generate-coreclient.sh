#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
REPO_ROOT=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd -P)
TEMP_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/agent-harbor-coreclient.XXXXXX")

trash_temp_root() {
	[ -d "$TEMP_ROOT" ] || return 0
	TRASH_ROOT=${TMPDIR:-/tmp}/agent-harbor-trash
	mkdir -p "$TRASH_ROOT"
	mv -- "$TEMP_ROOT" "$TRASH_ROOT/coreclient-generate.$$"
}
trap trash_temp_root EXIT HUP INT TERM

cd "$REPO_ROOT"
sha256sum -c api/v3/admin.openapi.sha256

bunx --bun @redocly/cli@2.39.0 bundle api/v3/admin.openapi.yaml \
	--output "$TEMP_ROOT/admin.bundle.yaml"
go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.7.2 \
	-config api/v3/deck-admin.go.yaml \
	-o internal/coreclient/generated/admin.gen.go \
	"$TEMP_ROOT/admin.bundle.yaml"
gofmt -w internal/coreclient/generated/admin.gen.go
