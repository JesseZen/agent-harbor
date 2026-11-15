# Product acceptance gates

The primary acceptance baseline is the current upstream Codex client release.
Results from older clients are historical evidence, not a pass for a newer
release. The joined connection additionally requires the local Magpie
caller-only candidate described in [the integration guide](../../docs/magpie-caller-integration.md).
Report the exact client and dependency versions tested, and distinguish these
gates from upstream acceptance or release.

## Terminal visual evidence

Use `render_terminal.cjs` for synthetic ANSI previews. It enables xterm's Unicode
15 grapheme provider before writing any text. The default xterm Unicode 6 provider
counts several Harbor emoji as one cell and makes correctly padded dividers look
crooked; old previews using that default are not reliable alignment evidence.

Install development-only dependencies in an isolated temporary prefix:
`@xterm/xterm@6.0.0`, `@xterm/addon-unicode-graphemes@0.4.0`, and
`playwright@1.60.0`. Pass that prefix's `node_modules` as `--modules`; Chrome must
be available. These packages are not bundled with Agent Harbor.

```
node bridge/tests/render_terminal.cjs --input ANSI_DIR --output OUTPUT_DIR --modules NODE_MODULES --check-divider
```

Fixture names end in `-60.ansi`, `-100.ansi`, or `-160.ansi` for the three practical
sizes. The renderer waits for terminal writes, saves PNGs and `geometry.json`, and
checks single-divider rows by their actual terminal cell coordinates. For the
reported emoji/CJK session fixture, `--legacy-unicode` reproduces the failure;
normal rendering must pass. The Unicode addon is experimental: preview checks
supplement actual isolated Herdr interaction, not replace it. Use only synthetic
or explicitly sanitized data for shareable screenshots.

Run `GOWORK=off go test ./...` in `bridge/` under a fresh isolated HOME/XDG/TMPDIR/Codex environment. This covers the process/application-service seams and UI model.

For a native platform archive, use:

```
python3 bridge/tests/run_acceptance.py --package ARCHIVE --codex NATIVE_CODEX --mcp HERDR_MCP_DIST_INDEX_JS --onboarding managed
```

Use `--onboarding external` for the other supported ownership flow. The runner creates fresh isolated environments, uses copied native Codex binaries, runs install, Magpie identity/lifecycle, routing-group rotation, Codex capability, real query recovery, manual fixture lifecycle/marker checks, Herdr shortcut smoke, and full state/UI/MCP/recovery/negative acceptance sequentially. It retains `results.json` and per-gate logs. Exit 77 from a fixture that cannot exclusively own the standard discovery port is a failed gate, not a pass: choose an isolated machine, never stop an existing service.

`app_acceptance.py` is the successor of the historical `e2e_package.py`, `e2e_local.py`, and `sessions_real.py` runners. Their mock server classes remain shared fixture implementations; those historical main entry points predate the current Herdr identity and reviewed recovery flow and are not gates. Their native-session, existing-notify, package, routing and MCP behaviors are covered by the maintained gates above. `e2e_magpie_group.py` remains a gate because it independently verifies actual group rotation.

`manual_acceptance.py start --binary B --herdr H --magpie M --codex NATIVE_C` provides the separate one-command manual environment. Follow its printed open/restart/status/stop commands. Stop preserves diagnostics and only stops fixture-owned resources. A prebuilt package does not require Go or Node for this fixture.

`settings_popup.py` runs the settings popup against real isolated Herdr0.9.5,
copied native Codex (updates disabled), three terminal sizes, configuration
save/reload, all three shortcut remaps including directCtrl, reentry and restart.
Uses the same owned discovery guard and full state isolation as mouse_popup.py.
Settings transaction failure/rollback/recovery tests live in settings_test.go.

For authoritative runtime state acceptance, run the isolated application gate:

```
python3 bridge/tests/app_acceptance.py --full-state --replacement --renamed-codex --binary HARBOR --herdr HERDR --magpie MAGPIE --codex NATIVE_CODEX --mcp HERDR_MCP_DIST_INDEX_JS
```

This adds a manually started external Codex, compares public Herdr observations
with CLI list/show, checks live activity and socket disconnect/reconnect while the
popup stays open, and reviews restored shells before exact recovery. It also
reviews a saved identity through the popup, then resumes the same synthetic
conversation through both native and managed connections and verifies their
original histories and routes. Use the locally patched development candidate
described in `docs/herdr-integration.md`; this is not evidence of official
upstream acceptance or release.

To exercise the opt-in App Server launch topology with the same isolated UI
fixture, add `--experimental-codex-accounts`. This fixture uses only a
synthetic API key; it does not establish live ChatGPT account switching.
`TestAccountRefreshVerifiesIdentityAndReadsOfficialQuota` checks the RPC
sequence, including failed refresh and identity mismatch. Live account
evidence must be reported separately.

The experimental App Server recovered both native and caller-only Magpie
conversations in isolated macOS testing. Immediately after stopping a
background pane, Herdr may briefly retain its old agent observation. Harbor
retries that specific stale observation for up to three seconds before refusing
recovery; the fixture still waits for the observation to disappear before
asserting the final state. The bounded retry is covered by bridge unit tests;
real-client timing evidence remains fixture-specific.

Add `--model-picker` to exercise the real Magpie catalog in Codex, save a model
and reasoning effort for one Harbor session, make a temporary choice in a second
session, and check the saved choice and actual request after recovery. The
fixture uses synthetic providers and separate Codex and Magpie homes.
