# Bundled software notices

Agent Harbor bundles pinned upstream release binaries: Herdr 0.9.5 and Magpie 0.1.3528. `package.sh` verifies each target's binary SHA-256 before packaging.

- `herdr-LICENSE`: upstream Apache-2.0 license.
- `herdr-THIRD_PARTY_NOTICES`: conservative superset of all 294 registry packages in Herdr v0.9.5 Cargo.lock, including build, development and other-platform dependencies. Each crate archive was verified against its lockfile SHA-256. Where a crate omitted its license files, notices came from its recorded upstream VCS commit. Also includes vendored portable-pty and libghostty-vt licenses.
- `herdr-NATIVE_NOTICES`: libghostty-vt dependencies identified from its vendored build graph: uucode and Unicode data, Highway, simdutf 9.0.0 (the version in the vendored header), Wuffs/pixels, Rust runtime, Zig runtime license, musl and LLVM compiler runtime notices. The release workflow pins Rust 1.96.1 and Zig 0.16.0. Zig's version-tag license URL was unavailable; its upstream current MIT license text is included with that provenance explicitly labelled. Oniguruma is disabled for the libghostty-vt Zig module. Other optional Ghostty GUI dependencies are not linked by Herdr's terminal-only library build.
- `magpie-LICENSE` and `magpie-THIRD_PARTY_NOTICES`: upstream Magpie notices.
- `go-LICENSE`: Go runtime license.
- Files named after Go module paths: notices for Harbor's direct/transitive dependencies, including Bubble Tea, Lip Gloss, Bubbles and TOML.

Herdr source: https://github.com/herdrdev/herdr/tree/v0.9.5
Magpie source: https://github.com/yetone/magpie/tree/v0.1.3528

The conservative Rust dependency list does not assert that every listed package is linked on every platform. Codex and herdr-mcp are separately installed test/user tools and are not included in these archives.

Inline list search uses github.com/sahilm/fuzzy v0.1.1 (MIT, copyright Sahil Muthoo); see `github.com-sahilm-fuzzy-v0.1.1-LICENSE`. This matcher does not implement fzf's query language or promise identical ranking.

Bundled codex-auth v0.4.0 is unmodified upstream software from
https://github.com/Loongphy/codex-auth/releases/tag/v0.4.0 (source commit
4e9803da1d9015f0ff70805322b2780f6fbf5475), under the MIT license in
`codex-auth-MIT.txt`. Release archive SHA-256 pins are in
`../codex-auth-assets.txt`; GitHub release asset digests identify the downloads,
not a reproducible-build or independently verified signature claim.
