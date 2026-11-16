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

An opt-in experimental Codex account integration may use `codex-auth` to save
and manually select logins and dedicated Codex App Servers to keep Harbor
sessions open during a switch. Refreshing an already-running server currently
depends on an undocumented side effect of `app/list`; show that limitation and
report each session whose selected identity cannot be verified. Switch only
between turns. The selection applies to the shared Codex home, and Magpie must
continue forwarding the selected caller's identity without automatic rotation.

The proposed [Codex and Magpie architecture](magpie-codex-architecture.md) and
[coordination spec](magpie-codex-coordination-spec.md) describe a
login-preserving connection and client-owned model selection. They are target
behavior, not a claim that the current development candidate provides it.
