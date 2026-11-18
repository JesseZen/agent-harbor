Changes in v0.1.0-dev.7:

- The new-session form recalls the most recent Native or Magpie choice for the current Profile. Magpie setup still asks for an instance and model.
- Accounts keeps the last quota visible during background refresh. Press `n` to add a login.
- Click a row to move the cursor, then click the current row to perform its Enter action. Arrow keys and `hjkl` navigate the main tabs, lists, and forms. Tab and Shift+Tab no longer cycle focus.
- Removed the Connections default-instance shortcut and obsolete first-run choice code.

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
