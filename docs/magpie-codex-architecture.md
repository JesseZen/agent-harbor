# Codex and Magpie: proposed integration architecture

Status: **target design; not yet implemented or accepted**. The goal is to
reuse Codex, Herdr and Magpie, keeping Harbor's coordination and long-term
client-specific maintenance small. This document describes desired boundaries
and known evidence, not a claim that every upstream feature works together.
The behavioral requirements are in the [coordination spec](magpie-codex-coordination-spec.md).

## Responsibilities

```text
Harbor ── session intent, lifecycle coordination, review ── Herdr
   │                                                       │
   └────────────── start or recover the client ─────────────┘
                              │
                            Codex
             login, conversation, /model, tools and plugins
                              │ model requests
                              ▼
                       local Magpie gateway
                        /               \
          caller's OpenAI identity     third-party providers / groups
                    │                          │
             OpenAI backend             Magpie routing and keys
```

Codex owns its authentication, thread, model and effort selection, skills,
plugins and client capabilities. Magpie owns its model catalog, providers,
groups, routing and protocol translation. Herdr owns terminal hosting and the
client capabilities it actually exposes. Harbor records only the intent and
evidence it needs to select the environment and connection, operate the
session and avoid acting on an uncertain conversation or identity.

Account switching, gateway integration and session recovery are distinct
responsibilities; they are **not** a mandate to create three Harbor adapters.
First use a documented upstream feature or a suitable existing tool. Add a
small client-specific integration only for a demonstrated gap, document its
owner and upgrade burden, and prefer an upstream contribution over a permanent
local fork. Do not duplicate OAuth, a model catalog, a gateway, a router or a
client's session implementation.

The same ownership principles apply to other clients, but their login, model
picker, routing and recovery capabilities must be verified separately. Herdr
support for a client's native session is not proof that its Magpie integration
or account features are supported.

Use the latest upstream client release available for the target platform as
the primary implementation and acceptance baseline, or the version the user
explicitly requests. Check the actual installed version and current upstream
release when work starts; for example, a future Codex 0.169.0 installation is
an example, not a permanent pin. Record exact versions for reproducibility.
Earlier 0.167.0 results below are historical evidence, not a requirement to
keep that client or to maintain compatibility with it. Recheck the behavior
and interfaces on the chosen version instead of assuming they are unchanged.

## Desired experience

1. Choose a client and login environment and, when desired, a verified Magpie
   instance and initial model or group. The native/direct connection remains
   available without silently changing another session's configuration.
2. In the login-preserving Codex connection, `/model` offers eligible official
   models alongside Magpie models and groups. A chosen group is a stable
   routing intent, not a promise that every request uses the same member.
   Magpie may change the member according to the user's rules; the client can
   continue displaying the group name. Group settings may constrain effort.
3. Users choose models and effort in Codex. On recovery, preserve the exact
   conversation and connection and let Codex restore its **last** selection,
   including a session-only selection made without another user turn. The
   group selected by the user is not replaced with its last routed member.
4. When both accounts have usable login state, aim for one explicit action to
   switch A to B without another browser login. Prefer keeping existing
   conversations running if the actual client and credential mechanism
   reliably support it. Otherwise offer a verified safe handoff, or explain
   that the operation is unavailable; never report a switch as complete based
   only on a configuration or UI change. Expired credentials can require login.

An added model may require a catalog refresh or client restart; group membership
and catalog refresh are separate. Do not promise live refresh without evidence
for the selected client and integration. Switching to a third-party model or a
group can send the relevant conversation context to the actual provider.

## Authentication and routing boundary

For a request intended for an official ChatGPT model, the target is **strict
caller-identity passthrough**: use the current Codex caller's ChatGPT identity,
do not substitute a Magpie-stored account, and do not use subscription-account
rotation, masking or failover to bypass an allowance. An authentication error
or limit must remain visible. This identity rule also applies to an official
member reached through a group and to compaction or retry paths. A deliberately
selected group can route among its explicitly configured third-party members;
never send the caller's ChatGPT credential to a third-party provider. Group
rules do not authorize replacing the caller's ChatGPT account.

All model requests in the proposed joined mode pass through the local Magpie
gateway, including requests eventually sent to OpenAI. “OpenAI upstream” does
not mean “no local proxy”: the gateway can see the request and its credential.
Sign-in, plugins, MCP and remote-control traffic can have separate paths; the
diagram does not imply that Magpie manages them.
The [OpenAI gateway documentation](https://learn.chatgpt.com/docs/enterprise/connect-to-a-gateway#handle-credentials-at-the-gateway)
describes forwarding the caller's credential for OpenAI inference, but does
not establish that a particular Magpie configuration enforces this boundary or
that every deployment has the same authorization terms. Commercial or hosted
use needs its own review of applicable official terms and deployment design.

Prefer existing Magpie configuration if it can be proved to enforce the
boundary, including concurrent callers and failures. If configuration is
insufficient, the preferred next step is a minimal change **in Magpie** that
offers this behavior as an upstream-compatible, explicitly chosen routing
policy. Prepare it against the current upstream code with tests and a clear
contribution path; keep existing Magpie routing available to users who do not
choose the policy. Avoid adding a second Harbor proxy or permanently carrying
Magpie's routing logic in Harbor.

The policy must cover all relevant official paths, not just the initial
response: caller authorization and account ID where supplied, model listing,
streaming, compaction, retries and concurrent requests. Verify the actual
upstream protocol and leave unrelated third-party group routing intact. If
enforcement is unproved, do not call a mode strict or silently enable it;
make the limitation visible and keep native/direct use available. A locally
built Magpie patch is a downstream maintenance responsibility until upstream
accepts and releases it; acceptance and release are not guaranteed.

Account switching is independent of the gateway policy. Prefer a supported
Codex mechanism or an appropriate existing account-switching tool; add only a
small bridge if it fills a verified gap without duplicating credential
management. Verify whether a switch reaches existing processes and changes the
identity of subsequent requests; otherwise use a verified handoff or expose
the limitation. Do not assume that a gateway change alone switches accounts.

An account environment and a Magpie group are different: an account identifies
the caller; a group describes routing. One shared login environment can affect
multiple running sessions when its identity changes. Simultaneous independent
account identities generally need separate client homes or another supported,
proven isolation mechanism. A Codex configuration `--profile` by itself is not
an independent login. Do not share or rewrite client state databases to make
separate environments appear interchangeable.

## Evidence and unresolved questions

These observations are version-specific; recheck current upstream interfaces
before implementation. They are not end-to-end acceptance of the design.

| Status | Finding |
| --- | --- |
| Source: Magpie 0.1.3528 | Its ChatGPT-preserving integration uses Codex's built-in provider with a Magpie base URL and a combined model list. Its separate `/v1/codex/models` catalog is not evidence of the joined path. [Wiring](https://github.com/yetone/magpie/blob/v0.1.3528/internal/agent/codex.go#L40-L64), [combined catalog](https://github.com/yetone/magpie/blob/v0.1.3528/internal/gateway/codex_backend.go#L2270-L2275). |
| Source: Magpie 0.1.3528 | Gateway routing can select stored accounts; `Always ChatGPT` avoids a particular provider takeover, but does not by itself prove strict caller-identity passthrough. [Routing](https://github.com/yetone/magpie/blob/v0.1.3528/internal/gateway/codex_backend.go#L264-L296), [modes](https://github.com/yetone/magpie/blob/v0.1.3528/docs/subsystems/agent-wiring.md#L698-L701). |
| Source: OpenAI App Server docs | `account/updated` is a server notification, while experimental `chatgptAuthTokens` makes the host responsible for external credentials and refresh. Neither establishes that a separate App Server can switch every existing Codex TUI process. [Authentication API](https://learn.chatgpt.com/docs/app-server). |
| Isolated test: Codex 0.167.0 and Magpie 0.1.3528 | Against synthetic providers and a separate Magpie provider, Codex restored a conversation's last model/group and effort, including an `s` choice with no later turn, when no model override was supplied on recovery. This was not the joined-login path or end-to-end Harbor recovery. |
| Isolated test: real clients and Magpie, synthetic providers | A live Codex and a live Claude Code session using the same group observed member changes in tested scenarios. This does not prove zero-delay propagation, live catalog refresh or Harbor support for every client. |
| User observation | After an official logout/login to account B, other already-running Codex sessions consumed B's allowance without restart. The particular authentication mechanism and behavior with Magpie or a third-party switcher remain unverified. |
| Unverified | Strict passthrough under 401/429, retry, compaction and concurrent accounts; joined-mode real-account features; account-switch scope and recovery across environments; provider or model changes after compaction; end-to-end Harbor and Herdr behavior. |

The current working-tree candidate uses a separate Magpie provider, a catalog
snapshot and per-session Codex profiles; its forced recovery selection can miss
a later `s` choice. It is not this design. The local Herdr recovery query remains
a development patch and is not an accepted or released upstream interface; see
[Herdr integration](herdr-integration.md).
