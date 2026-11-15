package main

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

// uiAction is the single source for a control's label, key, availability and
// help. IDs belong to the page; object IDs must remain stable across refresh.
type uiAction struct {
	ID, Label, Key, Disabled string
	Primary                  bool
}
type uiRect struct {
	X, Y, W, H int
	ID         string
}

func (r uiRect) contains(x, y int) bool { return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H }

type uiPage struct {
	Width, Height int
	Lines         []string
	Hits          []uiRect
}

func newUIPage(w, h int) uiPage { return uiPage{Width: max(1, w), Height: max(1, h)} }
func (p *uiPage) line(s string) {
	p.Lines = append(p.Lines, ansi.Truncate(s, max(1, p.Width-2), "…"))
}
func (p *uiPage) hit(id string, x, y, w, h int) {
	if w > 0 && h > 0 && y >= 0 && y+h <= p.Height {
		p.Hits = append(p.Hits, uiRect{x, y, w, h, id})
	}
}
func (p uiPage) target(x, y int) string {
	for _, r := range p.Hits {
		if r.contains(x, y) {
			return r.ID
		}
	}
	return ""
}
func (p uiPage) view() string {
	lines := append([]string(nil), p.Lines...)
	for len(lines) < p.Height {
		lines = append(lines, "")
	}
	if len(lines) > p.Height {
		lines = lines[:p.Height]
	}
	for i, s := range lines {
		s = " " + s
		s += strings.Repeat(" ", max(0, p.Width-ansi.StringWidth(s)))
		if !colorOff() {
			// Nested controls reset SGR backgrounds. Restore the page surface after
			// those resets so trailing cells never expose the host terminal theme.
			r, g, b, _ := lipgloss.Color(harborPalette["bg"]).RGBA()
			bg := fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r>>8, g>>8, b>>8)
			fr, fgc, fb, _ := lipgloss.Color(harborPalette["text"]).RGBA()
			foreground := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", fr>>8, fgc>>8, fb>>8)
			base := bg + foreground
			s = base + strings.NewReplacer("\x1b[0m", "\x1b[0m"+base, "\x1b[m", "\x1b[m"+base, "\x1b[49m", bg, "\x1b[39m", foreground).Replace(s) + "\x1b[0m"
		}
		lines[i] = s
	}

	return strings.Join(lines, "\n")
}
func (p *uiPage) button(a uiAction, focused bool) {
	text := " " + cleanUI(a.Label) + " "
	if focused {
		text = "›" + text + "‹"
	}
	role, bg := "key", "button"
	if a.Primary {
		role, bg = "ink", "brand"
	}
	if a.Disabled != "" {
		role, bg = "muted", "button"
	}
	y := len(p.Lines)
	p.line(paint(fg(role).Background(lipgloss.Color(harborPalette[bg])), text))
	if a.Disabled == "" {
		p.hit(a.ID, 1, y, min(ansi.StringWidth(text), p.Width-2), 1)
	}
}
func (p *uiPage) field(id, icon, label, value string, focused, editable bool) {
	y := len(p.Lines)
	mark := "  "
	if focused {
		mark = "› "
	}
	prefix := mark + icon + strings.Repeat(" ", max(1, 4-ansi.StringWidth(icon)))
	col := min(24, max(13, p.Width/3))
	key := prefix + label
	key += strings.Repeat(" ", max(2, col-ansi.StringWidth(key)))
	suffix := ""
	if editable {
		suffix = " ›"
	}
	available := max(1, p.Width-2-ansi.StringWidth(key)-ansi.StringWidth(suffix))
	value = ansi.Truncate(value, available, "…")
	value += strings.Repeat(" ", max(0, available-ansi.StringWidth(value)))
	base := lipgloss.NewStyle().Background(lipgloss.Color(harborPalette["bg"]))
	if focused {
		base = base.Background(lipgloss.Color(harborPalette["selBg"]))
	}
	p.line(paint(base.Foreground(lipgloss.Color(harborPalette["text"])), key) + paint(base.Foreground(lipgloss.Color(harborPalette["value"])), value+suffix))
	p.hit(id, 1, y, p.Width-2, 1)
}
func (p *uiPage) search(value, placeholder string, focused bool) {
	y := len(p.Lines)
	w := max(5, p.Width-2)
	role := "border"
	if focused {
		role = "section"
	}
	p.line(paint(fg(role), "╭"+strings.Repeat("─", w-2)+"╮"))
	text := value
	if text == "" {
		text = placeholder
	}
	if focused {
		text += "▏"
	}
	text = "🔎 " + ansi.Truncate(cleanUI(text), max(1, w-6), "…")
	p.line(paint(fg(role), "│") + paint(fg("value"), text+strings.Repeat(" ", max(0, w-2-ansi.StringWidth(text)))) + paint(fg(role), "│"))
	p.line(paint(fg(role), "╰"+strings.Repeat("─", w-2)+"╯"))
	p.hit("search", 1, y, w, 3)
}
func (p *uiPage) tabs(current, hover, focus string, accounts bool) {
	tabs := []uiAction{{ID: "sessions", Label: "🚢 Sessions"}, {ID: "magpie", Label: "🐦 Connections"}}
	if accounts {
		tabs = append(tabs, uiAction{ID: "accounts", Label: "👤 Accounts"})
	}
	tabs = append(tabs, uiAction{ID: "settings", Label: "⚙ Settings"})
	x := 1
	y := len(p.Lines)
	line := ""
	for _, a := range tabs {
		text := " " + a.Label + " "
		if focus == a.ID {
			text = "›" + a.Label + "‹"
		}
		width := ansi.StringWidth(text)
		if x+width > p.Width-1 {
			p.line(line)
			line = ""
			x = 1
			y = len(p.Lines)
		}
		st := fg("key")
		if current == a.ID {
			st = fg("ink").Background(lipgloss.Color(harborPalette["section"]))
		} else if hover == a.ID {
			st = st.Underline(true)
		}
		line += paint(st, text)
		p.hit("nav:"+a.ID, x, y, width, 1)
		x += width
	}
	p.line(line)
}
func actionHints(actions []uiAction) string {
	out := []string{}
	for _, a := range actions {
		if a.Key != "" {
			s := a.Key + " " + a.Label
			if a.Disabled != "" {
				s += " (" + a.Disabled + ")"
			}
			out = append(out, s)
		}
	}
	return strings.Join(out, "  ")
}

// buttons lays out a group in a row when it fits, otherwise stacks it. Both
// compositions register exactly the cells painted for each control.
func (p *uiPage) buttons(actions []uiAction, focused string) {
	needed := 0
	for _, a := range actions {
		needed += ansi.StringWidth(a.Label) + 6
	}
	if needed > p.Width-2 {
		for _, a := range actions {
			p.button(a, focused == a.ID)
		}
		return
	}
	line := ""
	x := 1
	y := len(p.Lines)
	for _, a := range actions {
		text := " " + a.Label + " "
		if focused == a.ID {
			text = "›" + text + "‹"
		} else {
			text = " " + text + " "
		}
		role, bg := "key", "button"
		if a.Primary {
			role, bg = "ink", "brand"
		}
		if a.Disabled != "" {
			role, bg = "muted", "button"
		}
		width := ansi.StringWidth(text)
		line += paint(fg(role).Background(lipgloss.Color(harborPalette[bg])), text) + "  "
		if a.Disabled == "" {
			p.hit(a.ID, x, y, width, 1)
		}
		x += width + 2
	}
	p.line(line)
}
