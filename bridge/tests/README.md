# Product acceptance gates

The primary acceptance baseline is the current upstream Codex client release.
Results from older clients are historical evidence, not a pass for a newer
release. The joined connection additionally requires the local Magpie
caller-only candidate described in [the integration guide](../../docs/magpie-caller-integration.md).
Report the exact client and dependency versions tested, and distinguish these
gates from upstream acceptance or release.

## Interactive TUI walkthrough

The opt-in walkthrough runs the production Harbor TUI with synthetic accounts,
sessions, Profiles and gateways. Build it from `bridge/` using
`GOWORK=off go test -c -o /path/to/isolated/walkthrough.test`. Run the compiled
binary in a terminal with the isolated environment described below and
`HARBOR_TUI_WALKTHROUGH=1`, passing `-test.run='^TestTUIWalkthrough$'`.

1. From Sessions, press `l` or Right to enter Connections, then repeat for
   Accounts. Use `h` or Left to go back. Try Tab and Shift+Tab on each page.
2. Accounts starts with cached quota. Wait four seconds for the refreshed
   numbers. Leave Accounts, return, or press `r` to repeat the refresh. Click
   the backup account once to move `>`, then again to open Details.
3. From Sessions, press `n`. Connection starts at Magpie. Open Connection,
   choose Native, cancel the form, and open another form to see Native recalled.
   To switch back, choose Magpie, then select an instance and model before
   cancelling and reopening.
4. Hover a session row and compare its tint to the current cursor row. Click
   twice on the unfocused row to see the two steps. Esc cancels the review.
   Try the same focus-then-edit gesture on a Settings field.
5. Press `?` for the page shortcuts. In Sessions search or a text editor,
   type `hjkl` to verify the text stays in the field.

## Terminal visual evidence

Remote workspace fixtures default to `NO_COLOR` on both hosts for text-based
assertions. Use `remote_workspace.py --color` for visual acceptance; its server
helper accepts `setup --color` when preparing a manual environment. Unsetting
`NO_COLOR` only in a later SSH shell does not change an already running Herdr
server's environment. Prepare visual fixtures with colors before starting it.

Use `render_terminal.cjs` for synthetic ANSI previews. It enables xterm's Unicode
15 grapheme provider before writing any text. The default xterm Unicode 6 provider
counts several Harbor emoji as one cell and makes correctly padded dividers look
crooked; old previews using that default are not reliable alignment evidence.

Install development-only dependencies in an isolated temporary prefix:
`@xterm/xterm@6.0.0`, `@xterm/addon-unicode-graphemes@0.4.0`, and
`playwright@1.60.0`. Pass that prefix's `node_modules` as `--modules`; Chrome must
be available; `--executable PATH` can select an existing Chromium executable. These packages are not bundled with Agent Harbor.

```
node bridge/tests/render_terminal.cjs --input ANSI_DIR --output OUTPUT_DIR --modules NODE_MODULES --check-divider
```

Fixture names end in `-60.ansi`, `-80.ansi`, `-100.ansi`, or `-160.ansi`.
Default render sizes are 60×20, 80×24, 100×30 and 160×45. Use `--rows 24`
for a directory of actual 60×24 captures; preserve the captured row count so
rendering does not silently crop the footer. The renderer waits for terminal
writes, saves PNGs and `geometry.json`, and
checks single-divider rows by their actual terminal cell coordinates. For the
reported emoji/CJK session fixture, `--legacy-unicode` reproduces the failure;
normal rendering must pass. The Unicode addon is experimental: preview checks
supplement actual isolated Herdr interaction, not replace it. Use only synthetic
or explicitly sanitized data for shareable screenshots.

Before accepting a rebuilt UI binary, verify that the fixture's executable matches
the intended build. Herdr may retain an existing `_app` process when a popup is
hidden and reopened; reentry alone does not prove that the new binary is running.
Replace a fixture-owned executable atomically if it is still in use, restart only
the fixture-owned popup process, and verify the running build before capturing
results. Keep user-managed services and sessions untouched.

Run `GOWORK=off go test ./...` in `bridge/` under a fresh isolated HOME/XDG/TMPDIR/Codex environment. This covers the process/application-service seams and UI model.

For a native platform archive, use:

```
python3 bridge/tests/run_acceptance.py --package ARCHIVE --codex NATIVE_CODEX --mcp HERDR_MCP_DIST_INDEX_JS --onboarding managed
```

Startup now opens the workspace directly after dependency checks. The runner retains `--onboarding` for fixture compatibility; it prepares required gateways explicitly after workspace entry. The runner creates fresh isolated environments, uses copied native Codex binaries, runs install, Magpie identity/lifecycle, routing-group rotation, Codex capability, real query recovery, manual fixture lifecycle/marker checks, Herdr shortcut smoke, and full state/UI/MCP/recovery/negative acceptance sequentially. It retains `results.json` and per-gate logs. Exit 77 from a fixture that cannot exclusively own the standard discovery port is a failed gate, not a pass: choose an isolated machine, never stop an existing service.

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

`query_recovery_real.py` now starts with no preinstalled Herdr integration. It
approves Harbor's scoped setup prompt, handles Codex's native hook trust review,
finishes a turn, exits with `/exit`, and recovers the exact Conversation and
context without `bind` or `reassociate`. `--preinstalled` retains the prepared
fixture path; `--skip-identity` checks that explicitly skipped setup leaves the
reference missing and stops UI recovery before confirmation. The Profile A/B gate likewise leaves scoped integration installation
to Harbor at first launch. The native gate also cancels preparation before creating a record, verifies no
terminal was opened, then re-enters the draft and exercises Prepare or Skip.
Setup tests cover approval, skip, cancellation, explicit
hooks disablement, installation failure, repeated launches and an unrelated
ambient Codex home. Missing-reference UI tests stop before confirmation; normal
identity reminders must not be rendered as list warnings.

Add `--model-picker` to exercise the real Magpie catalog in Codex, save a model
and reasoning effort for one Harbor session, make a temporary choice in a second
session, and check the saved choice and actual request after recovery. The
fixture uses synthetic providers and separate Codex and Magpie homes.

## Profile gates

The integrated candidate received maintainer-reported macOS manual acceptance on
2026-11-16. See the [acceptance record](../../docs/issues-19-20-acceptance.md).
The gates below retain their specific evidence requirements; the report does
not supply per-scenario results for the entire matrix.

For Session names and the Herdr sidebar, run `app_acceptance.py --replacement
--session-names` with the same binary arguments as above. It creates an unnamed
Session through the UI with hooks disabled, supplies a structured synthetic
response to real Codex's automatic title-generation request,
checks one-time capture and Unicode pane/tab labels, checks a later Codex title
does not overwrite the captured name, and exercises explicit Harbor rename.
It asserts display naming did not create a Conversation reference. The original
`/rename`-first fixture missed the no-hook failure found during user testing.
The fixture retains an ANSI sidebar capture and checks native style settings.
For an unpackaged executable, supply `--codex-auth NATIVE_CODEX_AUTH`; packaged
runs copy the adjacent bundled executable into the fixture. This gate passed
on Linux amd64 on 2026-11-16 with Codex 0.169.0 and Herdr 0.9.5.

`TestProfileContract*`, `TestProfileDefault*`, `TestProfileUpgrade*`,
`TestProfilePage*` and `TestProfileMagpie*` cover persisted identity, default
initialization, explicit legacy review, A/B scope, cancellation/coordination,
stale results, UI selection/editing, invalid roots and Magpie capability/env guards.
The UI fake uses in-memory Profile metadata; it never resolves live paths.

Set `HARBOR_TEST_CODEX_BIN` to copied native Codex 0.169.0 to run
`TestProfileNativeCodex*` under the isolated test environment. These tests use
local synthetic upstreams and synthetic independently authored API keys. They
check actual A/B requests, history/SQLite, exact recovery, B remaining unchanged
and separately owned App Servers. They do not pass the real-account gate.

Use the maintained runner for the real UI/runtime Profile journey:

```
python3 bridge/tests/app_acceptance.py --replacement --profiles --onboarding managed --binary HARBOR --herdr HERDR --magpie MAGPIE_PROFILE_CANDIDATE --codex NATIVE_CODEX
```

This creates Profiles through the UI, explicitly authors fixture-only settings
with signed-out API connections, runs simultaneous A/B and same-Profile Conversations, and resumes
A's fixed Conversation/instance while B is selected. It then restarts the workspace and gateway and recovers both A and B while the other Profile is selected. The runner preserves all
pre-existing services. Discovery port 3425 must be exclusively available; exit 77
is a blocked gate. The fixture's credentials/settings are synthetic setup, not
product import behavior. Run the existing full-state/model-picker gates separately
and repeat the combined #19/#20 account journeys on integration. Real official
login, independently authorized saved grants and native macOS evidence remain
separate mandatory acceptance records.

## Profile account integration candidate

`TestProfileAccountsScopeAndSignedOut` uses the real Profile resolver and lock
with controlled native tool subprocesses. It checks scoped selection, explicit
sign-out retaining the list, failed login rereading, A/B separation, uncertain
live activity and the production not-ready gate. This does not enable or prove
the missing Profile runtime. Stale Profile UI results and default account-page
keyboard/mouse controls have separate tests. Experimental refresh RPC tests
remain separate.

Package installation acceptance includes codex-auth version execution without
installing it on PATH, archive allowlisting and saved-account-data retention on
uninstall. Native Linux package checks do not establish macOS/keychain behavior.

The remaining detailed #20 validation matrix includes normal UI signed-out
managed/external Magpie turns and exact recovery, login/select/cancel against
real codex-auth, A/B real request identities, remote execution and native macOS
package journeys. Use the maintained application/package runner after integrating
#19; controlled-tool tests are not substitutes for these gates. Real disposable
account authorization and later model requests must be tested separately.

For the saved-reference recovery dead end and focus regression, run
`app_acceptance.py --replacement --profiles --recovery-review --onboarding managed`
with the same executable arguments as the Profile gate. It captures a real first
Codex turn, directly restarts Harbor, and replays only the final observation status
as unavailable in fixture-owned state. It verifies one Resume action records the
exact saved reference, a real gateway failure preserves Resume focus, and same-page
retry restores the original Magpie conversation and first-turn context. No user
state is modified; this remains synthetic-provider evidence.


`terminal_lifecycle.py --test-binary TEST_BINARY` is the isolated Linux PTY gate.
Build its opt-in production-model probe with `go test -c -o TEST_BINARY` in the
same isolated test environment. It uses a fake service, checks real shell job
control and retained search input, and compares termios before/after normal exit,
Ctrl+C, an injected model panic, and Ctrl+Z/fg. It also checks alternate-screen,
cursor, mouse and bracketed-paste cleanup sequences. This gate exercises the
production model and Bubble Tea options, not real client recovery.

`remote_workspace.py --identity-file KEY` can use a disposable SSH key for an
isolated transport fixture. Authentication remains native OpenSSH; the key is not
copied into Harbor state. Record whether the server was loopback or a separate
machine; loopback disconnect evidence does not establish physical sleep or a
cross-machine network failure.
