package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type sessionMotion struct {
	ctx                    context.Context
	cancel                 context.CancelFunc
	generation             uint64
	start, now, selectedAt time.Time
	selected               string
}
type motionTick uint64

func (m *harborModel) syncMotion(visible bool) tea.Cmd {
	active := false
	if visible && !colorOff() && !asciiUI() && os.Getenv("AGENT_HARBOR_REDUCED_MOTION") != "1" {
		geometry := m.sessionGeometry()
		entries, _, start := m.sessionListPlan(geometry)
		for _, entry := range entries {
			if !entry.isHeader && entry.top >= start && entry.top+geometry.row <= start+geometry.body && sessionWord(entry.view) == "Working" {
				active = true
				break
			}
		}
	}
	clock := &m.motion
	if !active {
		if clock.cancel != nil {
			clock.cancel()
			*clock = sessionMotion{generation: clock.generation + 1}
		}
		return nil
	}
	if selected, ok := m.selected(); ok && selected.ID != clock.selected {
		clock.selected = selected.ID
		clock.selectedAt = time.Now()
	}
	if clock.cancel != nil {
		return nil
	}
	clock.ctx, clock.cancel = context.WithCancel(m.refreshContext)
	clock.start = time.Now()
	clock.now = clock.start
	clock.generation++
	return waitView(clock.ctx, 90*time.Millisecond, motionTick(clock.generation))
}

func (m harborModel) motionFrame(msg motionTick) (tea.Model, tea.Cmd) {
	clock := &m.motion
	if clock.ctx == nil || clock.ctx.Err() != nil || clock.generation != uint64(msg) {
		return m, nil
	}
	clock.now = time.Now()
	return m, waitView(clock.ctx, 90*time.Millisecond, msg)
}

func sessionWord(s sessionView) string {
	if s.Runtime.Presence == "observed" {
		return runtimeLabel(s.Runtime)
	}
	if s.DiscoveryWarning != "" {
		return "Offline"
	}
	if s.Runtime.Presence == "not_observed" {
		return "Not observed"
	}
	if s.Runtime.Presence != "" {
		return unobservedStatus(s.Runtime)
	}
	return orUnknown(s.Status)
}

func workingOrange(intensity float64) lipgloss.Color {
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", 224+int(31*intensity), 147+int(71*intensity), 80+int(92*intensity)))
}

func (m harborModel) sessionIndicator(s sessionView, row uiRowStyle, showWord bool) (string, string) {
	word := sessionWord(s)
	symbol, ascii, role := "?", "?", "idle"
	switch word {
	case "Working":
		symbol, ascii, role = "◐", "~", "warn"
	case "Blocked":
		symbol, ascii, role = "◆", "!", "bad"
	case "Done":
		symbol, ascii, role = "●", "+", "ok"
	case "Idle":
		symbol, ascii = "●", "."
		if colorOff() {
			symbol = "○"
		}
	case "Starting":
		symbol, ascii, role = "◌", "*", "title"
	case "Stopped":
		symbol, ascii = "■", "-"
	case "Not started":
		symbol, ascii = "□", "_"
	case "Not observed":
		symbol, ascii = "◇", "/"
	case "Offline":
		symbol, ascii, role = "×", "x", "bad"
	}
	if asciiUI() {
		symbol = ascii
	}
	style := row.style(role)
	iconStyle := style
	if word == "Working" {
		style = style.Foreground(workingOrange(0))
		intensity := 0.4
		if m.motion.cancel != nil {
			intensity = (1 - math.Cos(2*math.Pi*m.motion.now.Sub(m.motion.start).Seconds()/1.8)) / 2
		}
		iconStyle = style.Foreground(workingOrange(intensity))
	}
	icon := paint(iconStyle, symbol)
	if !showWord {
		return icon, ""
	}
	if word != "Working" || m.motion.cancel == nil {
		return icon, paint(style, word)
	}
	origin := m.motion.start
	if s.ID == m.motion.selected {
		origin = m.motion.selectedAt
	}
	phase := math.Mod(m.motion.now.Sub(origin).Seconds(), 4) - 0.6
	if phase < 0 || phase > 1 {
		return icon, paint(style, word)
	}
	var letters strings.Builder
	for index, letter := range word {
		intensity := 0.0
		dist := math.Abs(float64(index) - (phase*10 - 2))
		if dist < 2 {
			intensity = (1 + math.Cos(math.Pi*dist/2)) / 2
		}
		letters.WriteString(paint(style.Foreground(workingOrange(intensity)), string(letter)))
	}
	return icon, letters.String()
}

func (m harborModel) sessionSuffixes(entries []sessionListEntry) map[string]string {
	groups := make(map[string][]sessionView)
	for _, entry := range entries {
		if !entry.isHeader {
			name := cleanUI(entry.view.Name)
			groups[name] = append(groups[name], entry.view)
		}
	}
	result := make(map[string]string)
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		counts := map[string]int{}
		for _, view := range group {
			counts[sessionProjectShort(view.WorkingDirectory)]++
			counts["profile:"+m.profileShortLabel(view.Profile)]++
		}
		for _, view := range group {
			project := sessionProjectShort(view.WorkingDirectory)
			profile := m.profileShortLabel(view.Profile)
			choice := ""
			if counts[project] == 1 {
				choice = project
			}
			if !view.External && counts["profile:"+profile] == 1 && (choice == "" || ansi.StringWidth(profile) < ansi.StringWidth(choice)) {
				choice = profile
			}
			if choice == "" {
				for length := 1; length <= len(view.ID); length++ {
					token := view.ID[len(view.ID)-length:]
					matches := 0
					for _, other := range group {
						if strings.HasSuffix(other.ID, token) {
							matches++
						}
					}
					if matches == 1 {
						choice = token
						break
					}
				}
			}
			result[view.ID] = " · " + cleanUI(choice)
		}
	}
	return result
}

func (m harborModel) previewHeader(view sessionView, width int) []string {
	icon, word := m.sessionIndicator(view, uiRowStyle{}, true)
	status := icon + " " + word
	name := ansi.Truncate(cleanUI(view.Name), max(1, width-ansi.StringWidth(status)-1), "…")
	lines := []string{paint(fg("text").Bold(true), name) + strings.Repeat(" ", max(1, width-ansi.StringWidth(status)-ansi.StringWidth(name))) + status}
	conn, model := "Native", ""
	if view.External {
		conn = "External · connection unknown"
	} else if view.Mode == "magpie" {
		conn = "Magpie"
		model = cleanUI(strings.Trim(strings.Join([]string{view.InstanceID, view.MagpieModel}, "/"), "/"))
	}
	profile := ""
	if !view.External && (len(m.profiles) > 1 || view.Profile == nil) {
		profile = " · " + ansi.Truncate(cleanUI(m.profileShortLabel(view.Profile)), 18, "…")
	}
	fields := conn + profile
	if view.CLI != "" {
		fields = cleanUI(view.CLI) + " · " + fields
	}
	if proj := sessionProjectShort(view.WorkingDirectory); proj != "" {
		projectWidth := max(1, width-ansi.StringWidth(fields)-3)
		if model != "" {
			projectWidth = max(1, projectWidth-min(21, ansi.StringWidth(model)+3))
		}
		fields = ansi.Truncate(proj, projectWidth, "…") + " · " + fields
	}
	modelWidth := width - ansi.StringWidth(fields) - 3
	if model != "" && modelWidth >= 2 {
		fields = strings.TrimSuffix(fields, profile) + " · " + ansi.Truncate(model, modelWidth, "…") + profile
	}
	lines = append(lines, paint(fg("value"), ansi.Truncate(fields, width, "…")))
	if badge, role := sessionStatusBadge(view); badge != "" && badge != "Offline" {
		diagnosis := sessionAttention(view)
		if diagnosis == "" {
			diagnosis = badge
		}
		lines = append(lines, paint(fg(role), ansi.Truncate(cleanUI(diagnosis), width, "…")))
	} else if view.Runtime.LaunchPending && !view.Runtime.InteractiveReady {
		lines = append(lines, paint(fg("muted"), "Waiting for interactive readiness"))
	}
	return lines
}

func (m harborModel) previewPane(view sessionView, g sessionGeometry) []string {
	lines := m.previewHeader(view, g.right)
	height := g.body - len(lines) - 1
	target, reason := targetForPreview(view)
	screen := m.preview.frame
	if reason == "" {
		reason = "Loading terminal…"
		if _, ok := m.service.(previewReader); !ok {
			reason = "Terminal preview unavailable"
		}
		if m.preview.target == target {
			reason = m.preview.message
			if reason == "" && len(screen.lines) == 0 {
				reason = "Loading terminal…"
			}
		}
	}
	start := max(0, len(screen.lines)-height-min(m.preview.offset, max(0, len(screen.lines)-height)))
	label := ""
	if reason == "" {
		if start > 0 {
			label = fmt.Sprintf("↑ %d ", start)
		}
		if screen.truncated {
			label += "truncated "
		}
	}
	lines = append(lines, paint(fg("border"), label+strings.Repeat("─", max(0, g.right-ansi.StringWidth(label)))))
	for index := range lines {
		lines[index] = chromeText(lines[index])
	}
	if reason != "" {
		return append(lines, paint(fg("muted"), chromeText(reason)))
	}
	for _, line := range screen.lines[start:min(len(screen.lines), start+height)] {
		lines = append(lines, ansi.Truncate(line, min(g.right, 1024), ""))
	}
	return lines
}

func (m harborModel) previewScrollLimit(view sessionView, geometry sessionGeometry) int {
	capacity := geometry.body - len(m.previewHeader(view, geometry.right)) - 1
	return max(0, len(m.preview.frame.lines)-capacity)
}
