package main

import (
	"os"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// Harbor's console palette. Views produce plain, width-fitted text first;
// painting happens last and never changes a line's visible width, so layout
// and tests stay independent of colour support.
var harborPalette = map[string]string{
	"bg": "#282A33", "brand": "#80D8CF", "ink": "#22252E",
	"text": "#ECECF2", "label": "#9397A4", "value": "#9397A4",
	"muted": "#777D8C", "border": "#4B505F", "button": "#333742",
	"section": "#BEB7F2", "title": "#94B9EF", "key": "#B4B7C2",
	"ok": "#99C793", "warn": "#DAB77E", "bad": "#E3A09B",
	"idle": "#9397A4", "selBg": "#3A4050",
}

// keysMark prefixes body lines that list keyboard shortcuts ("s start  x stop").
const keysMark = ""

func colorOff() bool {
	_, off := os.LookupEnv("NO_COLOR")
	return off || os.Getenv("TERM") == "dumb" || lipgloss.ColorProfile() == termenv.Ascii
}

func fg(role string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(harborPalette[role])).Background(lipgloss.Color(harborPalette["bg"]))
}

func paint(style lipgloss.Style, text string) string {
	if colorOff() || text == "" {
		return text
	}
	return style.Render(text)
}

func kv(label, value string) string {
	return kvw(label, value, 16)
}

func kvw(label, value string, col int) string {
	return paint(fg("text"), label+strings.Repeat(" ", max(2, col-ansi.StringWidth(label)))) + paint(fg("value"), value)
}

func note(text string) string { return "• " + text }

func statusDot(status string) string {
	if status == "" {
		return ""
	}
	return "● " + status
}

func statusRole(word string) string {
	switch strings.ToLower(word) {
	case "running", "ready", "verified":
		return "ok"
	case "exited", "stopped", "not observed":
		return "idle"
	case "failed", "error":
		return "bad"
	}
	return "warn"
}

// paintKeys renders "k label  k label" or "k label · k label" hint strings.
var controlKeyPattern = regexp.MustCompile(`(?i)ctrl\+([a-z])`)

func controlKeyNotation(s string) string {
	return controlKeyPattern.ReplaceAllStringFunc(s, func(v string) string { return "^" + strings.ToUpper(v[5:]) })
}
func paintKeys(text string) string {
	parts := strings.Split(text, " · ")
	for i, part := range parts {
		key, label := splitHint(part)
		if key == "" {
			key, label, _ = strings.Cut(controlKeyNotation(part), " ")
		}
		parts[i] = paint(fg("text").Bold(true), key) + paint(fg("muted"), " "+label)
	}
	return strings.Join(parts, "  ")
}

// Explicit fallback: terminal glyph coverage cannot be detected reliably.
func asciiUI() bool { return os.Getenv("AGENT_HARBOR_ASCII") == "1" || os.Getenv("TERM") == "dumb" }

// Keep chrome substitutions cell-for-cell so mouse regions and padding retain
// their geometry. Unicode letters remain readable in names and paths.
func chromeText(s string) string {
	if !asciiUI() {
		return s
	}
	pairs := []string{"┌", "+", "┐", "+", "└", "+", "┘", "+", "├", "+", "┤", "+", "┬", "+", "┴", "+", "┼", "+", "⚓", "AH", "🚢", "SH", "🐦", "MG", "📂", "WD", "🤖", "AI", "🔌", "CN", "🧠", "MD", "🎛️", "OP", "💬", "CV", "👤", "PR", "🗂", "PF", "⚙", "S", "⌕", "/", "›", ">", "‹", "<", "·", ".", "●", "*", "•", "-", "↑", "^", "↓", "v", "←", "<", "→", ">", "│", "|", "─", "-", "╭", "+", "╮", "+", "╰", "+", "╯", "+", "█", "#", "…", ".", "⚠", "!", "✓", "+", "✗", "x", "▏", "|", "’", "'", "—", "-", "⎋", "E", "↵", "R", "⇥", "T", "⇧", "+", "⏳", "..", "🔎", "/ ", "⚡", "! ", "🧩", "EX", "○", "o", "💻", "PC", "🚀", "GO", "🧭", "TO", "🛟", "RE", "🧹", "CL", "🗑", "X"}
	for i := 0; i < len(pairs); i += 2 {
		width := ansi.StringWidth(pairs[i])
		value := ansi.Truncate(pairs[i+1], width, "")
		pairs[i+1] = value + strings.Repeat(" ", max(0, width-ansi.StringWidth(value)))
	}
	return strings.NewReplacer(pairs...).Replace(s)
}
