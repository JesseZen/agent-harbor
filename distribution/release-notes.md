Fixes in v0.1.0-dev.4:

- Recognize official npm Codex installations by resolving their native executable, including Profile account management.
- Open the workspace directly after dependency checks. Choose native or Magpie connections per Session and set up optional gateways in Connections.
- Show dependency failures without incorrectly asking users to reinstall bundled Herdr.

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
