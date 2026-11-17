# Release maintenance

The tag workflow builds on native GitHub runners: macOS arm64/amd64 and Ubuntu
24.04 arm64/amd64. It pins dependency source commits, applies the tracked patches,
packages all four executables and licenses, and tests the actual native package
and CLI lifecycle in isolated homes. `build-dependencies.sh` is the reproducible
entry point. Rust 1.96.1, Zig 0.16.0 and Go 1.26.8 are build-time requirements.

To publish a development candidate, commit changes and push a new unique
`v0.*` tag, for example `v0.1.0-dev.2`. Never move or reuse a published tag.
The workflow requires all four builds/tests, uploads release archives,
`SHA256SUMS`, `install.sh` and `agent-harbor.rb`, publishes a prerelease, then
commits the Homebrew formula and `distribution/latest` pointer to main. The
channel pointer intentionally supports development prereleases, independently
of GitHub's stable-only latest-release API. If a build fails, it does not change
the public channel. If publication succeeds but the channel push fails, fix the
main-branch conflict and commit the generated formula/pointer; do not recreate
the release. A draft release from an interrupted upload must be inspected before
retrying. Workflow write access to main is required for channel updates.

The online installer verifies the selected release's checksum file over HTTPS.
This detects corrupt or mismatched downloads; it is not an independent signature.
It stages in the installation filesystem, serializes installers with a lock,
keeps old release directories and atomically switches `current`. It never edits
shell profiles, client credentials, histories, services or workspace state.
Close/reopen workspaces explicitly when ready to adopt an update. Old versions
are retained until uninstall because existing workspace configurations may pin
them. Uninstall does not stop running processes.

Homebrew uses a formula in this repository's `Formula/` directory and downloads
the same archives. Its lifecycle belongs to brew. No npm wrapper is published.
The binaries are not Apple Developer ID signed/notarized. Linux Herdr is a native
glibc build requiring glibc 2.39 or newer. Real client/account/recovery acceptance
is separate from native packaging and install/update/uninstall tests.

Local installer acceptance:

```sh
python3 bridge/tests/distribution_install.py /path/to/native/agent-harbor
python3 bridge/tests/package_install.py /path/to/native/archive.tar.gz
```

The first test serves synthetic release archives on an ephemeral loopback port;
it covers both aliases, CLI delegation, corrupt downloads leaving the current
version intact, lock/contention, foreign command refusal, update, reinstall,
retained old versions and data-preserving uninstall. No real accounts are used.
