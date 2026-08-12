# Agent Harbor V3

Agent Harbor manages Codex and Claude sessions, upstream API connections,
traffic rules, model mappings, failover, and request limits from one TUI.

This branch is the V3 release source. It contains the open TUI and launcher
source plus prebuilt Agent Harbor Core binaries. Core source code and local
design documents are intentionally excluded.

## Build

Go 1.26 or newer is required. A local three-file build uses the bundled Core
binary for the current platform:

```sh
make build
```

To build the portable Linux amd64 single file, Docker is also required:

```sh
AGENT_HARBOR_VERSION=0.1.16 make release-linux-amd64
```

The output is
`dist/release-linux-amd64-0.1.16/out/agent-harbor-linux-amd64`. It embeds Core,
the TUI, static tmux 3.6b, terminfo, licenses, and a payload hash manifest.

GitHub Actions builds the same portable artifact on this branch. Download the
workflow artifact, verify `SHA256SUMS`, then install it:

```sh
sha256sum -c SHA256SUMS
install -m 0755 agent-harbor-linux-amd64 /usr/local/bin/agent-harbor
agent-harbor doctor --repair-launchers
agent-harbor
```

Core binary terms are in `core/NOTICE` and `core/LICENSE`. TUI source licensing
and third-party notices are in `tui/LICENSE` and `tui/THIRD_PARTY_NOTICES.md`.
