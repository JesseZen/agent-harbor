# Magpie caller-only candidate

The joined Codex connection uses a local Magpie patch in
[`magpie-caller-only.patch`](magpie-caller-only.patch). It is a development
candidate: it has not been submitted to, accepted by, or released by Magpie.
The source-build candidate uses Magpie 0.1.4786. ZCode's provider is a
community plugin on this version and on the previous 0.1.3528 package base.

With `MAGPIE_CODEX_CALLER_ONLY=1`, a Codex request for an official model is
relayed using the initiating client's authorization and account ID. Magpie
does not load Codex login files, add that login to its account pool, switch
accounts, or retry an official request through its account routing. Official
responses and errors pass through an ordinary HTTP transport. Magpie may adapt
third-party compaction or reasoning items when a conversation returns to an
official model, or reject an incompatible checkpoint; the identity boundary
is not a promise of byte-for-byte request transparency. An
explicit operating-system or environment proxy may still affect network
traffic. After handing an official request to the upstream transport, the
gateway no longer applies its pre-route 30-second watchdog to the upstream
response wait. It waits at most 10 minutes for response headers; that limit
does not cap an SSE stream after headers arrive. Requests still held before
upstream handoff retain the pre-route guard.
Magpie models and groups continue to use their configured providers.

For joined Codex sessions the gateway declines the Responses WebSocket upgrade
and Codex uses its built-in HTTP fallback. This avoids reconstructing official
turns in Magpie's WebSocket adapter. It also means the joined connection uses
HTTP where Codex would otherwise use a WebSocket. Existing Magpie behavior is
unchanged when the flag is absent.

Groups with third-party members work. A Magpie stored Codex-account member
cannot currently act as the caller's official account, and is refused in this
mode. Harbor changes its Profile login and refreshes the official App Server for
a signed-in Codex window; the gateway, route and provider credentials stay the
same. Independent API sessions do not participate. Cache entries and
model listings do not prove the current account is eligible for every model.
The policy has synthetic identity and real-client evidence; it has not yet
completed the full multi-account and platform acceptance described in the
[coordination spec](magpie-codex-coordination-spec.md).

New Harbor-managed instances enable the mode. Existing managed instances and
external gateways retain their configuration; joined sessions require a gateway
whose `/api/hello` reports `codex_caller_only: true`. Legacy sessions retain
their recorded connection rather than being migrated automatically.

To build a package dependency from a clean Magpie checkout:

```sh
git checkout 58cf2ad413d0175810629dd20127361e5277582c
git apply /path/to/agent-harbor/docs/magpie-caller-only.patch
git apply /path/to/agent-harbor/docs/magpie-profile-isolation.patch
git apply /path/to/agent-harbor/docs/magpie-responses-lite.patch
go test -tags nogui ./internal/appdir ./internal/provider ./internal/gateway \
  -run '^(TestCallerOnly|TestCodexCallerOnly|TestCodexCallerTransport|TestCodexAsSent|TestHarborResponsesLite)' -count=1
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags nogui -trimpath \
  -ldflags '-s -w -X main.version=0.1.4786-harbor-dev' \
  -o /path/to/dependencies/magpie/magpie-caller-candidate-linux-amd64 .
```

The package script also expects the executable's SHA-256 in a sibling
`.sha256` file and the exact base commit above in a sibling `.source` file.
These files are local build attestations, not upstream signatures. Set
`HARBOR_TEST_CODEX_BIN` to a native Codex binary when running the tests to
also exercise the client's HTTP fallback against a synthetic API key and
synthetic official endpoint. No real login is needed for that test.

## Profile discovery/write isolation candidate

Isolated Profiles additionally require [magpie-profile-isolation.patch](magpie-profile-isolation.patch),
applied after the caller-only patch. It disables enumeration of Codex
homes/history and direct login writes/deletion while caller-only mode is
active, and reports `codex_profile_isolation: true` from `/api/hello`.
An isolated Profile requires both capability fields. Harbor management
verifies its own environment before launching Magpie's TUI.

## Responses Lite candidate

[magpie-responses-lite.patch](magpie-responses-lite.patch) adds
`codex_responses_lite` to `/api/hello` and an opt-in
`?responses_format=lite` variant of both `/v1/codex/models` and
`/backend-api/codex/models`. The variant advertises
`use_responses_lite: true` for the direct `aihub/gpt-6.2-sol` model when
the provider is configured for Responses and `codex=as-sent`. Enable that
transport in the intended Magpie instance with
`magpie provider set aihub codex=as-sent`. Magpie retains the same provider,
model ID and credential for both formats. The default catalogs keep their
regular behavior.

Harbor uses a private copy of the variant catalog for independent API
sessions. Joined sessions use the session's Lite base URL so Codex fetches
its combined model catalog with the initiating login. Magpie passes the
regular model list and request routes through their existing authentication
checks. Harbor verifies the saved route's Lite capability before the session
starts or recovers. If the capability is missing, launch stops with a repair
message.

Package the source build with the executable's `.source` and `.sha256`
and both `.profiles-patch.sha256` and `.responses-lite-patch.sha256`
attestations. The prebuilt path requires new fork assets and matching lock
entries for all four platforms. See [release maintenance](../distribution/README.md).
