# Profile foundation contract (#19 → #20)

This is the first-stage development contract, not completion of Profile isolation.
It extends `applicationService`; it introduces neither a launcher nor an auth
framework. #20 may implement against these signatures. Profile-bound client
execution deliberately returns `ErrProfileNotReady` until #19 integrates and
verifies the runtime boundary. Do not remove this gate merely to make an account
page work. Existing unbound behavior remains the pre-Profile candidate.

## Identity and storage

`profile`, `profileKind`, `profileClientHome`, `profileBinding` and all Profile
service methods are in `bridge/profiles.go`. `profiles.json` lives in the owned
bridge state directory, alongside `sessions.json`. Registry writes use the
existing atomic state-file writer and a cross-process workspace directory lock.
The registry has version 1. Unknown versions fail closed.

`profileBinding` contains `WorkspaceID`, `ProfileID`, `Client`, `EnvironmentID`.
All IDs except Client are opaque, stable random IDs. WorkspaceID identifies the
owning server workspace, not its hostname or a UI label. Remote workspaces reuse
and validate `remote-context.json`'s ID; local registries allocate their own ID.
State copied to another server is not a supported identity migration. Operations
must execute on the owning server. Never resolve remote bindings on a laptop.

Profiles have a display name, a kind, client-home references and an optional
`DefaultInstanceID`. Rename changes only the display name. Names are not unique
identifiers. Each client home records its directory paths and device/inode
identities, independently of its environment ID. This detects replacement at the
same path; normal repair requires an explicit future review operation.

`session.Profile` and `sessionDraft.Profile` are optional pointers to a binding;
`sessionApproval.Session.Profile` captures the reviewed binding as part of the
existing complete-record comparison. `runPlan.Profile` is reserved for persisted
run ownership evidence. Populating/checking it and process evidence is #19's next
stage. New Profile-bound creation currently fails before writing a Session.

Missing legacy fields stay nil. `CodexHome` alone is not a binding. Neither the
resolver nor session-scope query assigns old records to the visible Profile, even
when their home paths match. Explicit legacy review/migration and the labeled
existing-environment UI are still pending. No credential, setting or Conversation
is copied. New-install Default creation/selection is also pending; registry reads
do not silently initialize it.

## Service calls and execution status

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

Construct a binding from a returned Profile and the explicitly selected client
entry: `WorkspaceID: p.WorkspaceID`, `ProfileID: p.ID`, `Client: c.Client`,
`EnvironmentID: c.ID`. Keep this value with every asynchronous request/result.
`profileClientRequest` contains that binding; there is no implicit current Profile.

| Call | Implemented behavior |
| --- | --- |
| Profiles | Reads persisted metadata; absent registry returns an empty list; no initialization |
| CreateProfile | Persists isolated Codex layout or explicit existing-environment roots; validates ownership, permissions and root aliases; does not invoke a client |
| RenameProfile | Checks workspace/profile and changes display name without moving data |
| ResolveProfileClient | Read-only directory validation and environment planning; `Executable` is currently false |
| ProfileSessions | All owned persisted Sessions with the exact binding and client; no path-based inference, external discovery, activity assumption or credential access |
| WithProfileClient inspect | Resolves/revalidates and returns environment plus scoped records under the environment's cross-process lock |
| WithProfileClient account-mutation / launch | Revalidates but returns `ErrProfileNotReady`; callback is never called |
| ValidateProfileResult | Rejects changed visible identity and revalidates captured environment before displaying async results |
| Draft/Create/launch with Profile | `ErrProfileNotReady`; old launcher cannot use ambient environment for a bound record |

`CreateProfile` requires Name and Kind. For `isolated`, existing paths must be
absent; it creates private empty `profiles/ID/{codex,sqlite,runtime}` roots. It may
leave empty unregistered directories after an IO failure; reads/recovery never
adopt them. It does not copy credentials. For `existing-environment`, supply
absolute canonical `ExistingHome` and `ExistingSQLite` from explicit original
environment evidence. Both must already exist; they may be the same directory.
Their contents are preserved. Existing is compatibility, not an isolation claim.
The current stage supports Codex metadata/layout only, not every discovered client.

For #20: account listing/status use the resolved binding and environment. Login,
selection, explicit sign-out and new launch must eventually run inside
`WithProfileClient` with the corresponding operation. Do not use the old
`CodexAccountAction(string, ...)` or call `userEnvironment()` midway through a
Profile action. #20 owns its account request/result payloads and account UI; add
the captured `profileBinding` to them and use this service scope. Until runtime
integration lands, show the returned not-ready error instead of executing against
ambient homes. Extend/inject the existing UI fake for account tests; the base
fake's new Profile methods deliberately return not-ready, never fake success.

## Environment policy

Resolution calls `userEnvironment()` once to recover ordinary development tools,
then overrides the explicit homes. The result's `Environment` is ephemeral and
may contain inherited secrets; never persist or log it.

For isolated Codex the plan sets `CODEX_HOME`, `CODEX_SQLITE_HOME`, `TMPDIR` and
`XDG_RUNTIME_DIR` to the Profile roots. It removes inherited `CODEX_*`, `OPENAI_*`,
`CHATGPT_*` and `AGENT_HARBOR_MAGPIE_KEY` before adding the bound homes. HOME,
PATH, Git/SSH settings, ordinary development tools, network proxies and project
access remain inherited. This is client-data separation, not a process sandbox.
Project configuration/instructions remain deliberately shared.

The required Codex `Arguments` policy is `--no-daemon`,
`-c cli_auth_credentials_store="file"`, and
`-c mcp_oauth_credentials_store="file"`. These are planned upstream CLI overrides;
an environment array alone does not enforce credential-backend policy. #19 must
verify all launch/App Server/login paths apply them, including project override
precedence, saved-account storage, subprocess login and per-session endpoints.
#20 must verify pinned codex-auth honors the same home and invokes Codex with the
required backend policy. Do not pass these Codex arguments blindly to codex-auth.
The existing environment preserves inherited auth/config behavior and has no
isolation claim; it still requires explicit paths and runtime integration.

SQLite roots include database sidecars. Locks, socket paths, notification/status
resources, daemon/App Server ownership and client temporary files must resolve
from the same binding. Disabling shared daemon use and dedicated roots are a plan,
not proof of every resource's isolation. End-to-end verification is outstanding;
therefore no planned environment is currently executable through this contract.

Errors are comparable with `errors.Is`:

| Error | Meaning / response |
| --- | --- |
| ErrProfileNotFound | Registry/Profile absent; refresh or explicitly create/review |
| ErrProfileIdentity | Wrong workspace/client/environment, stale result, replaced directory or incompatible registry; review original identity |
| ErrProfileHome | Missing, unreadable, noncanonical, symlinked or incorrectly owned/private root; repair explicitly; no recreation/fallback |
| ErrProfileAlias | Equal, nested or aliased mutable roots across registered environments; choose independent roots |
| ErrProfileUnsupported | Unsupported client/kind/operation; no isolation promise |
| ErrProfileNotReady | Runtime integration/verification pending; no tool invoked |
| context cancellation | Lock acquisition canceled; no callback invoked |

IO and existing environment-snapshot errors can also propagate. No error permits
fallback to system HOME, another Profile, a different server or a fresh history.
Root checks do not claim to protect against hostile same-user processes replacing
nested files; this feature is not a security sandbox.

## Account scope, async operations and launch coordination

`ProfileSessions` returns stopped, running and pending owned records so #20 can
show the actual scope. It does not establish between-turn safety. #20 must query
activity using the existing application/runtime seams, retain unknown as unknown,
and report per-session adoption/required action; a failed query is not idle.
Unbound legacy records require explicit environment review before account changes
can claim their affected-session scope. External clients may share Existing and
cannot be controlled or counted as verified owned sessions.

Capture the binding when dispatching. `ValidateProfileResult` prevents A's delayed
result from being displayed/applied to B after navigation. Navigation need not
cancel a login already launched in A, but completion and actual resulting state
belong to A. Before consequential work, revalidate the captured binding under
`WithProfileClient`, not merely when the request was queued.

The real lock primitive is `withProfileClientLock`: per Profile/environment,
cross-process `flock`, cancellation-aware, and independent for A/B. The public
service holds it around inspection now. Account mutation and new startup will
share this lock after #19 enables execution. Hold it through credentials mutation,
reread and session adoption; launch holds it through process start and persisted
ownership evidence, then releases it (not the entire Conversation lifetime).
Callbacks must not recursively acquire the same lock. Do not block while asking
a user for confirmation under this lock. Revalidate activity and scope once the
explicit choice is ready to apply. Launch and account code must use the same
public entry point; a private #20 lock would not coordinate with #19's launcher.

## Magpie reference and unresolved boundary

`profile.DefaultInstanceID` is a reference for future session defaults only;
setting/selecting/verifying this default through the service/UI is pending in
#19. `session.InstanceID` and `InstanceSnapshot` remain the fixed connection target.
Never substitute the Profile default during recovery. Instance lifecycle,
provider keys, models/groups and logs stay in the existing instance registry.
Profile selection does not re-home/restart a shared Magpie process.

Inspected Magpie base: `5426c43cca9b90be809d51edebc5c926929e9f6c`
(0.1.3528), with this repository's local caller-only patch. Its
`internal/appdir/codex.go:CodexHomesIn` scans HOME and siblings of CODEX_HOME,
including one level below Codex-named directories. A separate HOME alone is not
proof. The candidate guards `readCodexLogin`, `rememberLogins`, `renewLogins`,
`renewSaved` and official forwarding. Explicit sign-in/write/menu paths still
need exhaustive verification. This patch is not an upstream release.

#19 owns the discovery boundary, including `magpie_record.go` startup environment,
`magpie_manage.go` management environment/menus, gateway capability verification
and any needed upstream candidate changes/tests. Do not expose Profile homes to
Magpie or create another discovery/isolation mechanism in #20. Before allowing
shared-instance isolated execution, #19 must verify startup, background refresh,
management writes and official paths, or block unsafe management/capabilities
explicitly. Caller-only `/api/hello` is necessary for joined requests but does
not independently prove all management/discovery paths safe. #20 owns API-only
versus joined selection, persistence and launch differences; preserve instance
snapshots and caller identity without changing Profile defaults or discovery.

Current upstream checks: Codex latest release API reports `rust-v0.169.0`;
codex-auth reports `v0.4.0`. No real-client account isolation or native-platform
runtime acceptance was run in this stage. Source/document pointers for the next
stage: [Codex environment variables](https://learn.chatgpt.com/docs/config-file/environment-variables),
[Codex auth backend](https://github.com/openai/codex/blob/rust-v0.169.0/codex-rs/login/src/auth/storage.rs),
[codex-auth v0.4.0](https://github.com/Loongphy/codex-auth/tree/v0.4.0).
Recheck exact versions/source and behavior before enabling execution.

## File ownership and integration

| Owner | Next work / entry points |
| --- | --- |
| #19 | profiles.go registry/resolver/coordination; Profile UI; legacy review; session_checks.go; environment.go; identity_candidate.go/run_plan.go; launch.go/session_safety.go; process evidence; query recovery; Magpie discovery/start/management; product/usage/isolation acceptance |
| #20 | app_accounts.go/accountExec request/result binding; codex_accounts.go upstream account operations and per-session results; codex-auth packaging; account page visibility; API-only/joined choice and persisted connection form; catalog and connection-specific launch arguments |
| Shared | service.go, service_fake.go, store.go, app_service.go, launch.go, session creation/UI, tests and public docs |

Keep new logic in focused files; edit shared files at the specific seam without
reformatting unrelated code. #20 extends existing application-service/account
seams and uses these frozen Profile types. Additional compatible methods are
allowed; do not silently change/remove signatures. Neither agent enables the
other's incomplete gate. Integration order remains #19 then #20, followed by
joint A/B acceptance. Development against this contract is not feature acceptance.
