# Product outline

Agent Harbor helps a user create, inspect, resume and manage AI CLI sessions inside an owned Herdr workspace. Herdr manages terminal and client capabilities; Magpie provides optional provider and routing configuration.

A session records a project, client, conversation reference and connection intent. A native connection uses the client's own settings and login. A Magpie connection names a verified instance and a model or routing group. Different sessions can use different connections. Auxiliary Magpie requests may route portions of a conversation to other providers and must be explained to users.

On restart or recovery Harbor must preserve the intended conversation and connection. If either cannot be established, show the uncertainty and require explicit review; never silently substitute a client, account, route or conversation. External Herdr agents may be listed and focused without claiming ownership or inferring their connection.

Use documented upstream integration interfaces. Unsupported operations must be presented clearly. Keep user credentials and conversations out of logs and test fixtures.

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

The account page is available by default and operates on an explicitly selected
Profile/client environment. It offers official Codex login through bundled
codex-auth, saved-login selection and “Not signed in” while retaining saved
accounts. Account mutations coordinate with launch and refuse active or unknown
session activity. Each affected session reports its outcome; ordinary account
management does not restart processes or depend on experimental refresh.
Profile client execution remains gated until its runtime integration is verified.

New Magpie connections record either an independent API provider when signed
out or a caller-preserving joined connection with a confirmed usable login.
Recovery retains the saved connection form, instance and Conversation regardless
of subsequent login changes. An expired or unconfirmed login requires repair or
explicit sign-out. API-only catalogs do not include cached official choices.
The complete Profile-bound creation journey still requires integration acceptance.

Opt-in running App Server refresh still relies on an undocumented side effect
of `app/list`. It is separate from basic account management and reports each
session whose identity cannot be verified.

The proposed [Codex and Magpie architecture](magpie-codex-architecture.md) and
[coordination spec](magpie-codex-coordination-spec.md) describe a
login-preserving connection and client-owned model selection. They are target
behavior, not a claim that the current development candidate provides it.
