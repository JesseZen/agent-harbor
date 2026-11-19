Changes in v0.1.0-dev.11:

- Enable live account refresh by default for new Native Codex and signed-in Magpie Codex windows, using a private official Codex App Server per window. No experimental environment variable or version allowlist is required. If the server cannot start, Codex opens with the same connection, model and launch options.
- Check window activity through Herdr before changing accounts. A busy window shows a waiting screen with its name; Esc cancels the pending switch. Missing an App Server no longer makes an otherwise verified idle window block the saved login change.
- Restore the selectable `Not signed in` row without deleting saved logins. Authorize added accounts in a private temporary home before changing the Profile login. Results show the saved login and whether each running window adopted it.

Existing windows do not acquire an App Server after updating. New eligible windows use it automatically; results identify older windows whose live account change could not be confirmed. Live sign-out is not verified. The refresh uses an undocumented upstream side effect and verifies the resulting identity before reporting success.

Validated with unmodified Codex 0.170.0 and isolated synthetic credentials/providers, including a second turn after switching Native and signed-in Magpie windows. Real account acceptance remains for this development candidate.

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
