package main

import (
	"os"
	"regexp"
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

// Emoji that open a heading line. ALL-CAPS headings become section rules.
var headingEmoji = []string{"⚓", "🚢", "📂", "🔌", "💬", "🛟", "🧭", "🐦", "✨", "🩺", "🧹", "🗑", "📖", "🔎", "🤖", "💻", "🧩", "🚀", "⚡"}

func colorOff() bool { _, off := os.LookupEnv("NO_COLOR"); return off }

func fg(role string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(harborPalette[role]))
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
	return label + strings.Repeat(" ", max(2, col-ansi.StringWidth(label))) + value
}

func note(text string) string { return "• " + text }

func statusDot(status string) string {
	if status == "" {
		return ""
	}
	return "● " + status
}

var (
	statusRe = regexp.MustCompile(`(● )?\b(Running|Ready|ready|verified|Verified|Exited|Stopped|stopped|not observed|Needs attention|unknown|Unknown|offline|missing|stale|unavailable|failed|Failed|error)\b`)
	stepRe   = regexp.MustCompile(`^(\s*)(\d+ / \d+)(\s+)(.*)$`)
	kvRe     = regexp.MustCompile(`^(\s*)([A-Z][A-Za-z0-9 /().'-]{0,22}?)( {2,})(\S.*)$`)
	helpRe   = regexp.MustCompile(`^(\s*)(\S.{0,10}?)( {2,})(\S.*)$`)
	capsRe   = regexp.MustCompile(`^[A-Z0-9 ·/&-]+$`)
)

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

// paintRich colours status words inside text while keeping a base style.
func paintRich(text string, base lipgloss.Style) string {
	if colorOff() {
		return text
	}
	out, last := "", 0
	for _, m := range statusRe.FindAllStringSubmatchIndex(text, -1) {
		out += paint(base, text[last:m[0]])
		word := text[m[4]:m[5]]
		st := base.Foreground(lipgloss.Color(harborPalette[statusRole(word)]))
		out += paint(st, text[m[0]:m[1]])
		last = m[1]
	}
	return out + paint(base, text[last:])
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

func startsWithHeadingEmoji(s string) (string, bool) {
	for _, e := range headingEmoji {
		if strings.HasPrefix(s, e) {
			return strings.TrimSpace(strings.TrimPrefix(s, e)), true
		}
	}
	return "", false
}

// paintLine styles one plain, already fitted line of at most width cells.
func paintLine(line string, width int, mode string) string {
	if colorOff() {
		return strings.ReplaceAll(line, keysMark, " ")
	}
	trim := strings.TrimSpace(line)
	lead := line[:len(line)-len(strings.TrimLeft(line, " "))]
	switch {
	case trim == "":
		return line
	case strings.HasPrefix(line, keysMark):
		return paintKeys(strings.TrimPrefix(line, keysMark)) + " "
	case strings.Trim(trim, "─") == "":
		return paint(fg("muted"), line)
	case strings.Contains(line, "› "):
		at := strings.Index(line, "› ")
		sel := fg("text").Bold(true).Background(lipgloss.Color(harborPalette["selBg"]))
		rest := line[at+len("› "):]
		pad := strings.Repeat(" ", max(0, width-ansi.StringWidth(line)))
		return line[:at] + paint(sel.Foreground(lipgloss.Color(harborPalette["brand"])), "› ") + paintRich(rest+pad, sel)
	case mode == "help" && helpRe.MatchString(line) && !strings.HasPrefix(trim, "•"):
		m := helpRe.FindStringSubmatch(line)
		return m[1] + paint(fg("key").Bold(true), m[2]) + m[3] + paint(fg("text"), m[4])
	}
	if rest, ok := startsWithHeadingEmoji(trim); ok {
		if capsRe.MatchString(rest) {
			fill := width - ansi.StringWidth(line) - 1
			if fill > 1 {
				return paint(fg("section").Bold(true), line) + " " + paint(fg("muted"), strings.Repeat("─", fill))
			}
			return paint(fg("section").Bold(true), line)
		}
		return paintRich(line, fg("title").Bold(true))
	}
	if m := stepRe.FindStringSubmatch(line); m != nil {
		title, dots := m[4], ""
		if i := strings.LastIndex(title, "   "); i > 0 && strings.ContainsAny(title[i:], "●○") {
			title, dots = title[:i+3], title[i+3:]
		}
		return m[1] + paint(fg("ink").Background(lipgloss.Color(harborPalette["title"])).Bold(true), m[2]) + m[3] + paint(fg("title").Bold(true), title) + paint(fg("brand"), dots)
	}
	switch {
	case strings.HasPrefix(trim, "!") || strings.HasPrefix(trim, "⚠") || strings.HasPrefix(trim, "Warning") || strings.HasPrefix(trim, "Needs attention"):
		return paint(fg("warn"), line)
	case strings.HasPrefix(trim, "• "):
		return lead + paint(fg("brand"), "•") + paint(fg("label").Italic(true), strings.TrimPrefix(trim, "•"))
	case strings.HasPrefix(trim, "● "):
		return paintRich(line, fg("text"))
	}
	if m := kvRe.FindStringSubmatch(line); m != nil {
		return m[1] + paint(fg("label"), m[2]) + m[3] + paintRich(m[4], fg("text"))
	}
	return paintRich(line, fg("text"))
}

func paintBlock(lines []string, width int, mode string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = paintLine(l, width, mode)
	}
	return out
}

// harborFrame assembles header, painted body, notice and key footer. Every
// row starts with one space of margin; width excludes that margin.
func harborFrame(crumb string, body []string, feedback, notice, footer string, width, height int, warn bool) string {
	brand := " ⚓ HARBOR "
	head := ansi.Truncate(cleanUI(brand+" › "+crumb), width, "…")
	if !colorOff() {
		rest := strings.TrimPrefix(head, brand)
		head = paint(fg("ink").Background(lipgloss.Color(harborPalette["brand"])).Bold(true), brand) + paint(fg("muted"), strings.TrimSuffix(rest, strings.TrimPrefix(rest, " › "))) + paint(fg("text").Bold(true), strings.TrimPrefix(rest, " › "))
	}
	rule := paint(fg("muted"), strings.Repeat("─", width))
	lines := []string{head, paint(fg("warn"), ansi.Truncate(cleanUI(feedback), width, "…")), rule}
	lines = append(lines, body...)
	for len(lines) < height-4 {
		lines = append(lines, "")
	}
	notice = ansi.Truncate(cleanUI(notice), width, "…")
	if warn {
		notice = paint(fg("warn"), notice)
	} else if strings.HasSuffix(notice, ".") {
		notice = paint(fg("label").Italic(true), notice)
	} else {
		notice = paintKeys(notice)
	}
	lines = append(lines, rule, notice, paintKeys(ansi.Truncate(cleanUI(footer), width, "…")))
	for i := range lines {
		if lines[i] != "" {
			lines[i] = " " + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}
