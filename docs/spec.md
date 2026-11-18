# Product outline

Agent Harbor helps a user create, inspect, resume and manage AI CLI sessions inside an owned Herdr workspace. Herdr manages terminal and client capabilities; Magpie provides optional provider and routing configuration.

A session records a stable Profile binding, project, client, conversation reference and connection intent. A native connection uses the client's own settings and login. A Magpie connection names a verified instance and a model or routing group. Different sessions can use different connections. Auxiliary Magpie requests may route portions of a conversation to other providers and must be explained to users.

On restart or recovery Harbor must preserve the intended conversation and connection. If either cannot be established, show the uncertainty and require explicit review; never silently substitute a client, account, route or conversation. External Herdr agents may be listed and focused without claiming ownership or inferring their connection.

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

When recovery has a complete saved reference and no conflicting identity evidence,
the Resume action confirms that exact reference and its recorded connection in
one step. Evidence is optional detail, not a prerequisite navigation step. A failed
launch retains the reviewed snapshot for retry; any intervening record or runtime
change still requires a fresh review. Recovery uses Enter for Resume and Esc to cancel, with no footer focus or Tab step.
Failure retains the same recovery context. Arrow keys scroll the
evidence. Missing references and actual identity or connection conflicts remain
blocking; recovery never substitutes another conversation.

An explicit Session name takes precedence over automatic titles. New Codex
Sessions created with an empty name initially show the project and connection,
then copy the first available title once into Harbor and Herdr's tab/pane
display labels. A verified Conversation selects its exact title-index entry.
Without a hook report, a verified owned running client may supply a display
title through Herdr's normalized terminal title, matched against that Profile's
Codex title index. Display naming never supplies Conversation/recovery identity.
Later manual renaming remains
available and disables pending automatic capture. Older records retain their
names. Other clients keep the project/connection fallback until their title
capability is verified. See [Herdr integration](herdr-integration.md).

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

An opt-in experimental Codex account integration may use `codex-auth` to save
and manually select logins and dedicated Codex App Servers to keep Harbor
sessions open during a switch. Refreshing an already-running server currently
depends on an undocumented side effect of `app/list`; show that limitation and
report each session whose selected identity cannot be verified. Switch only
between turns. The selection applies to the displayed Profile’s Codex home, and Magpie must
continue forwarding the selected caller's identity without automatic rotation.

The account page is available by default and operates on an explicitly selected
Profile/client environment. It offers official Codex login through bundled
codex-auth, saved-login selection and “Not signed in” while retaining saved
accounts. Account mutations coordinate with launch and refuse active or unknown
session activity. Each affected session reports its outcome; ordinary account
management does not restart processes or depend on experimental refresh.
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
Recovery retains the saved connection form, instance and Conversation regardless
of subsequent login changes. An expired or unconfirmed login requires repair or
explicit sign-out. API-only catalogs do not include cached official choices.
The integrated Profile-bound creation candidate is covered by that maintainer
acceptance report; per-scenario automated evidence remains separately recorded.

Opt-in running App Server refresh still relies on an undocumented side effect
of `app/list`. It is separate from basic account management and reports each
session whose identity cannot be verified.

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
`uninstall`. Updates keep old executables for running workspaces and never restart
client processes or migrate their configuration. Uninstall removes owned commands
and package executables while retaining client data, ownership metadata, licenses
and user-added files. Homebrew owns its own installation lifecycle. The bundled
Herdr and Magpie remain patched development candidates; distribution does not
change their upstream acceptance or runtime evidence.

Startup checks Herdr and Codex dependencies, then opens the workspace directly.
It does not require a connection choice or start/bind Magpie. Each new Session
chooses its own Native or Magpie connection. The new-session form starts with
the last applied connection type for the same Profile and client. On reopening
Harbor it can use the latest saved Session in that Profile and client. A new
Magpie session still requires an instance and a model or routing group. Optional
gateways are set up through Connections when needed. Codex discovery supports native executables and the
native payload in the official npm package without running unknown wrappers.
