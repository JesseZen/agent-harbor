package main

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

func colorOff() bool { _, off := os.LookupEnv("NO_COLOR"); return off }

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
func paintKeys(text string) string {
	if colorOff() {
		return text
	}
	sep := "  "
	if strings.Contains(text, " · ") {
		sep = " · "
	}
	parts := strings.Split(text, sep)
	for i, p := range parts {
		lead := len(p) - len(strings.TrimLeft(p, " "))
		body := strings.TrimLeft(p, " ")
		key, label, ok := strings.Cut(body, " ")
		if !ok || body == "" {
			parts[i] = paint(fg("label"), p)
			continue
		}
		parts[i] = strings.Repeat(" ", lead) + paint(fg("key").Bold(true), key) + paint(fg("label"), " "+label)
	}
	return strings.Join(parts, paint(fg("muted"), sep))
}
