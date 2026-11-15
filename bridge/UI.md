# Harbor UI contract

All active popup routes use semantic pages. Do not infer semantics from a
formatted string. UI actions also supply contextual help, including disabled
reasons. Documents share a scrollable body and fixed action area.

`app_ui.go` owns the cell layout. A page builds a `uiPage` with its actual inner
viewport dimensions. Each control appends rendered rows and hit rectangles in
the same operation. Rectangles use zero-based terminal cells, including the
one-cell page margin. Mouse handling calls `target` on the newly built layout,
so resize and filtering cannot use old coordinates. Page-specific IDs refer to
stable objects, never a cursor index captured before a refresh.

`uiAction` provides the control ID, label, key, primary emphasis and disabled
reason. The same definition supplies buttons and contextual help. Input editors
consume ordinary characters before page actions. Lists select on a single
click; explicit actions execute. Confirmation pages must never reuse a pending
list double-click. Busy pages suppress duplicate operations.

The palette lives in `app_style.go`: charcoal background, bright field labels,
gray values, quieter explanations, gray keys, sea green primary buttons,
lavender current tabs and blue keyboard focus. Focus uses a background and
indicator without reversing label/value emphasis. Current, hover and keyboard
focus have independent inputs. An unselected tab's hover adds only underline.
NO_COLOR preserves all text and indicators. Terminal font, line spacing and
mouse motion availability are runtime capabilities, not application promises.

Settings keeps its values in one draft independent of the search result list.
Accepting a field and restoring defaults do not persist it. Save uses the
existing settings transaction and retains the draft on failure. Search Esc
clears first, then leaves search; editor Esc cancels only that edit. Navigation
with an unsaved settings draft requires Save or Cancel first. Successful Save
updates the persisted baseline. Herdr's separate settings and all shortcut
scope restrictions remain in force.

Global shell, navigation dispatch, theme, focus/mouse infrastructure and shared
contracts belong to the integration owner. Page workers submit minimal wiring
requests for those modules rather than editing shared entry points concurrently.

## Reachable page inventory

| Entry / state | Shared page owner |
| --- | --- |
| First run, readiness, choice review, onboarding help / waiting | Secondary pages (09) |
| Sessions, empty/search states, contextual More menu | Sessions (02) |
| Settings, shortcut editor, search, diagnostics | Settings (01) |
| Object confirmation, recovery/reference review, evidence | Review (03) |
| Preparing a new session | Secondary pages (09) |
| New form, client picker, local name edit | Creation (04) |
| Project search, known/browse/recursive scopes, path/preview | Directory (05) |
| Native/Magpie choice, instance/catalog loading, selection, scoped disclosure | Connection selector (06) |
| Selected connection, bind, details, confirmations, managed actions, waiting | Connections (07) |
| Experimental account menu, external action waiting, per-session result | Accounts (08) |
| Session details, contextual help, rename/filter, history explanation, error/retry, waiting | Secondary pages (09) |

Magpie management, codex-auth and client terminals are external interfaces.
Harbor releases their terminal and restores its own mouse reporting on return.
They are not Harbor page layouts. The experimental account switch stays hidden
unless enabled; synthetic checks do not prove live ChatGPT account switching.

Normal creation enters the form after the guarded environment load. Historical
model tests can construct sequential creation states; their plain summaries use
the same semantic document shell. The old frame painter, string-based style
guessing and independently reconstructed mouse rectangles have been removed.

Printable characters belong to the focused input. Esc clears a nonempty search,
then leaves it, then cancels the local page. Tab and Shift+Tab cycle actual
controls. Help restores its parent control and scroll position. A secondary
page returning to Sessions restores list focus; a stale editor action cannot
consume the next Enter. Ctrl+C closes Harbor and Ctrl+G dismisses feedback.
