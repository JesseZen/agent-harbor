package main

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"time"
)

type mouseClick struct {
	id   string
	x, y int
	at   time.Time
}
type mouseRegion struct {
	x, y, w, h int
	id, kind   string
	index      int
}

func (r mouseRegion) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}
func (m harborModel) mouseLayoutKey() string {
	ids, _ := m.searchIDs()
	return fmt.Sprintf("%s/%d/%d/%s/%s/%d/%d/%s/%d/%s", m.screen, m.width, m.height, m.searchQuery(), strings.Join(ids, "\x00"), m.scroll, m.picker.cursor, m.picker.query.Value(), m.formFocus, fmt.Sprint(m.mouseRegions()))
}

// Geometry is measured in terminal cells from the same fitted body as View.
// Frame rows (header, reserved feedback, rule) and its one-cell margin are
// excluded. Wrapping is measured before clipping, so hidden rows never act.
func (m harborModel) mouseRegions() []mouseRegion {
	w := max(20, min(140, m.width-4))
	out := []mouseRegion{}
	add := func(x, y, width, h int, id, kind string, index int) {
		if y >= 3 && y+h <= m.height-4 && width > 0 {
			out = append(out, mouseRegion{x, y, width, h, id, kind, index})
		}
	}
	if m.screen == "new-form" {
		y, x := 5, 1
		if m.height < 20 {
			y, x = 1, 0
		}
		for i := 0; i < 7; i++ {
			if m.height < 20 {
				if y+i >= m.height-1 {
					continue
				}
				out = append(out, mouseRegion{x, y + i, w, 1, fmt.Sprint(i), "form", i})
			} else {
				add(x, y+i, w, 1, fmt.Sprint(i), "form", i)
			}
		}
		return out
	}
	if m.screen == "sessions" {
		lw := w
		if m.width >= 100 {
			lw = max(35, w*45/100)
		}
		add(1, 4, lw, 1, "search", "search", 0)
		rows := m.visible()
		count := max(1, (m.height-10)/3)
		start := m.listStart(m.cursor, count)
		for i := start; i < min(len(rows), start+count); i++ {
			y := 6 + (i-start)*3
			h := min(3, m.height-4-y)
			if h > 0 {
				add(1, y, lw, h, rows[i].ID, "row", i)
			}
		}
		add(1, 3, lw, max(1, m.height-7), "list", "list", 0)
		if m.width >= 100 {
			add(lw+4, 3, w-lw-3, max(1, m.height-7), "detail", "detail", 0)
		}
		return out
	}
	body := ""
	first, count, cursor, stride := 0, 0, 0, 1
	ids := []string{}
	switch m.screen {
	case "magpie", "new-instance":
		body = m.magpieView()
		count = max(1, m.height-18)
		if m.screen == "new-instance" {
			body = m.newView()
			count = max(1, m.height-14)
		}
		first = 2
		cursor = m.instanceCursor
		for _, r := range m.instanceRows() {
			ids = append(ids, r.ID)
		}
	case "new-catalog":
		body = m.newView()
		first = 4
		count = max(1, m.height-12)
		cursor = m.catalogCursor
		for _, r := range m.filteredModels() {
			ids = append(ids, r.ID)
		}
	case "new-directory":
		body = m.directoryView()
		first = 4
		count = max(1, m.height-12)
		cursor = m.picker.cursor
		for _, r := range m.picker.matches() {
			ids = append(ids, r.path)
		}
	case "new-cli":
		body = m.newView()
		first = 2
		count = 1
		ids = []string{"codex"}
	case "new-connection":
		body = m.newView()
		first = 2
		stride = 3
		count = 2
		ids = []string{"local", "magpie"}
		cursor = 0
	default:
		switch m.screen {
		case "detail", "error", "help", "magpie-detail", "new-instance-detail", "confirm", "magpie-confirm", "new-notice", "new-review", "delete-history":
			add(1, 3, w, max(1, m.height-7), "detail", "detail", 0)
		}
		return out
	}
	lines := strings.Split(body, "\n")
	rowY := func(n int) int {
		y := 3
		for _, line := range lines[:min(n, len(lines))] {
			y += len(strings.Split(fitBlock(line, w, 10000), "\n"))
		}
		return y
	}
	if m.searchList() || m.screen == "new-directory" {
		search := 1
		if m.screen == "new-catalog" {
			search = 2
		}
		add(1, rowY(search), w, 1, "search", "search", 0)
	}
	start := m.listStart(cursor, count)
	for i := start; i < min(len(ids), start+count); i++ {
		n := first + (i-start)*stride
		if n >= len(lines) {
			break
		}
		h := min(len(strings.Split(fitBlock(lines[n], w, 10000), "\n")), m.height-4-rowY(n))
		if h > 0 {
			add(1, rowY(n), w, h, ids[i], "row", i)
		}
	}
	return out
}
func (m harborModel) updateMouse(msg tea.MouseMsg, now time.Time) (tea.Model, tea.Cmd) {
	// Releases and motion never count as a click (SGR sends both).
	wheel := msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown
	if msg.Action == tea.MouseActionRelease {
		return m, nil
	}
	if !wheel && (msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft) {
		return m, nil
	}
	var hit *mouseRegion
	for _, r := range m.mouseRegions() {
		if r.contains(msg.X, msg.Y) {
			v := r
			hit = &v
			break
		}
	}
	if wheel {
		m.mouse = mouseClick{}
		if hit == nil {
			return m, nil
		}
		key := tea.KeyDown
		if msg.Button == tea.MouseButtonWheelUp {
			key = tea.KeyUp
		}
		if hit.kind == "detail" {
			if m.screen == "sessions" {
				if s, ok := m.selected(); ok {
					rw := max(20, min(140, m.width-4))
					rw -= max(35, rw*45/100) + 3
					limit := max(0, len(strings.Split(fitBlock(m.details(s), rw, 10000), "\n"))-max(1, m.height-7))
					delta := 1
					if key == tea.KeyUp {
						delta = -1
					}
					m.detailScroll = max(0, min(limit, m.detailScroll+delta))
				}
				return m, nil
			}
			if m.screen == "delete-history" {
				key = tea.KeyPgDown
				if msg.Button == tea.MouseButtonWheelUp {
					key = tea.KeyPgUp
				}
			}
			return m.Update(tea.KeyMsg{Type: key})
		}
		if hit.kind != "row" && hit.kind != "search" && hit.kind != "list" {
			return m, nil
		}
		if m.screen == "new-directory" {
			m.picker.list = true
		}
		m.searchFocus = ""
		return m.Update(tea.KeyMsg{Type: key})
	}
	if hit == nil {
		m.mouse = mouseClick{}
		return m, nil
	}
	r := *hit
	if r.kind == "search" {
		m.mouse = mouseClick{}
		if m.screen == "new-directory" {
			m.picker.list = false
			m.picker.query.Focus()
			return m, nil
		}
		if m.searchFocus != m.screen {
			return m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
		}
		return m, nil
	}
	if r.kind == "form" {
		m.mouse = mouseClick{}
		m.formFocus = r.index
		return m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	}
	if r.kind != "row" {
		m.mouse = mouseClick{}
		return m, nil
	}
	double := m.mouse.id == r.id && m.mouse.x == msg.X && m.mouse.y == msg.Y && !now.Before(m.mouse.at) && now.Sub(m.mouse.at) <= 350*time.Millisecond
	// Selecting an already visible row must not move it out from under
	// the pointer before the second press. View and highlighting share this offset.
	count, cursor := 0, 0
	switch m.screen {
	case "sessions":
		count = max(1, (m.height-10)/3)
		cursor = m.cursor
	case "magpie":
		count = max(1, m.height-18)
		cursor = m.instanceCursor
	case "new-instance":
		count = max(1, m.height-14)
		cursor = m.instanceCursor
	case "new-catalog":
		count = max(1, m.height-12)
		cursor = m.catalogCursor
	case "new-directory":
		count = max(1, m.height-12)
		cursor = m.picker.cursor
	}
	if count > 0 {
		start := m.listStart(cursor, count)
		m.mouseListScreen = m.screen
		m.mouseListStart = start
	}
	m.searchFocus = ""
	m.input.Blur()
	switch m.screen {
	case "sessions":
		m.cursor = r.index
		m.route = observedRoute{}
		m.detailScroll = 0
	case "magpie", "new-instance":
		m.instanceCursor = r.index
	case "new-catalog":
		m.catalogCursor = r.index
	case "new-directory":
		m.picker.cursor = r.index
		m.picker.list = true
	case "new-connection":
		m.connectionCursor = r.index
	}
	m.mouse = mouseClick{r.id, msg.X, msg.Y, now}
	if double {
		m.mouse = mouseClick{}
		return m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	}
	if m.screen == "new-directory" {
		return m, m.requestDirectoryPreview()
	}
	return m, nil
}

func (m harborModel) listStart(cursor, count int) int {
	if m.mouseListScreen == m.screen && cursor >= m.mouseListStart && cursor < m.mouseListStart+count {
		return m.mouseListStart
	}
	return max(0, cursor-count+1)
}
