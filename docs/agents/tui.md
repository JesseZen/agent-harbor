# TUI consistency contract

Apply this contract to new features, screens, dialogs and changes to shared controls. The current implementation status belongs to [the flow audit](../ui-flow-audit.md); the accepted visual rules and historical comparisons belong to [the visual review](../ui-visual-review.md). Read their latest status sections before treating an old recommendation or screenshot as the current design.

## Shared visual language

- Preserve Harbor's dark gray surface, semantic palette, field alignment, selection background and established emoji. New pages must look and behave like adjacent Harbor pages. A feature-specific theme or independently styled page requires an explicit design change from the user.
- Reuse `uiPage`, fields, search/edit boxes, shortcut formatting, footer docking and scroll rails in `bridge/app_ui.go`. Extend the shared component when a class of pages needs a change; avoid a parallel implementation made from page-specific spacing and strings.
- Keep navigation tabs, fields and list options. Harbor actions use keyboard shortcuts in a fixed bottom bar. Keep key and action together and limit the footer to two lines, with `...` when more hints are available. Hidden shortcuts remain available by key. Calculate content capacity from the displayed footer.
- Search fields use a muted idle border, pale purple focus border, `⌕`, an in-field gray placeholder and the real input cursor. Typing replaces the placeholder; clearing restores it. Use the shared `⎋` and `↵` display formatting while dispatching the actual keys.
- Put icons where they identify navigation or meaningful data. The Profile emoji appears only in its top-level tab, without repeating it in the page title or Profile fields. At narrow widths, keep all tab targets and expand the current or focused tab's text.
- In selectable lists, show the keyboard cursor with `>` and the accent foreground. Hover gives the row a background tint and preserves its foreground. If the list has an applied selection, give its main label the selection foreground even under the cursor. Keep status and measurement colors readable in all states. A check mark can identify the applied selection without color. Click an unfocused row to move the cursor. Click the current row to run the same action as Enter.
- Use a proportional right-edge scroll rail only for overflowing content. Reserve its cell without losing text; keep it out of the footer and clamp offsets on content and size changes.

## Content and interaction

- Give the body a clear hierarchy: object fields, list content and current state. Empty states use one muted line. Put key instructions in the footer, detailed paths and technical evidence in Details, and general usage in Help. Keep consequences that materially affect the current decision, including destructive actions and cross-provider disclosure.
- Show one explanation for a condition. Pressing an unavailable action must not append a second copy of an existing empty state. Success is ordinary feedback; an error should name the actual problem. Scope messages to the task while preserving error evidence during Details/Help round trips.
- On the main tabs, Left/Right and h/l change pages. Up/Down and k/j move the active list, field or document. Tab and Shift+Tab do not move focus. Text fields own ordinary characters. Show only actions that apply to the current input/focus state. Keep search Esc layers, visible Enter targets, caller return context and safe confirmation defaults consistent with sibling pages.
- Derive dangerous confirmations from product semantics. Review Profile, Conversation and connection requirements before changing recovery/setup or cancellation; a visual cleanup must preserve their identity boundaries.

## Regression and completion

1. Inspect the changed page and its closest existing siblings before editing. For a shared control, identify all its callers. Use the `tui-design` skill and its Go, visual and interaction references when available.
2. Verify state transitions with relevant tests. Inspect rendered pages for both the new feature and existing callers: a new feature is incomplete if it regresses their palette, spacing, focus, footer or key behavior.
3. Follow [the acceptance guide](../../bridge/tests/README.md) for isolated runtime checks. Exercise real Herdr/tmux at outer 160×45, 100×30, 80×24 and 60 columns, using the actual popup dimensions. Check short/long, empty/error, editing/cancel/back and resize states as applicable.
4. Retain sanitized ANSI evidence and view it with the repository's Unicode 15 renderer. Label terminal re-renderings, mockups and native pixel screenshots accurately. A standalone model render cannot establish real-client behavior.
5. Update both audit status sections with implemented, partial, unimplemented and unverified items. A passing batch or a final capture is not completion of the whole UI. Report remaining implementation separately from missing runtime/platform evidence; call the whole UI complete only after reconciling every remaining item in scope.
