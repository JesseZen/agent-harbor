# K9s resource-view source map

This package is an attributed Bubble Tea port of K9s v0.51.0 at commit
`558caafe7ba067467de46b320cc22ef11fef9c34`.

The selected modules form K9s' complete selectable resource-table unit:

- `internal/ui/table.go`: table cursor, selected column, sorting, responsive
  wide-column behavior and refresh state;
- `internal/ui/select_table.go`: stable row identity, selected row, marks and
  selection retention;
- `internal/ui/table_helper.go`: cell formatting and sort indicators.

The port replaces tview/tcell and Kubernetes models with a small Bubble Tea
row contract. Cursor retention, marks, filtering, selected-column sorting,
responsive priority columns, and footer/status rendering remain together. No
Kubernetes client, watcher, process, configuration, or storage owner is
included.

Visual chrome follows a k9s-style box (`┌─┐│└─┘`) with terminal-default
background (no solid black page fill), cyan header fill, selected-row
`TokenSelected` highlight, and Session-matching accent key chips in the footer.

License: Apache License 2.0. See `third_party/k9s/LICENSE` and
`THIRD_PARTY_NOTICES.md` at the repository root.
