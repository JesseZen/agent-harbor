# Herdr integration and local patch

Agent Harbor uses Herdr for terminal sessions, client discovery and conversation identity. Its current experimental recovery flow additionally needs a read-only command that asks Herdr to produce its existing client-specific resume command from a structured session reference. Harbor performs its own identity and connection review before invoking recovery; the query itself is not a recovery gate.

The candidate patch is [herdr-resume-command.patch](herdr-resume-command.patch), based on Herdr commit `ee5bfb5041bb3e4a49d591c72308aae3c81ecdde`; the resulting candidate source is `60a5316204b0c538229defb2628e50660a469b85`. The patch is provided for review and possible upstream contribution. It has not been submitted, accepted or released by Herdr. Do not replace a supported installation with this development candidate.

Herdr is licensed under Apache-2.0; its license and bundled dependency notices are in `bridge/licenses/`. Magpie is distributed under its own license, also included in that directory.

## Live popup upgrade

Packaged workspaces use a private launcher for the Harbor, Settings and Magpie
shortcuts, the status command and plugin pane entrypoints. The launcher verifies
the script installation's `current` or the formula's `opt` and uses the matching
bundled executables. If a live server passes the former bundled Herdr executable
path to a new plugin pane, the launcher selects the verified current companion
in that installation and keeps the existing server socket.

On the first `ah` after upgrading a legacy workspace, Harbor recognizes its
generated config and manifest from the installation layout and saved shortcuts.
An old Homebrew executable can already have been removed. Under the workspace
open lock, Harbor checks and hot-reloads the config and requires an online
`plugin.link` acknowledgment from the owned socket. The rollback journal lets a
later `ah` retry restoration after an interrupted upgrade. The current config,
manifest and registration are checked before writes. Existing popup processes
continue to run until closed. See [the isolated gate](../bridge/tests/README.md#live-popup-upgrade).

## Preparing Codex recovery identity

Managed Codex startup checks `integration status` and the hooks setting using the
Session's resolved Profile environment and project directory. It installs the
official integration through `herdr integration install codex` for isolated
Profiles, asking first for an existing environment or explicitly disabled hooks.
The choice `skip` opens the client without promising automatic recovery; cancel
does not start it. A prepared environment does not prompt for installation again.
Codex still presents its own hook trust review. Harbor neither edits trust hashes
nor treats installation as proof of the active Conversation.

This does not recover an ID that was never captured. For an ended record without
a reference, Harbor stops before resume confirmation. Inspect the original
client history and explicitly associate the intended Conversation; do not select
the globally newest history entry. Existing conflict, Profile and connection
checks remain in force. Normal observed identities no longer generate yellow
reminders in the Sessions list; uncertain observations remain visible in details.

## Immediate Codex primary selection candidate

[The Codex patch](codex-primary-selection.patch) is based on `rust-v0.170.0`
(`29eb79e9866b3262a777fdf8195c1b9e63684c91`).
[The Herdr patch](herdr-primary-selection.patch) is based on the 0.9.5 Harbor
fork at `7a19fabf023d1cf2d23cf58eeeb8028a9f282b61`. Build both in separate
checkouts and set `AGENT_HARBOR_EXPERIMENTAL_CODEX_PRIMARY_SELECTION=1` for an
isolated Harbor workspace. The patches are local candidates. Standard packages
still recover the saved conversation and make no claim about later `/resume`
selections. The Codex tag's `Cargo.lock` still lists unpublished workspace
crates as `0.0.0` after their manifests moved to `0.170.0`. Refresh the lock with
the pinned Cargo before a `--locked` build and check that the resulting changes
are workspace version substitutions. The candidate adds no dependencies.

After the local TUI commits an in-place `/resume` selection, it verifies the
selected thread is a main conversation and reports the exact old and new IDs
with a run token and sequence. Herdr uses the socket peer PID to verify the
pane's foreground process group. Harbor verifies the owned Herdr socket and
executable, the Codex process, its Profile environment and the active Session
run. The receiver can use the exact approved startup reference while its
`SessionStart` hook is pending. Herdr acknowledges the switch after Harbor's
atomic and synced Session write. The Session retains the original reference
evidence and connection. The client keeps both conversations' histories.

The internal request is `pane.report_primary_selection` on the local Herdr
socket. Accepted evidence is stored under `conversation.primary_selection` in
`sessions.json`. A subsequent passive report for a previously selected ID or an
empty pane observation leaves the accepted selection in place for that run.
The record links the previous verified run through `previous_run`, retaining
its original evidence and accepted references for review. Only the current
run's `prior_references` are used to recognize stale reports. A different
unconfirmed reference retains the saved target and requires review. See the
[isolated real-client gate](../bridge/tests/README.md#immediate-codex-primary-selection).

On 2026-11-19, the Linux amd64 candidates passed the exact no-prompt A→B
restart and recovery, immediate `/exit`, A→B→C, and real subagent navigation
gates with an isolated loopback Responses provider. The macOS arm64 stock
control used native Codex 0.170.0 and Herdr 0.9.5 and retained the saved A after
Codex displayed B. Patched native macOS, Magpie and remote sessions still need
their own real-client gates.

## Session display names

Verified upstream HEAD on 2026-11-16 is still `ee5bfb5041bb3e4a49d591c72308aae3c81ecdde`.
Its public `pane.rename` and `tab.rename` methods accept display labels, including
Unicode. `agent.rename` is a separate unique CLI handle with a restricted ASCII
grammar. The agents sidebar renders tab and pane labels, so Harbor sets both
display labels and retains the separate handle for CLI addressing.

Upstream does not read Codex's conversation-name index. Harbor's existing
identity observer supplies that small adapter without changing Herdr: new Codex
Sessions created with an empty name keep the project/connection fallback until
a same-run Conversation or owned running client can supply a title, then follow
usable title changes for the lifetime of that Harbor Session.
Codex 0.169.0 stores names in the bound `CODEX_HOME/session_index.jsonl`
(`id`, `thread_name`, latest complete entry wins). Harbor reads only metadata
in that Session's verified environment, with a bounded 4 MiB tail read. Harbor
prefers the reported ID's title when it matches the terminal title. If the
terminal title instead uniquely matches another complete entry in that Profile
index (including Codex's optional ` | project` suffix), Harbor can update only
the display name after verifying the owned foreground run, Harbor run token
and Profile environment. Without a verified Conversation it requires the same
foreground proof. Ambiguous matches, unknown processes and unmatched titles
retain the fallback. It never selects the newest index row or manufactures
Conversation identity from a title.
Missing/invalid titles retain the fallback. Existing records
are not retroactively opted in. Explicit Harbor renames disable synchronization,
and a different pane/tab label set directly in Herdr also disables it. Herdr does
not offer conditional label updates; label checks and renames are separate API
operations. When the terminal title is used without a verified Conversation,
Harbor still requires the same owned foreground run and Profile; this changes
only the display name, never the saved Conversation or recovery target.

The owned sidebar uses Herdr's native styled row tokens: the pane title is
bright and bold, machine/workspace and connection details are dimmed, and the
client label uses a restrained accent (blue by default, peach for Claude,
teal for Pi). State icons retain Herdr's semantic colors. These colors describe
client type and state, not account identity. No ANSI sequences are stored in
names and no Herdr rendering patch is needed. This first version uses visual
hierarchy rather than assigning a random hue to every Conversation.

Sources: [Herdr display/API implementation](https://github.com/herdrdev/herdr/tree/ee5bfb5041bb3e4a49d591c72308aae3c81ecdde/src/app),
[agents sidebar](https://github.com/herdrdev/herdr/blob/ee5bfb5041bb3e4a49d591c72308aae3c81ecdde/src/client/shell/agent_sidebar.rs),
[Codex title index](https://github.com/openai/codex/blob/rust-v0.169.0/codex-rs/rollout/src/session_index.rs),
[Codex terminal-title fields](https://github.com/openai/codex/blob/rust-v0.169.0/codex-rs/tui/src/chatwidget/status_surfaces.rs).

Validation on 2026-11-16: isolated Linux amd64 full Go tests, targeted race
tests, build and vet passed. The maintained `app_acceptance.py --replacement
--session-names` passed with real Codex 0.169.0, Herdr 0.9.5, Magpie
0.1.3528-harbor-dev and codex-auth 0.4.0. The fixture uses a synthetic native
provider, creates a Conversation before invoking Codex `/rename`, checks the
exact title in Harbor and Herdr pane/tab labels, verifies a later Codex rename
synchronizes to Harbor and Herdr, then verifies a Unicode Harbor manual rename.
The final sidebar configuration loaded in real Herdr and an ANSI capture was retained.
The Profile-bound title-reader regression also verifies that ambient home B
cannot supply a name for A, and A's missing home cannot trigger fallback.
This is Linux evidence; the new naming/style changes have not received native
macOS or remote visual acceptance.

Follow-up on 2026-11-16: user testing exposed a new unnamed live Session with a
generated Codex title and correct Profile home, but no Herdr hook identity. The
original observed-ID gate therefore prevented naming. A regression reproduced
that failure before the fix. The maintained naming gate now leaves hooks off,
returns structured title-generation responses to real Codex and checks its
automatic title without an initial `/rename`. This passed on Linux amd64 with
the versions above and asserts no Conversation ID was inferred. Negative tests
cover wrong foreground processes, unmatched titles and direct Herdr manual names.
The internal `_refresh-session-names` maintenance command applies the same
checks to pending names after upgrading observers, without restarting clients.

## Remote workspace candidate

Apply [herdr-remote-executable.patch](herdr-remote-executable.patch) to the query
candidate `60a5316204b0c538229defb2628e50660a469b85` to build this branch's remote
dependency. It adds an optional `--remote-executable /absolute/posix/path` to
`herdr machine add`, persisted per machine. Setup, live client reconnects,
machine status and remote API discovery use that exact path. An explicit path
does not fall back to PATH discovery or an automatic installation. API metadata
cached for another path is discarded. Existing profiles without the field keep
their normal discovery behavior. This local patch is not submitted, accepted or
released upstream and adds no server wire-protocol changes.

Remote candidate assets retain the query candidate's `.source` value and must
also include `.remote-patch.sha256`, the SHA-256 of the exact additional patch.
These are local operator attestations, not upstream signatures. See
[remote workspaces](remote-workspaces.md) for setup and recovery boundaries.
