# Harbor UI contract

The migration starts with Settings. Legacy pages remain supported until each
page is migrated and verified. Do not infer semantics from a formatted string
in new pages.

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
