Changes in v0.1.0-dev.6:

- Accounts automatically refreshes 5-hour and weekly Codex quota and reset-card availability for saved logins in the current Profile.
- Open an account's details to view quota reset times and card expiry. Confirm redemption to use one card through bundled codex-auth and refresh the results.
- The account list keeps quota percentages visible on narrow panes and adds timestamps and bars as space allows.
- Sessions and selection lists use consistent keyboard cursor, mouse hover, and applied selection styles. Shortcut hints fit in at most two lines.

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
