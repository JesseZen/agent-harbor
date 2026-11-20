# Workspace shortcuts

Harbor Settings lists the Herdr prefix and the bindings for Harbor, Magpie,
Settings and New session. Each action accepts multiple bindings. A workspace
can keep `prefix+a` and add `ctrl+g` for the same Harbor popup. Every configured
prefix activates the same prefix bindings. New session opens the existing form
using the current workspace and Profile context. The form still requires its
normal confirmation before creating a session.

## Defaults and saved preferences

The defaults are `ctrl+b` for the prefix, `prefix+a` for Harbor, `prefix+m`
for Magpie and `prefix+,` for Settings. New session starts unbound. Existing
workspaces retain their saved shortcuts and the default prefix on upgrade.
Adding `ctrl+n` is an explicit choice because direct shortcuts consume input
that would otherwise reach the focused terminal.

## Editing

Enter opens a binding list for the selected action. Choose a binding and press
Enter to edit it, use the Add shortcut row to append one, or press Delete to
remove it. Accepting an edit updates the draft. Esc cancels the active input
or returns to the overview. Ctrl+S saves the whole draft and reloads the owned
Herdr workspace. Ctrl+R restores defaults in the draft.

Use `ctrl+letter`, `alt+letter`, `ctrl+alt+letter` or `f1` through `f12` for
direct bindings and Herdr prefixes. Actions also accept `prefix+` followed by
an available letter or one of `, . ; /`.

The overview uses the existing fields and fixed footer. At narrow sizes it keeps
one column. Long binding lists use the shared scroll rail and keep the focused
row visible. The binding editor uses the shared input box. It shows collisions
with the action or Herdr binding already using the key. A failed save keeps the
draft and restores the preceding configuration. The CLI Settings entry remains
available if the popup shortcut has been reassigned.

## Acceptance on CNB

Run the Go suite and the isolated real Herdr/tmux gate on CNB. Exercise upgrade
from scalar preferences, alternative bindings on one action, both prefixes,
direct Magpie and New session entry, collisions, cancel, reset, live reload and
restart. Cancel the form and confirm that no session was created. Capture Settings,
its binding editor and the Sessions, Magpie and New session popups at outer
160×45, 100×30, 80×24 and 60×24. Review them with the repository Unicode 15
renderer and record the results in the flow audit and visual review.
