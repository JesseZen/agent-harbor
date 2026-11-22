# Agent Harbor

Agent Harbor manages AI CLI sessions in Herdr and connects them to either a client's native configuration or a selected Magpie model or routing group. Herdr owns terminal sessions and client discovery; Magpie owns provider configuration and routing.

## Development status

This repository contains an **experimental development candidate**, not a supported release. Exact conversation recovery relies on a local Herdr query patch which has not been submitted to, accepted by, or released by Herdr. See [Herdr integration](docs/herdr-integration.md). Earlier native macOS arm64 and Linux amd64 candidate acceptance used isolated real clients, real Magpie and simulated providers. The new Profile runtime has Linux amd64 evidence with Codex 0.169.0; its native macOS, remote and real-account gates remain open. Other architectures have build/package evidence only. Future behavior and client compatibility may change.

The integrated #19/#20 candidate passed maintainer-reported manual acceptance on
macOS on 2026-11-16. See the [acceptance record](docs/issues-19-20-acceptance.md)
for its scope and the separate validation requirements.

## Install and update

Native packages are published on [GitHub Releases](https://github.com/JesseZen/agent-harbor/releases)
for macOS and Linux, arm64 and amd64. These are development candidates, including
patched Herdr and Magpie plus codex-auth. Install the Codex CLI separately (native or official npm installation).
Linux requires glibc 2.39+ (for example Ubuntu 24.04); Alpine/musl is not supported
by these packages. macOS packages are not Apple notarized.

```sh
curl -fsSL https://raw.githubusercontent.com/JesseZen/agent-harbor/main/distribution/install.sh -o /tmp/install-agent-harbor.sh
sh /tmp/install-agent-harbor.sh
export PATH="$HOME/.local/bin:$PATH"
ah
# agent-harbor is the same command
ah update
# ah uninstall
```

Add the PATH setting to your shell profile for future terminals. The installer
needs curl and tar; it does not need Go, Rust, Node or sudo. It supports
`--version TAG`, `--prefix ABSOLUTE_PATH` and `--bin-dir ABSOLUTE_PATH`.
Both command names must be free on first install. The default prefix is
`~/.local/share/agent-harbor`; commands go in `~/.local/bin`.
Updates retain previous executables. Run `ah` after updating to migrate a
recognized workspace's popup commands and plugin registration online. Close and
reopen the popup to use the new version. Restart a workspace explicitly when
you want to replace its running Herdr server. Uninstall keeps accounts,
conversations, configuration, package ownership metadata, licenses and added
files. Reinstall uses the same retained client data.

Homebrew (macOS or Linux):

```sh
brew tap jessezen/agent-harbor https://github.com/JesseZen/agent-harbor
brew install jessezen/agent-harbor/agent-harbor
brew upgrade agent-harbor
# brew uninstall agent-harbor
```

Use one installation method per machine. Homebrew manages its own updates and
uninstall; script lifecycle commands only manage script installations. npm is
not required or published. See [release maintenance](distribution/README.md).

## Code and testing

The active Go module is in `bridge/`. Go 1.26 is required. Run `GOWORK=off go test ./...` from that directory using isolated HOME, XDG, temporary directories and client homes. Product runtime and packaging require Herdr, Magpie and a supported native client.

For rapid local TUI validation on the development machine, run
`./bridge/dev-run.sh`. It builds only the host's Harbor binary and atomically
replaces the Homebrew `libexec/agent-harbor`, reusing the installed Herdr,
Magpie and codex-auth companions. On Apple Silicon this is a native arm64 build
and normally takes a few seconds. `brew upgrade` or `brew reinstall` restores
the released binary. Set `AGENT_HARBOR_DEV_COMPANION_DIR` when the companions
are not installed through Homebrew. The command uses the existing Harbor state,
so close the production workspace before testing changes that affect its
running sessions; already-running Herdr panes keep their current process.

Start here when working on the project:

- [Product outline](docs/spec.md) and [glossary](docs/glossary.md): intended behavior and terminology.
- [Codex and Magpie architecture](docs/magpie-codex-architecture.md) and [coordination spec](docs/magpie-codex-coordination-spec.md): login-preserving integration targets and the boundaries for the development candidate. The Magpie caller-only patch and account refresh are experimental and are not upstream releases.
- [Profile service contract](docs/profile-contract.md): workspace identity, scoped client environments, shared Magpie boundaries and current acceptance limits.
- [Herdr integration](docs/herdr-integration.md): the upstream boundary and candidate recovery patch.
- [Remote workspaces](docs/remote-workspaces.md): native Herdr machine connections and server-owned Harbor sessions.
- [Development candidate guide](bridge/PACKAGE-README.md): packaging and usage of the candidate.
- [Acceptance guide](bridge/tests/README.md): tests and isolated runtime acceptance.
- [Agent instructions](AGENTS.md): project guidance for coding agents. An optional, untracked `AGENTS.local.md` and `.agents.local/` directory can hold machine-specific guidance and documents.

This snapshot replaces a previous product direction. The former Core, TUI and launcher are not part of this product.

## License

Agent Harbor is available under the [MIT License](LICENSE). Bundled dependencies and Herdr's separately licensed patch have their own notices in [bridge/licenses](bridge/licenses/).
