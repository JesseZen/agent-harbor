# Product acceptance gates

## Sessions Pin, Archive and group folding

`python3 -B bridge/tests/sessions_organize_popup.py --binary HARBOR --herdr HERDR`
runs an isolated Herdr/tmux popup with 110+ sessions (9 verified live Active runs,
100 Stopped records, and an External agent) under a disposable Profile and synthetic
client history file. It verifies pure-keyboard `j`/`k` crossing across collapsed groups,
live resurfacing when a folded `Idle` session transitions to `working` (with Herdr
process group unchanged), in-place `▸ Show N more` / `▾ Show less` expand and column
alignment across outer `160×45`, `100×30`, `80×24`, and `60×24`, deep collapse via `z`
returning focus to the action row, search matching folded records, Pin persistence,
Archive refusal on `Idle` sessions, Archive/Unarchive preserving Conversation/Profile/Magpie
connection and Pin, and Remove record from Archived without modifying client history.

## Sessions preview prerequisite

Run `python3 -B bridge/tests/pane_read_probe.py --herdr BUNDLED_HERDR --codex NATIVE_CODEX`
with tmux and Vim installed. The probe uses disposable homes, Unix sockets, a project,
an unserved loopback provider address and synthetic text. It prints measurements and
deletes its temporary files on exit. The final line is `PASS` for the current product
meaning: `pane.read` returns the selected pane's current screen, even if another
foreground briefly ran in that same pane. An assertion or timeout means the probe
itself needs investigation.

Tested on 2026-11-23 with Harbor base `a80e1d2`, macOS 27.0.1 arm64, the packaged
Herdr 0.9.5, native Codex 0.169.0 and tmux 3.6b. Outer size was 160×45 and the
source Codex PTY was 65×42. This run measured the Herdr panes directly. Harbor popup
dimensions, responsive behavior, motion, and Linux/remote preview acceptance are covered
by the popup fixture below; Linux and remote real-client evidence remain pending.

| Check | Observed |
| --- | --- |
| Targeted read | `pane.read` with explicit `pane_id`, `source: visible`, `format: ansi`, `strip_ansi: false`. `result.read` contained `workspace_id`, `tab_id`, `pane_id`, `source`, `format`, `text`, `truncated` and `revision` (0). |
| Correct pane | The focused shell returned red `SHELL-PRIMARY`. The background Codex returned `UNSENT-PREVIEW-DRAFT` and six distinct SGR sequences. Full-screen Vim returned its styled fixture line. |
| Read side effects | The focused pane, selected target's terminal, PTY size, foreground process group and Codex PID matched before and after. The composer still contained its draft on returning. A public synthetic working→idle report on the background tab produced `done` in `agent.list` both before and after the read. |
| Scrolled shell | At offset 50, tmux showed rows `ROW-090` through `ROW-131`. The RPC returned those same rows in order and the offset stayed at 50. |
| Boundary cases | A cleared screen was empty. The dense color fixture returned 102,962 ANSI bytes with `truncated: false`, exceeding Harbor's 64 KiB envelope budget. Closing that pane produced `pane_not_found`. |

The following three samples show the foreground change that blocks a strict run-specific
preview. The probe keeps the **same pane selected**. A fixture actor suspends Codex,
runs another job in its shell, then returns to Codex with `fg`. These are synthetic
content markers from the source screens. The only snapshot under test is the middle one.

| Sample | Foreground | Screen marker |
| --- | --- | --- |
| Before | Codex A | `UNSENT-PREVIEW-DRAFT` |
| `pane.read` | Shell job B | `OTHER-FOREGROUND` |
| After | Original Codex A | `UNSENT-PREVIEW-DRAFT` |

In one run the interval took 269 ms. Both outer observations agreed on socket inode,
workspace, tab, pane, terminal, client, Codex PID, foreground group, activity, focus,
scroll, PTY size and pane revision. The conversation reference was absent throughout.
The middle `result.read` carried only the pane coordinates and `revision: 0` for
attribution. A selection generation can drop late UI results but stays the same
through this foreground change. Herdr deliberately retains a suspended agent during
an intervening shell command. [Herdr suspend/resume test](https://github.com/herdrdev/herdr/blob/ee5bfb5041bb3e4a49d591c72308aae3c81ecdde/src/pane.rs#L5722)

The foreground switch is an accepted boundary case: the preview shows the selected pane
at read time, not a guaranteed frame from one process. Harbor still verifies the pane,
socket, terminal, run/profile binding and discovery before publishing a result; it does
not add a content-revision gate or infer recovery identity from the captured text.

## Sessions live preview

`python3 -B bridge/tests/sessions_live_preview.py` runs an isolated Herdr 0.9.5/tmux
popup with synthetic ANSI panes, an external real Codex composer, and a transparent RPC
proxy. It checks rapid A→B→A selection, pane reuse/lifecycle errors, stopped and
disconnected placeholders, Working animation versus RPC count, five outer sizes and
zero preview RPCs in narrow layouts. `app_acceptance.py --sessions-preview` additionally
checks a real Native and real Magpie Codex turn through loopback providers. Both fixtures
save sanitized ANSI frames for `render_terminal.cjs`; the current macOS run produced
Unicode 15 evidence and measured popup interiors as `160×45→104×33`, `100×30→56×21`,
`80×24→40×16`, `60×24→45×15`, and a short `160×20→104×13`.
The popup fixture also checks `o` against a disposable Git project: the collapsed
summary keeps the project, branch, CLI, connection and final model segment; the full
path and route appear when expanded, with Profile on the same row when it fits and
the diagnosis last. The same pane remains in the preview.

## Live popup upgrade

Build the candidate in an isolated Go environment. Run the two package paths
sequentially with the same old dev.8 package executables and native Codex.

```sh
for method in script homebrew; do
  python3 bridge/tests/popup_upgrade.py --method "$method" \
    --old-libexec /opt/homebrew/opt/agent-harbor/libexec \
    --binary CANDIDATE_HARBOR --codex NATIVE_CODEX || break
done
```

The script gate serves checksum-verified release archives on a temporary port
and runs the production installer and `ah update`. It injects a partial reload
acknowledgment and blocks the plugin registry write, verifying rollback after
each fault. The default Homebrew gate reproduces the formula's Cellar and `opt`
layout with real dev.8 binaries and removes the fixture's old keg. To run the
actual `brew install`, `brew upgrade` and `brew cleanup` commands, also pass
`--brew FIXTURE_PREFIX/bin/brew`. Use a fresh, working Homebrew clone in a
writable directory outside `/tmp` on macOS. Give its prefix a file named
`.harbor-brew-fixture` containing `agent-harbor-brew-fixture-v1` and a final
newline. The fixture checks the prefix and uses a local tap generated from the
release formula template and two checksum-verified loopback archives.

Both package paths exercise the old popup, new popup,
custom shortcuts, unchanged Herdr/Codex PIDs, pane and unsent draft, and a new
managed Codex launch. The runner reserves `127.0.0.1:3425` exclusively and exits
77 if it cannot bind. Run it apart from other fixtures using port 3425. It uses
fixture-owned tmux, HOME, XDG and client state, and stops only its own workspace.

The primary acceptance baseline is the current upstream Codex client release.
Results from older clients are historical evidence, not a pass for a newer
release. The joined connection additionally requires the local Magpie
caller-only candidate described in [the integration guide](../../docs/magpie-caller-integration.md).
Report the exact client and dependency versions tested, and distinguish these
gates from upstream acceptance or release.

## Immediate Codex primary selection

Build the [Codex and Herdr candidates](../../docs/herdr-integration.md#immediate-codex-primary-selection-candidate)
and point the fixture at the three native binaries.

```sh
python3 bridge/tests/primary_selection_real.py --binary HARBOR --herdr HERDR_CANDIDATE --codex CODEX_CANDIDATE
```

The fixture runs real interactive clients in isolated homes and processes with
a loopback Responses provider. It saves A, seeds B under the same Profile,
then reopens A through the Harbor popup. It cancels one `/resume`, selects B
with the popup closed and checks the durable reference before restarting the
workspace. It recovers B through Harbor and checks A's history, the original
Profile and Connection Intent, and that neither switch nor recovery sent a
model request. `--quick-exit` submits `/exit` as soon as B is visible and
checks that exit reports leave B in place. `--consecutive` selects C after B in
the same run, recovers C and selects B in a new run while retaining evidence
from the previous run. `--subagent` creates a child through the fixture
provider after recovery and opens its actual thread through `/subagents`.
Use `--stock` with stock Codex and Herdr to check that the visible selection of
B leaves the saved reference A. Receiver tests cover duplicated, out-of-order
and mismatched events, pending startup identity, and passive observations. The
real gate also verifies Herdr rejects a report sent from a process outside the
pane's foreground group.

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

## Accounts popup

```sh
python3 bridge/tests/accounts_popup.py --binary HARBOR --herdr HERDR \
  --codex NATIVE_CODEX --codex-auth CODEX_AUTH
```

Uses Herdr and tmux with isolated homes and the pinned codex-auth binary.
The fixture imports two synthetic identities and tests A → Not signed in → B,
cancellation, and account details at 160×45, 100×30, 80×24 and 60×24. This
checks stored login selection and retained saved accounts. The fixture retains
sanitized ANSI captures.

Add `--live-switch native` for a real Codex window and a loopback Responses
provider, or `--live-switch magpie --magpie MAGPIE` for a signed-in Magpie
window and a loopback Chat Completions provider. These runs require Python
3.14+ for compressed Codex requests. Both use synthetic accounts, Herdr and
the default owned App Server. The fixture pauses a turn, checks that the
Profile still uses A, cancels once, then finishes the turn and verifies B
without changing the pane, processes, Conversation, or model. Native verifies
the next model request uses B; joined Magpie verifies its provider credential
stays the same and the official quota request uses B. These checks do not
replace validation with two real authorized accounts.

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

## Workspace shortcuts

```sh
python3 bridge/tests/settings_popup.py --binary HARBOR --herdr HERDR --codex NATIVE_CODEX
```

This gate uses isolated Herdr 0.9.5, copied native Codex with updates disabled
and an owned tmux server. It checks scalar preferences, mouse and keyboard
editing, conflicts, two active prefixes, alternate and direct popup bindings,
live reload, restart and restore defaults. The direct creation binding opens
the form and cancellation leaves the workspace without a new session.
The runner keeps ANSI captures at outer 160×45, 100×30, 80×24 and 60×24.
Render the 60-column captures separately with `--rows 24`.
Settings transaction failure and recovery tests live in `settings_test.go`.

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

Native sessions exercise the owned App Server in this fixture by default.
`TestAccountRefreshVerifiesIdentityAndReadsOfficialQuota` checks the RPC
sequence, including failed refresh and identity mismatch. Live account
evidence must be reported separately.

The owned App Server recovered both native and caller-only Magpie
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

### Enter recovery (#21, decision 1)

Build the candidate and run the popup gate with the native Codex executable and
the Herdr query candidate. The fixture uses dynamic discovery and provider ports,
an owned tmux server, and a separate Profile home and history.

```sh
python3 bridge/tests/query_recovery_real.py --enter-recovery --width 60 --height 24 \
  --binary HARBOR --herdr HERDR --magpie MAGPIE --codex NATIVE_CODEX --work-root ISOLATED_DIR
```

Repeat with `--color` at 80×24, 100×30 and 160×45 for rendered comparison.
The keyboard path checks one live row and focus from another tab, cancellation
from the existing Stop process confirmation, the blocked
reason and View old terminal action when the restored shell has foreground work,
Esc before persistence, configuration changes before Enter, exact Native history
and Profile, and the original terminal ID after recovery. It replays legacy
conflict evidence with the last reported ID from a real Codex history. The
fixture verifies an unselected Enter writes nothing and choosing Last reported
recovers that history. The 160×45 run shrinks to 60×24 and back with a target
selected. Go tests cover either choice, two-click confirmation, incomplete
evidence, same-target retry, and occupancy across evidence sources.

For real Magpie failure and retry run the Profile gate with `--recovery-review
--dynamic-discovery`. It temporarily stops the fixture-owned gateway, then
retries from the same confirmation and checks the original Profile, instance,
conversation history and restored terminal ID.

Confirmation compares the bound Profile, scoped Codex configuration and
file-backed login, plus the recorded connection and available Herdr runtime.
Recovery checks the existing Codex hook setup in the bound Profile. If it needs
preparation, the popup shows what to repair and offers Recheck.
Codex's effective provider and keyring state, Magpie group membership, and
writers outside the discoverable workspace remain client or gateway concerns.
The popup can retry failures detected before terminal handoff. Recovery readiness
inside Codex is checked by exercising the resumed history in the fixture. A
client-side error after handoff appears in the new terminal.

Add `--model-picker` to exercise the real Magpie catalog in Codex, save a model
and reasoning effort for one Harbor session, make a temporary choice in a second
session, and check the saved choice and actual request after recovery. The
fixture uses synthetic providers and separate Codex and Magpie homes.

## Profile gates

### Codex options and defaults

`TestCodexOption*` and `TestCodexWindow*` cover binding-scoped memory, Apply and
Cancel, inherited values versus `0`, argument construction and scoped usage
evidence. `TestProfileNativeCodexDefaults*` additionally uses the native client
named by `HARBOR_TEST_CODEX_BIN` to verify Codex's actual config RPC, project
precedence, stale-write refusal, a separate Profile, and comment preservation.
`TestProfileNativeCodexOptionsTriggerPostTurnCompaction` verifies that a synthetic
6,000-token turn triggers compaction at 40%, but not at 70% or 0%.

Run `app_acceptance.py --replacement --codex-options` with the executable
arguments below. It allocates its own loopback discovery address, drives the
real Herdr popup and checks saved configuration, same-Conversation Native and
Magpie routing-group recovery, two different catalog capacities, and Codex's
reported permissions, reasoning and window. It also edits the stopped Native
session and verifies the changed window on the next launch. It checks that
`/new` in a newly launched Native window keeps the options while starting a
fresh Conversation. Native uses an owned App Server and API-only Magpie uses
the ordinary CLI. No real accounts or upstream providers are used.

`TestSessionClientForegroundHandoff` uses a Python 3 PTY to check that a
managed client starts in the foreground and that Harbor regains terminal
ownership after client exit or an exec failure. The pipe-backed launch is
covered by `TestManagedCreateAcceptsPipeInput`.

The integrated candidate received maintainer-reported macOS manual acceptance on
2026-11-16. See the [acceptance record](../../docs/issues-19-20-acceptance.md).
The gates below retain their specific evidence requirements; the report does
not supply per-scenario results for the entire matrix.

For Session names and the Herdr sidebar, run `app_acceptance.py --replacement
--session-names` with the same binary arguments as above. It creates an unnamed
Session through the UI with hooks disabled, supplies a structured synthetic
response to real Codex's automatic title-generation request,
checks initial and later Codex title synchronization, starts `/new` and verifies
its generated title reaches Harbor and Unicode pane/tab labels, then exercises
explicit Harbor rename.
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
sign-out retaining the list, failed authorization, A/B separation and uncertain
live activity. Other Go tests verify Herdr run ownership despite Conversation
conflicts, omission of API-only sessions, cancellation, stale UI results and
refresh failure.

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
