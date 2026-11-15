# Magpie caller-only candidate

The joined Codex connection uses a local Magpie patch in
[`magpie-caller-only.patch`](magpie-caller-only.patch). It is a development
candidate: it has not been submitted to, accepted by, or released by Magpie.
The patch applies to Magpie 0.1.3528 and 0.1.4248. The current package uses
0.1.3528 because that release includes the ZCode integration needed by the
existing candidate; later releases moved it to a community plugin.

With `MAGPIE_CODEX_CALLER_ONLY=1`, a Codex request for an official model is
relayed using the initiating client's authorization and account ID. Magpie
does not load Codex login files, add that login to its account pool, switch
accounts, or retry an official request through its account routing. Official
responses and errors pass through an ordinary HTTP transport. Magpie may adapt
third-party compaction or reasoning items when a conversation returns to an
official model, or reject an incompatible checkpoint; the identity boundary
is not a promise of byte-for-byte request transparency. An
explicit operating-system or environment proxy may still affect network
traffic. Magpie models and groups continue to use their configured providers.

For joined Codex sessions the gateway declines the Responses WebSocket upgrade
and Codex uses its built-in HTTP fallback. This avoids reconstructing official
turns in Magpie's WebSocket adapter. It also means the joined connection uses
HTTP where Codex would otherwise use a WebSocket. Existing Magpie behavior is
unchanged when the flag is absent.

Groups with third-party members work. A Magpie stored Codex-account member
cannot currently act as the caller's official account, and is refused in this
mode. Account switching is not implemented by this patch. Cache entries and
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
git checkout 5426c43cca9b90be809d51edebc5c926929e9f6c
git apply /path/to/agent-harbor/docs/magpie-caller-only.patch
go test -tags nogui ./internal/gateway ./internal/provider -run '^TestCodexCallerOnly' -count=1
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags nogui -trimpath \
  -ldflags '-s -w -X main.version=0.1.3528-harbor-dev' \
  -o /path/to/dependencies/magpie/magpie-caller-candidate-linux-amd64 .
```

The package script also expects the executable's SHA-256 in a sibling
`.sha256` file and the exact base commit above in a sibling `.source` file.
These files are local build attestations, not upstream signatures. Set
`HARBOR_TEST_CODEX_BIN` to a native Codex binary when running the tests to
also exercise the client's HTTP fallback against a synthetic API key and
synthetic official endpoint. No real login is needed for that test.
