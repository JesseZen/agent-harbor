# Profile service contract

The initial contract is commit `5fcd13ad650566fc42d9f3ba3a4a4f90b21015c9`.
Its signatures remain source compatible. The runtime implementation now resolves
bound environments and coordinates launch with account mutations. This document
separates implemented behavior from acceptance that still needs a real environment.

## Stable identity and persistence

`bridge/profiles.go` defines `profile`, `profileKind`, `profileClientHome`,
`profileBinding`, `profileCreateRequest`, `profileClientRequest`,
`profileClientEnvironment`, `profileClientOperation` and `profileClientScope`.
`profiles.json` has version 1 and is stored beside `sessions.json`, using the
existing atomic writer and a cross-process workspace-directory lock.

A binding contains `WorkspaceID`, `ProfileID`, `Client` and `EnvironmentID`.
IDs are opaque and stable; display names never select an environment. Remote
workspaces reuse and validate their `remote-context.json` identity. Resolve and
execute on the owning server, never on a laptop with a similarly named Profile.
Copying state to another server is not a supported identity migration.

Each client reference records absolute canonical roots and device/inode identities.
Missing, unreadable, symlinked, replaced or overlapping roots require repair;
recovery does not recreate them. Rename does not move data. Profile creation
creates empty private Codex, SQLite and runtime roots; it imports no settings,
credentials or history. An IO failure may leave empty unregistered directories;
reads never adopt them automatically.

`session.Profile`, `sessionDraft.Profile`, `sessionApproval.Session.Profile` and
`runPlan.Profile` carry the binding. Session creation copies it; the existing
complete-record comparisons invalidate stale approvals. Run plans must match the
record before reservation. Process evidence additionally checks workspace,
Profile, environment and run-token variables, plus the bound Codex home.

Missing old fields stay nil. `CodexHome` alone does not establish a Profile.
Unbound sessions cannot launch or open another pane, including before registry
initialization. Explicit `ReviewSessionProfile` can bind a stopped, unchanged
record to a reviewed existing environment; it cannot move it into an isolated
Profile or change its Conversation/connection. Legacy inspection retains the
previous read-only compatibility behavior until the environment is reviewed.

## Application service

The original methods remain:

```go
Profiles() ([]profile, error)
CreateProfile(profileCreateRequest) (profile, error)
RenameProfile(profileBinding, string) error
ResolveProfileClient(profileClientRequest) (profileClientEnvironment, error)
ProfileSessions(profileClientRequest) ([]session, error)
WithProfileClient(context.Context, profileClientRequest,
    profileClientOperation, func(profileClientScope) error) error
ValidateProfileResult(captured, visible profileBinding) error
```

Compatible additions are:

```go
EnsureProfiles() (profile, error)
SelectProfile(profileBinding) error
SetProfileDefaultInstance(profileBinding, string) error
ReviewSessionProfile(sessionApproval, profileBinding) error
ProfileEnvironment(profileBinding) (appEnvironment, error)
ProfileCatalog(profileBinding, string) ([]catalogModel, error)
```

`EnsureProfiles` is an explicit application initialization effect, serialized
across processes. A fresh installation receives isolated Default. An existing
client home containing data, or existing session records, preserves an explicitly
labeled existing environment. If the original roots are unavailable, creation
stops; the user can explicitly create a fresh isolated Profile alongside the
unresolved old records. `Profiles` and `ResolveProfileClient` do not initialize.

Selection persists a default for future actions. An open UI retains its captured
selection; another popup changing the default does not move its drafts or running
sessions. `appEnvironment.Profile` reports the captured environment.

For existing environments, creation requires explicit `ExistingHome` and
`ExistingSQLite` roots. It records the original path-variable snapshot separately
from client credentials. Codex uses those explicit roots. Other discovered native
clients can use the existing environment's anchor and original paths; this is
compatibility, not a claim that they share Codex's storage layout or have isolated
support. New isolated support is Codex-only.

## Environment policy and account integration

Resolution restores ordinary user tooling once, then applies the bound roots.
The returned `Environment` can contain inherited secrets: never log or persist it.
Isolated environments remove inherited `CODEX_*`, `OPENAI_*`, `CHATGPT_*` and
`AGENT_HARBOR_MAGPIE_KEY`. They set CODEX_HOME, CODEX_SQLITE_HOME, TMPDIR and
XDG_RUNTIME_DIR to their own roots, preserving HOME, PATH, Git/SSH and normal
project/tool access. Project files, instructions and deliberately shared tools
remain shared. Profiles are not OS-user/container security sandboxes.

`Arguments` includes the supported Codex policy: disable shared daemon use,
force `cli_auth_credentials_store="file"`, force
`mcp_oauth_credentials_store="file"`, and pin `sqlite_home` and `log_dir`.
SQLite configuration can override CODEX_SQLITE_HOME, so both controls are needed.
The resolver also rejects Profile home config that redirects those roots or
selects another credential backend. The launcher applies the policy to TUI and
owned App Server starts; `--no-daemon` is a TUI flag and is omitted from the App
Server subcommand. Login adapters must apply the same policy to their Codex child.
Do not blindly pass Codex flags to codex-auth itself.

`Executable` is now true for successfully resolved supported environments.
Consequential operations still revalidate the binding, client/connection,
Conversation, instance, directory and runtime evidence. An environment plan is
not authorization to ignore those checks. The old ambient `CodexAccountAction`
remains source compatible but always refuses mutations; #20's scoped account
adapter is the consumer of `WithProfileClient`.

`ProfileSessions` returns all owned matching records, including stopped/pending
ones. It does not prove activity, idleness or safe adoption. #20 owns activity
checks, explicit signed-out selection, actual codex-auth operations, per-session
results and optional experimental refresh. Unknown activity must remain unknown.
External clients may share an existing environment and cannot be counted as
verified owned sessions.

Capture the value binding at dispatch and carry it in every async result.
`ValidateProfileResult(captured, visible)` rejects stale cross-Profile UI results.
Account execution revalidates the captured binding under `WithProfileClient`;
navigation never redirects a pending login into the newly visible Profile.

`WithProfileClient` uses a cancelable cross-process lock keyed by
workspace/Profile/environment/client. A and B do not serialize each other.
Account mutations hold it through mutation, reread and adoption. Launch uses the
same primitive from validation through child start and persisted ownership, then
releases it before the Conversation lifetime. Do not recursively acquire the
same lock or wait for user confirmation while holding it. #20 must not add an
independent switching lock that fails to coordinate with launch.

Dedicated App Server sockets live under the bound runtime root. Codex 0.169.0 can
publish long requested socket paths through its supported short-path rendezvous
symlink; Go RPC resolves the physical socket path before dialing. Ownership still
checks the requested socket identity, PID, run token and binding. Shared upstream
socket infrastructure is not treated as permission to reuse another server.

Errors remain comparable using `errors.Is`: `ErrProfileNotFound`,
`ErrProfileIdentity`, `ErrProfileHome`, `ErrProfileAlias`,
`ErrProfileUnsupported`, `ErrProfileNotReady`, and context cancellation.
Policy/configuration and IO errors can also propagate. None permits fallback to
system home, a different server/Profile, a new Conversation or another gateway.

## Magpie boundary

Profile `DefaultInstanceID` affects future selections only. Existing sessions
retain `InstanceID` and `InstanceSnapshot`; recovery never substitutes the new
default. Providers, routing rules, keys, logs and lifecycle remain instance-owned.
The UI explains that an instance shared by Profiles has shared configuration/logs.

Isolated sessions require both `codex_caller_only` and `codex_profile_isolation`
in `/api/hello`. The latter comes from the supplemental local
[`magpie-profile-isolation.patch`](magpie-profile-isolation.patch), applied after
the caller-only patch to base `5426c43cca9b90be809d51edebc5c926929e9f6c`.
It disables Codex-home enumeration (including history scans), direct credential
writes/deletion, and retains the caller-only read/refresh/request guards.
This is a local development candidate, not an upstream release.

Startup/helper/management environments discard caller auth/home selectors and the
original-environment snapshot. A management process must preserve the verified
caller-only flag and must not use a Profile's mutable roots. Unsafe/unknown
management is blocked when isolated Profiles exist, including unbound TUI
fallback. An older gateway is not silently reconfigured or stopped. Package
creation verifies the supplemental patch attestation as well as executable hashes.

#19 owns this resolver/discovery/management boundary. #20 owns API-only versus
joined choice, catalogs for that connection form, persisted provider provenance,
account UI/actions and codex-auth packaging. It should extend ProfileCatalog or
consume ResolveProfileClient rather than restore ambient homes. The existing
unscoped catalog remains for instance inspection and legacy compatibility;
Profile-aware catalog reads only the bound client's official model cache.

## Evidence and integration

The integrated #19/#20 candidate received maintainer-reported manual acceptance
on macOS on 2026-11-16; see the [acceptance record](issues-19-20-acceptance.md).
The detailed automated evidence and remaining validation scope follow below.

The targeted native tests use Codex 0.169.0, local synthetic upstreams and
independently authored synthetic credentials. They check A/B request identity,
separate histories/SQLite, A credential change with B untouched, exact A
Conversation recovery, and separately owned App Servers. They do not establish
real ChatGPT authorization, codex-auth rotation or cloud account eligibility.
The supplemental Magpie guard tests run on the actual patched source, alongside
its caller-only native-client/identity tests.

The maintained `app_acceptance.py --replacement --profiles` exercises UI-created
Profiles, concurrent real clients and same-Profile Conversations, recovery of
A while B is selected, and exact A/B recovery across workspace/gateway restart.
This journey passed on Linux amd64 with Codex 0.169.0, Herdr 0.9.5 and the
Magpie 0.1.3528 Profile candidate using synthetic providers and credentials. It requires an exclusive discovery port, as documented in
the acceptance guide. Exit 77 is a blocked gate, never a pass. Runtime evidence,
exact SHAs and outstanding native-platform/real-account gates must accompany a
candidate handoff.

Shared files include service.go, service_fake.go, store.go, app_service.go,
launch.go, session creation/UI and public docs. Keep compatible signatures and
focused edits. #20's API/account branches must be retained when integrating;
#19's environment policy, lock, run ownership and recovery checks must also be
retained. Integration order remains #19 then #20, with joint A/B acceptance before
claiming the complete account feature. Do not merge to main or release implicitly.
