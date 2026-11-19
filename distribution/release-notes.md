Changes in v0.1.0-dev.9:

- New session → Options remembers Codex sandbox, approval, reasoning, context window, compaction and writable directory choices for the selected Profile environment. Recovery uses the options saved with the original conversation.
- Profiles → Codex defaults edits window and compaction defaults. Session details separates the saved window from the window Codex actually reported, and lets you edit options for the next launch of a stopped session.
- In Sessions, Enter focuses a running terminal or reviews recovery for a stopped conversation. Conflicting conversation references require an explicit choice, and the old terminal is kept.
- Run `ah` after updating to migrate an existing workspace's popup entry points and preserve custom shortcuts. Close and reopen the popup to use the new version without restarting running sessions.

Native development candidate for macOS and Linux, arm64 and amd64.

Includes Agent Harbor, patched Herdr 0.9.5, patched Magpie 0.1.3528 and upstream codex-auth 0.4.0. Install the Codex CLI separately; native and official npm installations are supported. Local dependency patches are not upstream releases.

Install (no sudo):

```sh
curl -fsSL https://raw.githubusercontent.com/JesseZen/agent-harbor/main/distribution/install.sh -o /tmp/install-agent-harbor.sh
sh /tmp/install-agent-harbor.sh
```

Put `$HOME/.local/bin` on PATH, then run `ah` or `agent-harbor`.
Update with `ah update`; uninstall with `ah uninstall`. Uninstall preserves accounts, settings and conversations. Updates retain old executables and do not restart running workspaces. Restart your workspace explicitly when ready.

Homebrew:

```sh
brew tap jessezen/agent-harbor https://github.com/JesseZen/agent-harbor
brew install jessezen/agent-harbor/agent-harbor
```

Use `brew upgrade agent-harbor` and `brew uninstall agent-harbor` for Homebrew installations.

The release workflow runs native package/install tests on all four platforms. Real Codex account and recovery acceptance has a separate scope documented in the repository; a package test is not full client acceptance. Linux binaries require glibc 2.39 or later (for example Ubuntu 24.04). macOS builds are unsigned developer candidates, without Apple notarization.
