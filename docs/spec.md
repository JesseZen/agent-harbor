# Product outline

Agent Harbor helps a user create, inspect, resume and manage AI CLI sessions inside an owned Herdr workspace. Herdr manages terminal and client capabilities; Magpie provides optional provider and routing configuration.

A session records a stable Profile binding, project, client, conversation reference and connection intent. A native connection uses the client's own settings and login. A Magpie connection names a verified instance and a model or routing group. Different sessions can use different connections. Auxiliary Magpie requests may route portions of a conversation to other providers and must be explained to users.

On restart or recovery Harbor must preserve the intended conversation and connection. If either cannot be established, show the uncertainty and require explicit review; never silently substitute a client, account, route or conversation. External Herdr agents may be listed and focused without claiming ownership or inferring their connection.

If a verified owned foreground run switches to another valid main Conversation
inside Codex (through `/resume`, `/agents` or `/new`) and reports it through the
official Herdr hook, Harbor updates that Session's current recovery target to
the newly reported Conversation and records the transition in Details history,
preserving the Session's Profile, project, connection and options. Automatic
titles follow the latest Conversation unless the Session was manually renamed.
If another currently active Harbor Session already owns that Conversation, or if
the run association is lost, the baseline is unverified, or an older record
already carries an unresolved conflict, Harbor keeps the saved target unchanged
and requires explicit review. Unverified or ambiguous runs remain separate from
saved records.

Use documented upstream integration interfaces. Unsupported operations must be presented clearly. Keep user credentials and conversations out of logs and test fixtures.

Before a managed Codex launch, Harbor checks Herdr's identity integration in the
Session's bound Profile environment. An isolated Profile prepares the official
integration automatically unless hooks were explicitly disabled. Existing
environments and explicitly disabled hooks require a choice to prepare recovery,
continue without automatic recovery, or cancel. In Harbor's creation flow this
choice happens in the popup before publishing a Session record or opening its
terminal; cancelling preserves the draft. A first-launch skip remains an explicit
choice in the bound Profile and never supplies Conversation identity.
Codex owns hook trust; Harbor
never approves it on the user's behalf. Installation alone is not Conversation
identity: only an observed reference can make recovery ready. Records without a
captured reference stop before recovery confirmation and require explicit
association with the original Conversation. Normal identity observations do not
produce warning banners; blocking summaries stay brief and details retain evidence.

Sessions uses Enter to focus a verified live terminal or review recovery after
the run ends. The recovery path checks the previous pane and can proceed with
its verified idle shell still open. A complete saved reference and connection
need one confirmation. A conversation conflict with two complete references
requires an explicit choice between the originally saved and last reported
conversations. Details shows the complete references and their evidence. If
Harbor cannot verify the runtime or target, it explains the blocker and offers
a recheck. A failed launch keeps the selected target for retry. Changes to the
reviewed record, runtime or connection require fresh confirmation. Esc returns
to the selected session without applying an unconfirmed choice.

The latest primary conversation selected through Codex `/resume` can be a
recovery target when the client and Herdr support immediate selection reports.
Harbor accepts a report from the verified foreground run, persists the selected
reference and preserves the prior evidence, Profile, project and connection.
That capability requires a real candidate test where the user switches and
exits without sending another turn or opening the Harbor popup. With older
clients or without the integration, Harbor identifies the saved reference as
the saved conversation.

An explicit Session name takes precedence over automatic titles. New Codex
Sessions created with an empty name initially show the project and connection,
then follow verified Codex title changes in Harbor and Herdr's tab/pane display
labels for the lifetime of that Harbor Session. Harbor prefers a reported
Conversation's title-index entry when it matches the terminal title. If another
unique title in the verified Profile index matches, Harbor verifies the owned
foreground run before updating only the display. Without a hook report, title
matching also requires a verified owned foreground run. Display naming never
supplies Conversation/recovery identity.
Later manual renaming remains available and disables automatic synchronization.
Older records retain their names. Other clients keep the project/connection
fallback until their title capability is verified. Display naming never changes
Conversation identity or recovery target. See [Herdr integration](herdr-integration.md).

Settings configures Herdr workspace shortcuts and houses Profiles under
Environment & Profiles (also directly reachable via `p`). Wide viewports split
Settings into a clickable left section sidebar and a right option list where
active-section items are bright, inactive sections are dimmed, modified unsaved
options highlight in yellow, the bottom gray line describes the focused option,
and attempting to leave with unsaved edits surfaces a red `Unsaved changes`
banner above that description; narrow viewports hide the left sidebar. See
[Workspace shortcuts](workspace-shortcuts.md) for defaults, editing and the
direct New session entry.

Extensions (`🧩 Extensions`) manages Skills and Codex Plugins for the active
Profile. Skills are discovered from the shared `~/.agents/skills` store and
`$CODEX_HOME/skills` (using Codex `skills/list` RPC with pure-Go fallback) and
enabled/disabled per Profile in `$CODEX_HOME/config.toml` (`[[skills.config]]`)
without modifying shared source files or requiring Node. Searching, adding,
updating and removing Skills hand off interactively to `skills` CLI (resolved
from `AGENT_HARBOR_SKILLS_BIN`, companion dir, `PATH`, or `npx skills`), while
Codex Plugins use `codex plugin` scoped to the Profile. When multiple Profiles
exist, Extensions offers an explicit declarative plugin-manifest sync between
Profiles that installs missing marketplaces and plugins without symlinking
`plugins/` or copying credentials, histories or MCP OAuth tokens.

New session → Options sets Codex-only launch overrides: Sandbox, Approval,
Reasoning, configured context window, automatic compaction token limit,
post-turn compaction percent, and additional writable directories. Unset means
inherit; `0` explicitly disables post-turn compaction only. Reasoning choices
come from the selected model, not from a shared list of levels. Paths refer to
the server owning the session. `Never` does not remove sandbox restrictions.

Apply remembers options for the full Workspace/Profile/client environment
binding, shared by that binding's Native and Magpie drafts. Cancel discards
unapplied changes; applying and cancelling creation still remembers the choices.
Creating records a copy, so later defaults and recent choices do not change what
Harbor passes on recovery. Sessions → Details shows saved overrides separately
from the last verified Codex context window, when available. A stopped session
can edit options for its next launch without changing the Conversation,
Profile, or connection. Missing path or model evidence is not silently replaced.
Harbor passes explicit Sandbox and Approval overrides to the private Codex App
Server and supplies them to its connected `--remote` TUI through a session-owned
Codex profile (`--profile agent-harbor-<session>`), instead of CLI permission
flags that Codex rejects when resuming a remote task in Command Center or
`/resume`. The App Server owns additional writable directories and the full
server-side overrides. If a higher configuration layer such as project
`.codex/config.toml` or an active unrelated user profile would override the
selected Sandbox or Approval setting, Harbor falls back to in-process
(`--no-daemon`) launch so the session overrides stay effective while keeping
Command Center and `/new` working. Codex applies the session overrides when
starting another Conversation through `/new`. Harbor still uses its own
Conversation evidence and recovery rules. The context
window and the two automatic compaction triggers are independent settings.
Codex can reserve part of the window and cap it to the active model's capacity.

Profiles → Codex defaults edits only the three window/compaction values in the
selected Codex home's user config. The page shows the effective value and its
source for the inspected project. Saving checks the user config version and
leaves unrelated settings, other Profiles and running Conversations alone.
Editing a shared existing environment requires confirmation. Magpie continues
to own model and routing-group capacity; a Codex override does not expand a
model's real capacity.

Remote workspaces use Herdr's native saved-machine client. The local machine
renders the workspace; Harbor management, project files, terminals, agent
clients and optional Magpie run on the selected server. Harbor's session list
is scoped to that server. Disconnecting the client leaves remote work running.
Remote setup explicitly selects an installed Harbor package and an owned state
directory. Each saved connection pins the workspace identity and its isolated
Herdr executable; a missing or changed workspace must not fall back to another
installation, Local, or a new conversation. Transport reconnection and recovery
after server/environment loss are distinct operations. Environment recycling is
controlled by the host platform; surviving processes and retained client history
must not be assumed after recreation. See [remote workspaces](remote-workspaces.md).

Accounts selects the login for the displayed Profile's Codex home; Connections
selects where model requests go. Accounts offers official login through bundled
`codex-auth`, saved logins, and “Not signed in” in the same list. Sign-out
requires confirmation and removes the logged-out active account while retaining
other saved accounts. The check mark represents the stored login, not the login
in an already-running window.

New Native and signed-in caller-only Magpie Codex runs use a private official
App Server automatically, without a switch or version allowlist. If the server
cannot start, open the original CLI with the same launch settings. Independent
Magpie API sessions do not start a dedicated server or block account changes.

Account changes coordinate with launch. Herdr activity is trusted only for a
verified foreground run in the captured Profile, independently of its saved
Conversation identity. A busy window defers the change and the user can cancel
the wait. Unknown activity prevents writing the login and names the window.
Adding a login completes authorization in a private temporary home before this
check. Harbor rechecks before writing the login and before requesting a refresh;
these checks cannot lock out new input to Codex.

Harbor reports the stored login separately from each affected window's result.
`app/list` refresh uses an undocumented Codex side effect. Verify the selected
identity in the owned App Server before reporting a live switch. A verified idle older window without a dedicated server does not block saving.
Older windows, `--no-daemon` fallback windows, failed refreshes and live
sign-out are reported honestly as not confirmed. Do not restart any window
automatically or change its Conversation, model, or connection.
Magpie keeps its providers and does not rotate the caller's official account.
Opening Accounts loads saved Codex logins for the selected Profile and
automatically refreshes 5-hour and weekly availability and reset cards
through bundled codex-auth. Previously displayed API quota stays visible while
refreshing. On a fresh popup, Harbor can display the saved quota snapshot from
codex-auth during that wait. A window without a snapshot shows a loading
placeholder until its result arrives. Each account has its own detail view and
lookup result. An explicit confirmation in Details redeems one card for
that saved account through codex-auth, which selects the card. Harbor
refreshes the account list after the operation.
Profile-bound execution is implemented and received maintainer-reported macOS
manual acceptance on 2026-11-16. The [acceptance record](issues-19-20-acceptance.md)
retains the separate detailed real-account, remote and platform validation scope.

New Magpie connections record either an independent API provider when signed
out or a caller-preserving joined connection with a confirmed usable login.
In the New session form, Model opens the Magpie catalog directly when Harbor
has a selected instance, a verified default, or one verified instance with no
default configured. With an ambiguous or unavailable instance, Harbor asks the
user to choose. The catalog opens with search focused and keeps Change instance
available. Applying a model requires the same routing disclosure and
acknowledgement.
Recovery retains the saved connection form, instance and Conversation regardless
of subsequent login changes. An expired or unconfirmed login requires repair or
explicit sign-out. API-only catalogs do not include cached official choices.
The integrated Profile-bound creation candidate is covered by that maintainer
acceptance report; per-scenario automated evidence remains separately recorded.

The proposed [Codex and Magpie architecture](magpie-codex-architecture.md) and
[coordination spec](magpie-codex-coordination-spec.md) describe a
login-preserving connection and client-owned model selection. They are target
behavior, not a claim that the current development candidate provides it.

A Profile groups Sessions that share personal client data and current login within
one owned workspace/server. New installations get isolated Default; other Profiles
can be created, renamed and selected without moving or restarting running Sessions.
Codex has separate home, SQLite and runtime roots, with file credential backends.
Other clients retain native operation through an explicitly identified existing
environment until their isolation capabilities are verified.

Upgrades retain existing data in a visibly shared existing environment. Old
Sessions lacking a binding require explicit review of their original environment;
neither UI selection nor a matching display name supplies that evidence. Missing
or replaced roots stop recovery instead of being recreated. Profile changes do
not isolate project files; separate worktrees remain the way to separate code edits.

Magpie can be shared across Profiles. Provider keys, configuration and request logs
belong to that instance. A Profile default applies only to future Sessions;
existing Sessions retain their recorded instance and caller identity. Isolated
use requires the local discovery/write isolation candidate as well as caller-only
forwarding. See [the service contract](profile-contract.md) for implementation and
acceptance boundaries; real-account and platform verification remain separate.

Distribution uses versioned native GitHub release archives for macOS/Linux on
arm64 and amd64. Script installations expose `ah` and `agent-harbor`, validate
SHA-256 before switching the active package, and support explicit `update` and
`uninstall`. A reopened popup can follow the workspace's verified package entry
after an upgrade. Opening an existing workspace migrates recognized
Harbor-generated popup configuration and registration under the workspace lock.
The migration preserves shortcuts and rolls back on failure. Already open popups
can finish on their current version. The running workspace, terminals and agent
processes continue through popup migration. Uninstall removes owned commands
and package executables while retaining client data, ownership metadata, licenses
and user-added files. Homebrew owns its own installation lifecycle. The bundled
Herdr and Magpie remain patched development candidates; distribution does not
change their upstream acceptance or runtime evidence.

Startup checks Herdr and Codex dependencies, then opens the workspace directly.
It does not require a connection choice or start/bind Magpie. Each new Session
chooses its own Native or Magpie connection. The new-session form starts with
the last applied connection type for the same Profile and client. On reopening
Harbor it can use the latest saved Session in that Profile and client. Applying
a Magpie model remembers its exact instance and model or routing group in that
Profile/client, including when creation is later cancelled. The model field
restores that pair after reopening Harbor and positions the catalog on it when
it is still available. A new Magpie session still requires a valid instance
and model or routing group. Optional gateways are set up through
Connections when needed. Codex discovery supports native executables and the
native payload in the official npm package without running unknown wrappers.
