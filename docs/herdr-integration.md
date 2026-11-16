# Herdr integration and local patch

Agent Harbor uses Herdr for terminal sessions, client discovery and conversation identity. Its current experimental recovery flow additionally needs a read-only command that asks Herdr to produce its existing client-specific resume command from a structured session reference. Harbor performs its own identity and connection review before invoking recovery; the query itself is not a recovery gate.

The candidate patch is [herdr-resume-command.patch](herdr-resume-command.patch), based on Herdr commit `ee5bfb5041bb3e4a49d591c72308aae3c81ecdde`; the resulting candidate source is `60a5316204b0c538229defb2628e50660a469b85`. The patch is provided for review and possible upstream contribution. It has not been submitted, accepted or released by Herdr. Do not replace a supported installation with this development candidate.

Herdr is licensed under Apache-2.0; its license and bundled dependency notices are in `bridge/licenses/`. Magpie is distributed under its own license, also included in that directory.

## Session display names

Verified upstream HEAD on 2026-11-16 is still `ee5bfb5041bb3e4a49d591c72308aae3c81ecdde`.
Its public `pane.rename` and `tab.rename` methods accept display labels, including
Unicode. `agent.rename` is a separate unique CLI handle with a restricted ASCII
grammar. The agents sidebar renders tab and pane labels, so Harbor sets both
display labels and retains the separate handle for CLI addressing.

Upstream does not read Codex's conversation-name index. Harbor's existing
identity observer supplies that small adapter without changing Herdr: new Codex
Sessions created with an empty name keep the project/connection fallback until
a same-run Conversation or owned running client can supply a title, then capture
the first usable title once.
Codex 0.169.0 stores names in the bound `CODEX_HOME/session_index.jsonl`
(`id`, `thread_name`, latest complete entry wins). Harbor reads only metadata
in that Session's verified environment, with a bounded 4 MiB tail read. With a
Conversation report it selects the exact observed ID. Without one, it verifies
Herdr's foreground process group, Harbor run token and Profile environment, then
matches Herdr's `terminal_title_stripped` against a complete name in that index
(with Codex's optional ` | project` suffix). Ambiguous matches, unknown processes
and unmatched titles retain the fallback. It never selects the newest index row
or manufactures Conversation identity from a title. Missing/invalid titles
retain the fallback. Existing records
are not retroactively opted in. Explicit Harbor renames disable capture, and a
different pane/tab label set directly in Herdr also disables pending capture.
Herdr does not offer conditional label updates; label checks and renames are
separate API operations. This is a one-time copy, not ongoing synchronization.

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
does not overwrite it, then verifies a Unicode Harbor manual rename. The final
sidebar configuration loaded in real Herdr and an ANSI capture was retained.
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
