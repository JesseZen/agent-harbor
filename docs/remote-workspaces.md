# Remote workspaces

Harbor uses Herdr's native multi-machine UI. Run the Herdr client on your laptop;
the selected server owns Harbor's management popup, project files, terminals and
agent clients. Closing the client or losing SSH does not deliberately stop the
server or its tasks. Commands that need approval still wait for your answer.

This branch requires the local Herdr remote-executable patch described in
[Herdr integration](herdr-integration.md). This is an experimental integration,
not an accepted or released Herdr capability. The query candidate alone is not
enough to add an isolated remote workspace.

## Prepare once

Install the matching Linux or macOS Harbor candidate into a dedicated prefix on
the server, using the candidate's installer. Install a native client executable
there and deliberately configure its login or providers. Harbor does not copy
your laptop's credentials, conversation history, plugins or project files.

Confirm ordinary SSH access and the server's host key through your usual SSH
configuration. Then, from your laptop:

```sh
agent-harbor remote add Build \
  --ssh workbox \
  --binary /srv/harbor/agent-harbor \
  --state /srv/harbor-state \
  --native
agent-harbor
```

Replace the SSH alias and absolute server paths. `--native` explicitly chooses
the server's native client configuration at first setup; it does not modify an
already completed onboarding choice. Without it, an existing remote onboarding
must already be complete. The state directory must be empty or Harbor-owned and
private. Setup is idempotent for the same owned workspace.

Select **Build** in Herdr's machine sidebar. Open the Harbor popup as usual;
the list, directory picker and connection settings belong to that server. A
server's native connection uses that server's client configuration. Its
`127.0.0.1` Magpie address refers to that server, not your laptop.

The initial remote package installation is separate from adding a connection.
`remote add` does not replace a server's Herdr installation or deploy executables.

## Daily operation

```sh
agent-harbor remote list --json
agent-harbor remote status Build --json
agent-harbor remote reconnect Build
agent-harbor remote remove PROFILE_ID
```

Use the opaque profile ID from `remote list` for removal. Removing or disabling
a machine disconnects it and leaves its server and tasks running. SSH
authentication and host-key recovery follow Herdr/OpenSSH; unavailable remote
machines do not fall back to Local. Herdr's cached disconnected display is not
proof that a task is currently running.

For unattended work, the server needs independent provider access, source files,
MCP services and any Git credentials required by the task. A forwarded laptop
SSH agent or a laptop-hosted gateway cannot perform new operations while that
laptop is offline.

## Restart and temporary environments

An SSH reconnection reattaches to existing remote processes. A server restart or
recycled container ends those processes. Harbor disables Herdr automatic agent
restoration and retains the existing explicit conversation/connection review.

Persist Harbor's state directory, the project's files, the client's history and
the necessary configuration using the host's supported storage mechanisms. Code
backup alone does not establish that client history is retained. If the state
directory is missing, the saved identity-specific executable fails closed. A
new environment requires explicit preparation and a new saved connection; it
does not silently inherit the old workspace's identity.

For an intentionally temporary platform, reopen it through that platform's
normal controls. Harbor does not prevent provider-enforced recycling or schedule
environment restarts.

## Implementation and validation

The server stores a private workspace context and generates an identity-specific
Herdr wrapper. The local Herdr saved profile pins that exact executable. Wrapper
invocations validate the context and session before forwarding native Herdr
probes and bridges. Process, file and socket ownership checks stay on the server
that owns them. Terminal transport, rendering, navigation and reconnects remain
Herdr's responsibility; Harbor does not implement a second terminal protocol.

`bridge/tests/remote_workspace.py` exercises real SSH between two isolated Linux
environments, native Herdr UI, a remote Harbor popup and native Codex against a
synthetic provider. It checks completion after client termination, matching
process/conversation after reconnect, local state isolation and replacement
rejection. Physical laptop sleep and platform-specific data retention need
separate host acceptance.

See [candidate validation](remote-workspaces-validation.md) for tested versions,
package checksums and the limits of the disconnect tests.
