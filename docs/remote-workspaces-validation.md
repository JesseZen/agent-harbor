# Remote workspace candidate validation

Validation date: 2026-11-16. Runtime/package source: `2e85b34` on
`remote/native-herdr`. Later test and documentation commits do not change the
packaged runtime. This is a local development candidate, not an upstream release.

## Dependencies and builds

- Herdr 0.9.5: upstream base `ee5bfb5041bb3e4a49d591c72308aae3c81ecdde`,
  query candidate `60a5316204b0c538229defb2628e50660a469b85`, plus this branch's
  `herdr-remote-executable.patch`.
- Remote patch SHA-256:
  `fd407c1f9c283c0d02223431228899a42d6c453a313fac7eeae537a9cb8b567e`.
- Magpie 0.1.3528: base `5426c43cca9b90be809d51edebc5c926929e9f6c` plus
  `magpie-caller-only.patch`.
- Native Codex 0.169.0, with updates disabled in exclusive fixture homes.
- Linux amd64 Herdr uses Rust 1.96.1, Zig 0.16.0 and the musl target. It is
  statically linked and runs on the server with glibc 2.36. The initial GNU
  build required a newer glibc and was not used for passing remote acceptance.
- macOS arm64 Herdr was crossbuilt on Linux using the installed macOS 27.0 SDK,
  then executed on the Mac. Magpie and Harbor were also crossbuilt on Linux.

Both packages include the patched dependencies, installer and license notices.
The package script requires the exact source, executable checksum and additional
remote-patch attestation. These are operator attestations, not upstream signatures.

| Archive | SHA-256 |
| --- | --- |
| `agent-harbor-remote-final-development-candidate-darwin-arm64.tar.gz` | `c2d51a7de1965ee581b9070db255330834667c7d5b95ee55ff4974a27289d012` |
| `agent-harbor-remote-final-development-candidate-linux-amd64.tar.gz` | `1ddf917fd3e54a9b4941b36d350ae934ea5e64581ec8837cdfefa57a0ad9ec87` |

## Acceptance boundaries

The two-host fixture uses real OpenSSH, the native local Herdr renderer and a
native Codex executable on the remote server. Only the provider is synthetic.
HOME, XDG paths, client homes, sockets, processes, project files and provider
ports belong to each fixture. No real credentials or conversations are copied.

The fixture selects the saved remote machine, opens its Harbor popup and starts
Codex in the remote project. After a request reaches a deliberately delayed
provider, it terminates the client terminal session. The remote request completes
before the client is reopened. The same PID, process-group ID and Linux process
start ticks must remain live. The official Herdr conversation reference must
match after reconnect, and the returned answer must appear in the native UI.
The local Harbor session list stays empty. Replacing the remote workspace
identity must make the old saved connection fail.

| Final packaged client → server | Result |
| --- | --- |
| Linux amd64 → separate Linux amd64 CNB environment | Passed all fixture assertions |
| Native macOS arm64 → Linux amd64 CNB environment | Passed all fixture assertions |

The final macOS and Linux archives also passed the isolated native installer
gate, including ownership, uninstall preservation and optional PATH-link checks.

Physical Mac sleep, power-off, an entire overnight duration and CNB persistence
after recycling are not simulated by this test. A recycled server terminates
its processes; source backup does not imply conversation history retention.
Unattended real work also needs provider, Git and MCP access independent of the
laptop. The fixture does not validate a user's live account or provider quota.

## Regression checks

The complete Harbor Go suite and `go vet ./...` passed in isolated environments
on Linux. The final Go suite took 79.289 seconds and includes restoration of both
set and unset client-home variables during remote wrapper invocation.

Before the final diagnostic-message adjustment, Herdr's full nextest run passed
4,136 tests with 17 skipped. The final adjustment was checked by two focused
native musl tests, then both target executables were rebuilt. The entire Rust
suite was not repeated after that message-only adjustment.

The earlier `remote-07c7618` Linux package passed install/uninstall and ownership,
Magpie identity/lifecycle, routing-group rotation, native Codex capabilities,
real query recovery, manual fixture lifecycle and shortcut smoke. The broad
UI/MCP gate passed after adding a synthetic API-key login for caller connections.
Its historical initial-model refusal and metadata expectations were updated to
the current caller contract: Codex restores its last model; Harbor's initial
model is not an assertion about the current model. The legacy separate-provider
model-refusal check remains conditional on that connection type.
The UI/MCP run includes external client visibility, socket reconnect, reviewed
recovery of three conversations, negative directory/identity/instance/duplicate
checks, native Codex MCP invocation, exact pane/tab/connection targeting and
request-history routing. It preceded the final remote-wrapper environment and
diagnostic-message fixes; final-package evidence for those fixes is the full Go
suite, focused Herdr tests and both two-host runs above.
