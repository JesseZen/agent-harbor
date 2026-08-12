package commandmenu

import (
	"github.com/asheshgoplani/agent-deck/internal/ui"
	"github.com/charmbracelet/lipgloss"
)

// Theme-aware Spotlight chrome.
//
// GrokNight used near-black (#141414) which fights typical terminal / AH
// theme backgrounds. Charm / OpenCode guidance is to follow the active
// palette (or leave bg unset for terminal-default). We paint with the live
// AH UI theme so Spotlight tracks :theme and matches the rest of the TUI.
//
// Layout tokens (search label, ◆, [x], footer) stay Grok-shaped; only the
// color tokens are remapped.

func chromeBGBase() lipgloss.Color   { return ui.ColorBg }      // panel fill ≈ page / terminal
func chromeBGVisual() lipgloss.Color { return ui.ColorSurface } // selection / hover lift
func chromeGrayDim() lipgloss.Color  { return ui.ColorBorder }
func chromeGray() lipgloss.Color     { return ui.ColorComment }
func chromeText() lipgloss.Color     { return ui.ColorText }
func chromeTextMuted() lipgloss.Color {
	return ui.ColorTextDim
}
func chromeScrollBG() lipgloss.Color { return ui.ColorBg }
func chromeScrollFG() lipgloss.Color { return ui.ColorBorder }
