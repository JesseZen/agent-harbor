# commandmenu source map

## Interaction (lazygit)

This package's filtering / navigation / selection behavior is an attributed
Bubble Tea port of lazygit v0.63.1 at commit
`aafe61082e7ed383d318fd40e48f85645e6afc7b`.

- `pkg/gui/menu_panel.go`
- `pkg/gui/context/menu_context.go`
- `pkg/gui/controllers/menu_controller.go`

License: MIT. See `third_party/lazygit/LICENSE` and
`THIRD_PARTY_NOTICES.md` at the repository root.

## Visual chrome + pointer interaction (Grok Build)

The Spotlight / Commands panel chrome and mouse interaction are a Go port of
SpaceXAI Grok Build's CommandPalette (Apache-2.0):

- `crates/codegen/xai-grok-pager/src/views/modal_window.rs`
  — square border, bold title on top border, `[x]` close, footer shortcuts,
  `compute_modal_dims`, `handle_modal_mouse` (close / outside click)
- `crates/codegen/xai-grok-pager/src/views/picker.rs`
  — ` search: ` bar, section headers (` label ` + `─`), `◆` leaf rows,
  `bg_visual` selection, hit areas, wheel scroll (±3), row click/hover
- `crates/codegen/xai-grok-pager/src/app/modals.rs`
  — CommandPalette composition (`title: "Commands"`, shortcut labels, sizing)
- `crates/codegen/xai-grok-pager-render/src/theme/groknight.rs`
  — palette tokens (`bg_base`, `bg_visual`, `gray_dim`, scrollbar colors)
- `crates/codegen/xai-grok-pager-render/src/render/scrollbar.rs`
  — overflow scrollbar thumb/track

Upstream: https://github.com/xai-org/grok-build  
License: Apache-2.0, Copyright 2023-2026 SpaceXAI.

Ratatui cell rendering is expressed with Lip Gloss string styles; keyboard
filtering/navigation remains the lazygit-derived Bubble Tea model above.
