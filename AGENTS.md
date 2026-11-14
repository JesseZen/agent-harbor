# Agent Harbor development

Start with [README.md](README.md) for the repository layout and development status. Read [docs/spec.md](docs/spec.md) before changing product behavior, and inspect the relevant code and tests before editing.

If `AGENTS.local.md` exists at the repository root, read it for machine-specific development constraints and the current work entry point. It is local and must not be committed. The user's current instructions take precedence over repository guidance.

## Product boundaries

- `bridge/` is the active Go module. Herdr provides terminal sessions and client capabilities; Magpie provides optional provider configuration and routing. Check the actual upstream interfaces before relying on a capability.
- Preserve the intended conversation and connection on recovery. When identity or connection evidence is insufficient, expose the uncertainty and require an explicit choice.
- For recovery or upstream changes, read [docs/herdr-integration.md](docs/herdr-integration.md). The Herdr recovery query is a development candidate; distinguish a local patch from upstream acceptance and release.

## Working and validation

- Inspect Git status and worktrees first. Preserve unrelated user changes and protect credentials, conversations and private material.
- Match tests to the changed behavior. For runtime, packaging or acceptance work, follow [bridge/tests/README.md](bridge/tests/README.md) and use isolated homes, configuration, state, ports and processes. Do not disturb existing user-managed services.
- Report what was actually tested and any missing platform, real-client or upstream evidence. Keep secrets, local state, build artifacts and private planning material out of commits.
