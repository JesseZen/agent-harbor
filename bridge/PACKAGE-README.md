# Agent Harbor development candidate

This package is an experimental contraction build. Herdr's recovery query is a local contribution candidate, not accepted or released upstream. Install only into a new empty dedicated prefix; the installer refuses populated prefixes and existing PATH links. Do not replace a supported installation.

The previous supported workflow is not included in this public snapshot. This snapshot builds development candidates, not a normal supported package.

Provide a native executable named for the client on PATH. Harbor does not inspect npm/vendor layouts or execute wrappers to discover versions. Install the supported native client binary explicitly. Identity is observed from Herdr; recovery requires its query plus explicit uncertainty approval. Legacy IDs retain their original provenance and require review/reassociation, never automatic official trust. Harbor does not scan client histories or private writer locks, and does not delete conversation histories.

Build with `AGENT_HARBOR_DEVELOPMENT_CANDIDATE=1`, a candidate version, dependency directory, and explicit `AGENT_HARBOR_CANDIDATE_TARGETS` (default linux-amd64). Each target requires `herdr/herdr-candidate-TARGET`, its `.sha256` containing the binary digest and `.source` containing exactly `60a5316204b0c538229defb2628e50660a469b85`. This local patch is based on `ee5bfb5041bb3e4a49d591c72308aae3c81ecdde`. Magpie remains pinned with release digest checks. Candidate source metadata is an operator-provided attestation, not an upstream signature.

The filename, version and packaged README identify the candidate. No automatic upgrades or process/configuration changes are performed by installation.

Selected candidate flows have been exercised on native Linux amd64 and macOS arm64; Linux arm64 and macOS amd64 have cross-build and package evidence only. These results apply to the revisions and flows that were exercised. Verify changes on the relevant targets, and keep official upstream acceptance and release as separate, open delivery gates.

## Using the candidate

Run `./agent-harbor` to open the workspace. First-run checks offer Local Codex, a Harbor-managed Magpie, or a detected running Magpie with verified configuration identity. Harbor keeps its Herdr configuration and state separate from your existing workspaces.

Install into an empty dedicated directory with `sh install.sh install --prefix /absolute/path/agent-harbor`. Run `/absolute/path/agent-harbor/agent-harbor` or add that directory to PATH. The installer refuses a populated prefix and does not change services or configuration. Uninstall with `sh /absolute/path/agent-harbor/install.sh uninstall --prefix /absolute/path/agent-harbor`; licenses and any files you added remain. Session and connection state are retained.

Press `Ctrl+B a` for Harbor sessions and `Ctrl+B m` for Magpie management in the current pane's context. Both shortcuts appear in `Ctrl+B ?`. Inside Harbor, `n` creates a session, `/` filters, Enter enters or reviews recovery, `i` inspects connection and conversation evidence, `s` stops, `e` renames, and `x` removes a Harbor record while keeping Codex history. `?` shows the current screen's keys. The single-page creation form shows the directory, Codex CLI, connection, model or routing group, and name. Create is initially focused. Tab or arrows move between controls; Enter opens a field, Enter inside accepts only that field, and Esc cancels the local edit. The directory picker and Magpie catalog support inline search. Only Create submits; first-use Magpie acknowledgement remains explicit. Lists support click selection, double-click activation and scoped wheel scrolling.

Local uses your own Codex settings and login. Magpie sessions pin an instance and selected model or routing group. A catalog ID is not evidence that the provider or group definition has stayed unchanged. Details distinguish the selection from Magpie's observed main and auxiliary routes; use Magpie Usage / Requests for audit. Magpie may send auxiliary requests containing parts of the conversation to other models or providers. Harbor remembers acknowledgement per instance and retains a brief reminder.

The Magpie screen binds external instances, chooses the default for new sessions, starts or stops Harbor's managed instance, and opens provider management for the verified configuration. External instances remain user-managed. Closing management leaves the existing gateway process running; if another TUI originally owns that gateway, closing that original TUI stops it. Harbor's managed gateway keeps running until explicitly stopped.

Manage conversation history with the client’s own tools. Harbor no longer deletes client history. `x` removes only the Harbor record and preserves the conversation; running or starting records must be stopped first.

`./agent-harbor stop` stops the managed Herdr workspace and its panes. It does not stop Magpie. Use the Magpie screen or `magpie stop` for Harbor's owned gateway. State lives under `$XDG_STATE_HOME/agent-harbor-bridge`, falling back to `$HOME/.local/state/agent-harbor-bridge`.

Optional PATH entry: append `--link` to the install command to create `~/.local/bin/agent-harbor`. An existing entry is never overwritten. Uninstall removes that link only while it still points at this installation. Add `~/.local/bin` to PATH yourself if needed.

For first use, run `agent-harbor`, review the dependency checks, choose Local only, create a managed Magpie, or bind a verified external gateway. Managed Magpie has its own home, configuration, Codex home and ports; it does not import subscription accounts from your native CLI. Set up providers in `Ctrl+B m`. To stop its background gateway explicitly, run `agent-harbor magpie stop`. Uninstall does not stop services: stop sessions and owned services first.

- Directory missing or moved: restore the original directory and resolved path.
- Gateway offline: start the owned gateway explicitly, or ask the external instance's owner to restore it.
- Instance changed or unknown: inspect its configuration identity before recording an explicit override; an address alone is insufficient.
- Model missing: restore the original model/group in the recorded Magpie instance. Harbor will not choose a substitute.
- Identity missing or ambiguous: inspect the complete Herdr reference and explicitly reassociate legacy records; then approve query recovery.
- Conversation open elsewhere: stop its other writer before resuming.
- Start failed: inspect the reported dependency or launch error, fix it, then retry.

Use `agent-harbor sessions --json`, `agent-harbor show SESSION_ID` and `agent-harbor magpie status --json` for diagnostics. Do not share conversation files or credentials. A routing group's unchanged ID does not prove unchanged membership: review it in Magpie before confirming resume. Optional migration from existing Magpie configuration is not offered because backup/restore has not been verified.

The repository's disposable manual environment can be started with:

```sh
python3 bridge/tests/manual_acceptance.py start \
  --binary /path/to/unpacked/agent-harbor --herdr /path/to/unpacked/herdr \
  --magpie /path/to/unpacked/magpie --codex /path/to/native/codex
```

Use the native Codex executable, not `codex.js`. The fixture copies it, disables update checks, uses simulated providers A/B/C and prints its own open/restart/stop commands. Keep its diagnostics directory when reporting a failure. This developer fixture is run from the repository, not included in the end-user archive.

Harbor settings: press `,` inside Harbor, use `Ctrl+B ,` in the workspace,
or run `agent-harbor settings`. Edit the Harbor, settings and Magpie shortcuts,
then Save; Restore defaults changes the draft until saved. `Ctrl+B s` keeps
Herdr's native settings. Preferences apply only to the owned workspace. Direct
Ctrl shortcuts can intercept CLI input and conflict with terminal or Emacs keys.
Press `i` in settings to read full notices and scope; scroll with arrows.
If interrupted while saving, close clients and explicitly stop the owned
workspace, then run `agent-harbor settings-recover` before reopening.


Herdr MCP clients must explicitly discover and select a target. Use Harbor's recorded pane and tab identifiers to match the managed workspace. MCP relay/read calls target the selected pane; agents do not communicate automatically. Configure MCP against Harbor's isolated Herdr wrapper, rather than another running Herdr instance.

Herdr automatic agent restoration is disabled. Resume explicitly through Harbor with the matching candidate query to preserve the recorded conversation and connection. Missing or ambiguous identities require review/reassociation and explicit uncertainty approval. After a Herdr restart, previous tabs can be plain shells: `c` inspects a recorded previous pane and offers to close it only after checking its tab, title and foreground shell. It refuses other foreground work.
