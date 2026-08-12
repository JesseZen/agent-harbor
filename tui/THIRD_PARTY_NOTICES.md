# Third-Party Notices

Agent Harbor TUI is a public source distribution. It contains no source code
from the proprietary `agent-harbor-core` binary.

## Agent Deck

- Project: Agent Deck
- Source: https://github.com/asheshgoplani/agent-deck
- Version: v1.9.73
- Commit: `2eedbc1ff60bcc23dd3f97848517b571e5f74ab9`
- Copyright: Copyright (c) 2025 Ashesh Goplani
- License: MIT; the complete license text is in [LICENSE](LICENSE).

This repository is a direct source fork. Its complete `internal/ui.Home`,
`Home.Update`, and `Home.View` implementation remains the Sessions interface.

## lazygit

- Project: lazygit
- Source: https://github.com/jesseduffield/lazygit
- Version: v0.63.1
- Commit: `aafe61082e7ed383d318fd40e48f85645e6afc7b`
- Copyright: Copyright (c) 2018 Jesse Duffield
- License: MIT; the exact upstream text is in
  [third_party/lazygit/LICENSE](third_party/lazygit/LICENSE).
- Incorporated module: the complete temporary command-menu behavior from
  `pkg/gui/menu_panel.go`, `pkg/gui/context/menu_context.go`, and
  `pkg/gui/controllers/menu_controller.go`, ported to Bubble Tea in
  `internal/upstream/lazygit/commandmenu`.

## K9s

- Project: K9s
- Source: https://github.com/derailed/k9s
- Version: v0.51.0
- Commit: `558caafe7ba067467de46b320cc22ef11fef9c34`
- Copyright: Copyright Authors of K9s
- License: Apache License 2.0; the exact upstream text is in
  [third_party/k9s/LICENSE](third_party/k9s/LICENSE).
- Incorporated modules: the complete selectable resource-table behavior from
  `internal/ui/table.go`, `internal/ui/select_table.go`, and
  `internal/ui/table_helper.go`, ported to Bubble Tea in
  `internal/upstream/k9s/resourceview`.

The lazygit and K9s ports are presentation-only. They do not include or start
git, Kubernetes, process, storage, watcher, or network runtime owners.
