# Agent Harbor development

Start with [README.md](README.md) for the repository layout and development status. Read [docs/spec.md](docs/spec.md) before changing product behavior, and inspect the relevant code and tests before editing.

If `AGENTS.local.md` exists at the repository root, read it for machine-specific development constraints and the current work entry point. It is local and must not be committed. The user's current instructions take precedence over repository guidance.

## Product boundaries

- `bridge/` is the active Go module. Herdr provides terminal sessions and client capabilities; Magpie provides optional provider configuration and routing. Check the actual upstream interfaces before relying on a capability.
- Preserve the intended conversation and connection on recovery. When identity or connection evidence is insufficient, expose the uncertainty and require an explicit choice.
- For recovery or upstream changes, read [docs/herdr-integration.md](docs/herdr-integration.md). The Herdr recovery query is a development candidate; distinguish a local patch from upstream acceptance and release.
- For Codex account switching or Magpie routing, read [docs/magpie-caller-integration.md](docs/magpie-caller-integration.md) and the experimental account section of [bridge/PACKAGE-README.md](bridge/PACKAGE-README.md). Verify current upstream behavior: caller-only preserves identity, while cross-model history adaptation and HTTP fallback differ from byte-for-byte direct transport. App Server auth refresh currently relies on an undocumented side effect.

## Working and validation

- Inspect Git status and worktrees first. Preserve unrelated user changes and protect credentials, conversations and private material.
- Match tests to the changed behavior. For runtime, packaging or acceptance work, follow [bridge/tests/README.md](bridge/tests/README.md) and use isolated homes, configuration, state, ports and processes. Do not disturb existing user-managed services.
- Report what was actually tested and any missing platform, real-client or upstream evidence. Keep secrets, local state, build artifacts and private planning material out of commits.
- When a planned approach fails, report the reproduction, affected user behavior and recommended alternative promptly. Get the user's agreement before changing the workflow, supported scope or acceptance criteria. Ask for specific real-client, endpoint or account access when it is required and isolated fixtures cannot answer the question.
- Lead user updates with the result or blocker. State directly what works, what failed and what remains to verify.

## TUI changes

Before adding a screen or changing TUI layout, controls or interaction, read [the TUI consistency contract](docs/agents/tui.md) and the current implementation tables in [the flow audit](docs/ui-flow-audit.md) and [visual review](docs/ui-visual-review.md). Reuse the shared controls and regress existing pages when adding features; completion requires the documented scope and actual runtime evidence, not only passing tests.

## Agent skills

### Issue tracker

For ticket creation, specification publication and issue updates, read [the GitHub tracker workflow](docs/agents/issue-tracker.md).

### Triage labels

When triaging or publishing agent-ready work, use [the configured label vocabulary](docs/agents/triage-labels.md).

### Domain docs

Before domain research or design, follow [the single-context domain documentation rules](docs/agents/domain.md).
