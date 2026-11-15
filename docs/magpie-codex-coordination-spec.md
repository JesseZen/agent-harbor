# Codex and Magpie coordination spec

Status: **approved direction; implementation and acceptance pending**. The
[architecture](magpie-codex-architecture.md) sets the ownership and evidence
boundaries. This spec describes target behavior; it does not certify the
current development candidate.

## Scope and priorities

Provide a login-preserving Codex connection to an explicitly selected Magpie
instance, with official and Magpie models/groups in Codex's picker, while
preserving safe conversation recovery and native/direct use. Magpie owns
provider configuration and routing; Codex owns login, model selection and
thread state; Herdr owns terminal and supported client capabilities; Harbor
coordinates rather than rebuilding these features.

An explicit account switch, ideally without restarting an existing session, is
a desired capability. When both accounts have usable login state, aim for one
action without another browser login; expired credentials may require login.
Implement it using an upstream or established mechanism only when its identity,
scope and recovery behavior are demonstrably correct.
When a reliable hot switch is unavailable, use a verified minimal handoff if
possible, or clearly report the operation as unavailable. A limitation is an
unmet feature, not a successful switch. This does not authorize implicit
account rotation.

Broad refactoring and removal of obsolete Harbor-specific code are in scope.
Use the smallest maintainable integration supported by evidence. Preserve user
data and unrelated work; do not retain a custom adapter solely because it
already exists. Do not add a general adapter framework, proxy, auth manager,
model registry or compatibility layer without a concrete demonstrated need.
When Herdr or Magpie already expose a supported client's capabilities, avoid
creating another Harbor-specific lifecycle, history parser or recovery command
for that client. Show unverified capabilities as such; this spec does not
claim that other clients already support the Codex experience.

The primary client baseline is the latest upstream release available for the
target platform when implementation and acceptance run, unless the user
specifies another version. Recheck the installed client and upstream release
at that time, record the precise versions and whether they are stable or
prerelease, and adapt the product and relevant fixtures to that baseline.
Codex 0.167.0 is historical test evidence, not a requirement to stay on it or
to add compatibility machinery for it. If newer clients expose a real Herdr or
Magpie gap, resolve or clearly report it instead of silently falling back to
the old client and treating that as acceptance of the new one.

## User-visible behavior

- **Connections and environments:** Allow the native configuration and a
  verified Magpie connection without silently rewriting unrelated native
  settings, another account's credentials or another session's connection.
  Respect user-managed and Harbor-managed Magpie ownership. If upstream
  connection setup writes shared configuration, constrain its scope or make
  the effect explicit; do not mistake a configuration profile for an isolated
  account. A native connection uses the user's effective client settings and
  must not be advertised as direct-to-OpenAI without evidence.
- **Model and group selection:** The joined connection exposes eligible
  official and Magpie entries using the actual upstream catalog. An initial
  selection may be supplied at new-session launch; subsequent `/model` and
  reasoning-effort choices belong to Codex. Clearly distinguish a selected
  group from its routed member. Magpie group policy may override effort.
  Explain a stale, hidden or unavailable catalog or model and the refresh
  needed; do not fabricate model names or routing results.
- **Conversation recovery:** Recover the same identified thread, working
  directory, account environment and connection. Preserve the last client
  model/group and effort, including an `s` choice made with no subsequent turn.
  Prefer native client restoration over replaying a Harbor-recorded initial
  model. Do not silently force an older model or show an initial choice as a
  verified last choice. If selection evidence is unavailable, say so rather
  than invent it; use supported client behavior to restore it when proven. A
  session-only `s` selection must not change another session's default.
- **Identity and account switching:** Make account changes deliberate and
  indicate their scope, especially when several sessions share a client home.
  Verify the effective caller identity and, in acceptance, the identity used
  on the next request; a changed file, `account/read` result or UI label alone
  is not proof of request routing. Separate environments for concurrent A/B
  use must not share mutable session databases as an isolation shortcut.
  An explicit A-to-B switch authorizes the displayed scope; do not demand a
  redundant confirmation just because that authorized identity changed.
  Requests already in flight can complete under A; report an uncertain outcome
  instead of claiming subsequent requests use B without evidence. Expired
  credentials, a failed handoff or unclear identity must be reported without
  silently selecting a different account or thread.
- **Skills and capabilities:** Preserve the client's own skills, plugins, MCP
  and eligible signed-in features; do not replace them with a Harbor manager.
  Magpie Library may manage the targets it supports, whose scope must be clear.
  Login is a prerequisite for some capabilities, not proof that every plugin,
  connector, remote-control flow or model supports them. Compaction behavior
  and continuation after model/account/provider changes must be checked before
  claiming portability. Do not modify a real conversation to make a test pass.

## Required routing guarantees

For any official ChatGPT request, including a group member, retry and
compaction, use the initiating caller's current identity and never silently
substitute another ChatGPT account or bypass an allowance. A direct official
model choice must not silently fall back to a different provider. On 401, 429,
missing identity or unavailable guarantees, report the failure or require a
deliberate choice as appropriate. Do not leak ChatGPT credentials to third-party
providers. A deliberately selected group retains its explicitly configured
third-party routing and failover within these identity boundaries.

Do not advertise strict passthrough until its entire relevant path is verified;
prove a configuration-only solution first. If the available configuration
cannot satisfy it, prefer implementing a small, testable, upstream-compatible
policy in Magpie itself over adding a separate proxy or routing implementation
to Harbor. The policy must preserve the existing choices of Magpie users who
do not enable it and cover model listing, response/streaming, compaction,
retries and concurrent requests as supported by the actual client protocol.
Validate that OpenAI authorization and the account ID when present remain the
caller's, while third-party requests receive only the appropriate provider
credential. Test the policy with the joined catalog and configured groups.

Keep account switching separate: evaluate Codex and established tools first,
then a minimal bridge only if needed and justified. A switch mechanism must
not become an automatic subscription-account pool. If no solution satisfies
the routing guarantee, leave the feature unavailable or explicitly limited and
preserve a safe native/direct path. Do not silently change the route of an
existing session as a migration shortcut. An existing provider-bound
conversation may not be transferable by merely changing its provider ID,
endpoint or home. A Magpie patch, an upstream contribution, maintainer
acceptance and a released version are distinct statuses to report separately.

## Recovery and migration safety

Before launch, focus or recovery, retain the established ownership, workspace,
directory, live-target, duplicate-writer and conversation/connection checks.
If evidence conflicts, is stale or is insufficient, expose the uncertainty and
require an explicit review; never infer the latest conversation or substitute
a different account or Magpie instance. Preserve existing records, user
settings and histories. Keep old paths working until a replacement has passed
the corresponding behavior and migration checks; an explicit, reviewed
transition is required where an old connection cannot be preserved.

Use supported Herdr and client interfaces where possible. The current Herdr
recovery query is a local candidate, not upstream acceptance or release. A
successful isolated test or a locally patched binary must be reported as such.

## Acceptance evidence

Use isolated homes, state, ports and processes with synthetic accounts and
providers first. Record the client, Magpie and Herdr versions and distinguish
source evidence, synthetic fixtures, real clients and real-account results.
Run the relevant real-client acceptance on the current client baseline above;
adapt historic version-pinned checks rather than treating their old baseline as
the new requirement. Preserve historic fixtures where useful, without making
old-client compatibility an implicit delivery obligation.
At minimum, exercise:

1. A joined catalog with official, third-party and group choices; actual
   requests and effort; two concurrent sessions; group member changes; hidden,
   removed and unavailable models; and unchanged unrelated native settings.
2. Restart and recovery of the same thread after Enter and `s` selections,
   including no following user message and changed shared defaults; preserve
   the selected group, not its last member. Check actual routed requests and
   duplicate/uncertain identity refusal through Harbor and Herdr.
3. Caller A/B passthrough concurrently and after an explicit switch, including
   401, 429, retry, group, compaction and gateway failure. Check which
   synthetic identity reached which upstream without recording credentials;
   verify no ChatGPT credential reaches third parties or another account is
   silently selected.
4. The real client account-switch mechanism with multiple running sessions,
   its scope and a safe fallback. Establish the identity used by a subsequent
   request, not just changed credential storage. If real-account evidence or
   account support is unavailable, mark the claim and ticket incomplete.
5. Available skills, MCP, plugins/connectors, remote control and compaction
   with appropriate account permissions. Record each verified, unsupported or
   untested capability; do not equate login or a synthetic fixture with a
   complete real-account pass.
6. Existing and legacy connection recovery, missing/changed instance or home,
   unavailable Herdr, concurrent writers, stale approvals and no mutation of
   unrelated settings or histories. Record platform limitations and upstream
   patch status separately from product behavior.

Use the repository's [acceptance guide](../bridge/tests/README.md). Heavy
tests should run remotely when practical, with test-created artifacts cleaned.
No real token, private conversation, sensitive log or local machine state may
be included in test fixtures or committed documentation.
