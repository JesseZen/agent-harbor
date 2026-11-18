# Agent Harbor development candidate

This package is an experimental contraction build. Herdr's recovery query and Magpie's caller-only mode are local contribution candidates, not accepted or released upstream. Install only into a new empty dedicated prefix; the installer refuses populated prefixes and existing PATH links. Do not replace a supported installation.

The previous supported workflow is not included in this public snapshot. This snapshot publishes development candidates through GitHub Releases and Homebrew; see the [installation guide](../README.md#install-and-update).

Provide a native executable named for the client on PATH. Harbor does not inspect npm/vendor layouts or execute wrappers to discover versions. Install the supported native client binary explicitly. Identity is observed from Herdr; recovery requires its query plus explicit uncertainty approval. Legacy IDs retain their original provenance and require review/reassociation, never automatic official trust. Harbor does not scan client histories or private writer locks, and does not delete conversation histories.

Build with `AGENT_HARBOR_DEVELOPMENT_CANDIDATE=1`, an explicit release tag as the version, dependency directory, and explicit `AGENT_HARBOR_CANDIDATE_TARGETS` (default linux-amd64). Each target requires `herdr/herdr-candidate-TARGET`, its `.sha256` containing the binary digest and `.source` containing exactly `60a5316204b0c538229defb2628e50660a469b85`. This local patch is based on `ee5bfb5041bb3e4a49d591c72308aae3c81ecdde`. It also requires `magpie/magpie-caller-candidate-TARGET` and matching `.sha256` and `.source` files; the Magpie source value is `5426c43cca9b90be809d51edebc5c926929e9f6c`. The repository's `docs/magpie-caller-integration.md` explains how to build it. Candidate source metadata is an operator-provided attestation, not an upstream signature.

The filename, version and packaged README identify the candidate. No automatic upgrades or process/configuration changes are performed by installation.

Selected candidate flows have been exercised on native Linux amd64 and macOS arm64; Linux arm64 and macOS amd64 have cross-build and package evidence only. These results apply to the revisions and flows that were exercised. Verify changes on the relevant targets, and keep official upstream acceptance and release as separate, open delivery gates.

## Using the candidate

### Remote workspaces

This branch also requires the Herdr remote-executable patch on top of the query
candidate. Each Herdr asset needs a `.remote-patch.sha256` file containing the
SHA-256 of `docs/herdr-remote-executable.patch`; package creation rejects a missing
or different attestation. The remote patch is a local candidate, not an upstream
release.

Install the matching candidate on a server and configure its native client.
From your laptop, run `agent-harbor remote add Build --ssh workbox --binary
/srv/harbor/agent-harbor --state /srv/harbor-state --native`, using your actual SSH
alias and absolute server paths. Then open `agent-harbor` and select Build in
Herdr's machine sidebar. The Harbor popup, directories, clients and connections
belong to that server; the local client renders them through native Herdr.

`remote list --json`, `remote status Build --json` and `remote reconnect Build`
manage connection visibility and SSH authentication. `remote remove PROFILE_ID`
forgets a connection without stopping its tasks. Closing the local client leaves
remote work running. Provider access, Git authentication and MCP services must
remain available without the laptop. Temporary environment recycling ends its
processes; reopening the host and reviewing conversation recovery are separate
from reconnecting to a live server. Preserve client histories as well as code.

### Local workspace

Run `./agent-harbor` to open the workspace. First-run checks offer Local Codex, a Harbor-managed Magpie, or a detected running Magpie with verified configuration identity. Harbor keeps its Herdr configuration and state separate from your existing workspaces.

Install into an empty dedicated directory with `sh install.sh install --prefix /absolute/path/agent-harbor`. Run `/absolute/path/agent-harbor/agent-harbor` or add that directory to PATH. The installer refuses a populated prefix and does not change services or configuration. Uninstall with `sh /absolute/path/agent-harbor/install.sh uninstall --prefix /absolute/path/agent-harbor`; licenses and any files you added remain. Session and connection state are retained.

Press `Ctrl+B a` for Harbor sessions and `Ctrl+B m` for Magpie management in the current pane's context. Both shortcuts appear in `Ctrl+B ?`. Inside Harbor, `n` creates a session, `/` filters, Enter enters or reviews recovery, `i` inspects connection and conversation evidence, `s` stops, `e` renames, and `x` removes a Harbor record while keeping Codex history. `?` shows the current screen's keys. The single-page creation form shows the directory, Codex CLI, connection, model or routing group, and name. The Project field is initially focused. Tab or arrows move between fields; Enter opens a field, Enter inside accepts only that field, and Esc cancels the local edit. The directory picker and Magpie catalog support inline search. `Ctrl+S` creates the session; first-use Magpie acknowledgement remains explicit.
If recovery hooks need a decision, Harbor offers Prepare, Skip and Cancel before
creating a record or terminal. Cancel is the initial Enter target and returns to
the draft. Codex still asks for hook trust. Actions appear as fixed footer shortcuts;
Settings uses `Ctrl+S` to save and `Ctrl+R` to restore its draft defaults. Lists support click selection, double-click activation and scoped wheel scrolling.

Local uses your own Codex settings and login. Magpie sessions pin an instance and selected model or routing group. A catalog ID is not evidence that the provider or group definition has stayed unchanged. Details distinguish the selection from Magpie's observed main and auxiliary routes; use Magpie Usage / Requests for audit. Magpie may send auxiliary requests containing parts of the conversation to other models or providers. Harbor remembers acknowledgement per instance and retains a brief reminder.

In a new joined Magpie-backed Codex session, `/model` combines official models with the instance's models and routing groups. Codex owns the current model and reasoning selection; recovery does not reapply Harbor's initial selection. A saved session-only `s` choice was preserved in a real-client recovery test. The initial model shown in Harbor is not a claim about the current selection. Official catalog entries may include cached models that are unavailable to the present account.

New Harbor-managed Magpie instances enable caller-only mode. It forwards official requests with the initiating Codex login, without selecting a Magpie account or switching accounts on errors. Third-party providers use their own configured credentials. A group containing a stored Magpie Codex account cannot currently use that member in caller-only mode. Existing managed or external gateways retain their current settings: a joined session requires a gateway that reports caller-only support. Existing legacy sessions keep their original connection and profile behavior; they are not silently migrated.

### Profiles

Press `p` in the session list or open Profiles to create, rename or select a
Profile. New Sessions capture the displayed Profile. Selecting another one does
not move existing Sessions, change their login or restart them. Codex Profiles
keep separate personal settings, credentials, histories, SQLite and runtime roots;
project files remain shared. Use separate worktrees for separate code changes.

A fresh install gets isolated Default. An upgrade preserves the existing client
environment, labeled as shared with clients outside Harbor. An older Session
without a binding must be explicitly reviewed against its original environment
before another launch. In Profiles, select the existing environment and choose
“Bind reviewed original environment” for the selected stopped Session. Do not
choose an isolated Profile to move an old Conversation.

CLI equivalents:

```
agent-harbor profiles list
agent-harbor profiles new Work
agent-harbor profiles rename PROFILE_ID NAME
agent-harbor profiles select PROFILE_ID
agent-harbor new --profile PROFILE_ID --dir /path/to/project --connection local
agent-harbor profiles existing --home /original/codex --sqlite /original/sqlite NAME
agent-harbor profiles review SESSION_ID PROFILE_ID --confirm
agent-harbor profiles default PROFILE_ID INSTANCE_ID
```

Use `shared` as the default instance ID to retain the workspace's shared default.
This never rewrites an existing Session's instance. Provider settings and request
logs of a shared Magpie instance remain shared across its Profiles. Isolated
sessions require the Profile discovery/write isolation candidate; an old or
unverified gateway is refused. Explicitly update your dedicated candidate rather
than replacing or stopping a user-managed service.

### Codex accounts

The Accounts page (`a`) automatically refreshes Codex usage and reset-card
availability for the selected Profile. `p` chooses a Profile and `r` reruns the
lookup. Enter on a saved account opens Details. A ticket marks available
reset cards. In Details, `u` opens a confirmation and `y` redeems one card
through bundled codex-auth, which chooses the card.

`l` adds a login through official Codex, `s` selects a saved login, and `o`
selects Not signed in while retaining the saved list. Unknown or active
session activity blocks login changes. Stop the named session explicitly
before retrying when its activity cannot be inspected. Existing environments
may also affect external clients sharing that home.

Profile runtime and account integration are implemented together. The maintainer
reported macOS manual acceptance on 2026-11-16; the [acceptance record](../docs/issues-19-20-acceptance.md)
distinguishes that trial from the remaining detailed real-account, remote and
native-platform validation matrix. Unbound records cannot mutate an ambient login.

Packages include the unmodified codex-auth v0.4.0 release. No PATH installation
or npm download is needed. The explicit `AGENT_HARBOR_CODEX_AUTH_BIN` override
remains for development. Package builders supply the upstream release archive
under `codex-auth/` in the dependencies directory; `codex-auth-assets.txt` pins
the exact filenames and GitHub release-asset digests for all four targets.
The archive checksum is verified before extraction; MIT notices travel in
`licenses/codex-auth-MIT.txt`. Uninstall removes the package executable and
preserves Codex/codex-auth account and conversation data in their client homes.

### Experimental running-session refresh

Set `AGENT_HARBOR_EXPERIMENTAL_CODEX_ACCOUNTS=1` only to try the dedicated App
Server topology and in-place refresh. Basic account-page visibility does not
enable this behavior.

New native and joined Magpie Codex sessions use a dedicated official App
Server while this option is enabled. Harbor verifies the selected identity in
each reachable owned server and reads the active account's official rate
limits. Existing embedded or external sessions cannot be verified by this
flow; check any session reported as unconfirmed before continuing. The
in-place refresh uses an undocumented side effect of an experimental Codex
request, not an upstream authentication-reload contract. Its behavior can
change with Codex versions; plugin/account notifications and switching during
a turn are not established. Turning the option off keeps the prior launch
behavior for new sessions.

For native Codex, Harbor shows the model and provider set in Codex's home configuration when creating or reviewing a session, and flags a configured OpenAI base URL override without displaying it. Project settings and profiles may override home defaults. The original route or login may have changed since the conversation started; check both before resuming. If that configuration already points to Magpie, Harbor does not silently change it. Review and restore the intended client settings explicitly.

In isolated tests with Codex 0.167.0, changing the provider ID made it refuse to resume the original conversation. Keeping that ID but changing its endpoint sent the conversation context to the new endpoint. A provider name alone does not establish the original destination.

The Magpie screen binds external instances, chooses the default for new sessions, starts or stops Harbor's managed instance, and opens provider management for the verified configuration. External instances remain user-managed. Closing management leaves the existing gateway process running; if another TUI originally owns that gateway, closing that original TUI stops it. Harbor's managed gateway keeps running until explicitly stopped.

The managed Magpie UI uses Harbor's separate client configuration paths. An external Magpie UI uses that instance's environment; its agent-model controls can change the client settings in that environment, including settings used by native sessions.

Magpie Library can share skills and MCP servers with a specific project: open the managed Magpie web page with `Ctrl+B m`, then `w`, and use Library's Projects section to add the project and select the items and Codex target. Review the resulting project files: Magpie may place skills in `.agents/skills`, configure MCP in `.codex/config.toml`, and update `.gitignore`. The managed instance's global Codex target uses its isolated home, so assigning a global skill there does not automatically put it in your normal Codex home. Codex plugins remain managed by Codex; assigning a Library item does not install a Codex plugin.

Manage conversation history with the client’s own tools. Harbor no longer deletes client history. `x` removes only the Harbor record and preserves the conversation; running or starting records must be stopped first.

`./agent-harbor stop` stops the managed Herdr workspace and its panes. It does not stop Magpie. Use the Magpie screen or `magpie stop` for Harbor's owned gateway. State lives under `$XDG_STATE_HOME/agent-harbor-bridge`, falling back to `$HOME/.local/state/agent-harbor-bridge`.

Optional PATH entry: append `--link` to the install command to create `~/.local/bin/agent-harbor`. An existing entry is never overwritten. Uninstall removes that link only while it still points at this installation. Add `~/.local/bin` to PATH yourself if needed.

For first use, run `agent-harbor`; dependency checks open the workspace directly when ready. Native and official npm Codex installations are supported. Choose native configuration or Magpie per Session. When needed, use Connections to start a managed Magpie or bind a verified external gateway. Managed Magpie has its own home, configuration, Codex home and ports; it does not import subscription accounts from your native CLI. Set up providers in `Ctrl+B m`. To stop its background gateway explicitly, run `agent-harbor magpie stop`. Uninstall does not stop services: stop sessions and owned services first.

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

Herdr automatic agent restoration is disabled. Resume explicitly through Harbor with the matching candidate query to preserve the recorded conversation and connection. When a complete saved reference has no conflicting evidence but its latest observation is missing, `v` lets you review that reference and connection in the session list; saving the review never starts a conversation. Conflicting or incomplete identities still require independent inspection and explicit reassociation. Resume shows the unverified login/writer state and requires an explicit
confirmation; Details retains the complete evidence. A complete non-conflicting
saved reference can be reviewed and resumed in one action. A failed gateway open
retains the decision and Resume focus for a same-page retry. After a Herdr restart, previous tabs can be plain shells: `c` inspects a recorded previous pane and offers to close it only after checking its tab, title and foreground shell. It refuses other foreground work.

For terminals without Harbor's Unicode glyphs, set `AGENT_HARBOR_ASCII=1` before
starting the managed workspace. `NO_COLOR` disables color and `TERM=dumb` selects
both fallbacks. The display changes do not rewrite stored names or paths.
