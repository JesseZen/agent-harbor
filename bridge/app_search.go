package main

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"
	"strings"
)

// searchMatches shares sanitized candidates and rune offsets with future pickers.
// Ranking and syntax are sahilm/fuzzy's, not fzf's.
func searchMatches(q string, items []string) []fuzzy.Match {
	clean := make([]string, len(items))
	for i, s := range items {
		clean[i] = cleanUI(s)
	}
	if q != "" {
		hits := fuzzy.Find(cleanUI(q), clean)
		// v0.1.1 returns UTF-8 byte offsets; expose rune positions to renderers.
		for i := range hits {
			offsets := map[int]int{}
			n := 0
			for b := range hits[i].Str {
				offsets[b] = n
				n++
			}
			for j, b := range hits[i].MatchedIndexes {
				hits[i].MatchedIndexes[j] = offsets[b]
			}
		}
		return hits
	}
	out := make([]fuzzy.Match, len(clean))
	for i, s := range clean {
		out[i] = fuzzy.Match{Str: s, Index: i}
	}
	return out
}
func (m harborModel) searchList() bool {
	switch m.screen {
	case "sessions", "magpie", "new-instance", "new-catalog":
		return true
	}
	return false
}
func (m harborModel) searchQuery() string {
	switch m.screen {
	case "magpie", "new-instance":
		return m.instanceFilter
	case "new-catalog":
		return m.catalogFilter
	}
	return m.filter
}
func (m *harborModel) setSearchQuery(s string) {
	switch m.screen {
	case "magpie", "new-instance":
		m.instanceFilter = s
	case "new-catalog":
		m.catalogFilter = s
	default:
		m.filter = s
	}
}
func (m harborModel) searchIDs() ([]string, int) {
	ids := []string{}
	switch m.screen {
	case "magpie", "new-instance":
		for _, r := range m.instanceRows() {
			ids = append(ids, r.ID)
		}
		return ids, m.instanceCursor
	case "new-catalog":
		for _, r := range m.filteredModels() {
			ids = append(ids, r.ID)
		}
		return ids, m.catalogCursor
	default:
		for _, r := range m.visible() {
			ids = append(ids, r.ID)
		}
		return ids, m.cursor
	}
}
func (m harborModel) searchSelectedID() string {
	ids, c := m.searchIDs()
	if c >= 0 && c < len(ids) {
		return ids[c]
	}
	return ""
}
func (m *harborModel) restoreSearchSelection(id string) {
	ids, c := m.searchIDs()
	c = min(c, max(0, len(ids)-1))
	for i, s := range ids {
		if s == id {
			c = i
			break
		}
	}
	switch m.screen {
	case "magpie", "new-instance":
		m.instanceCursor = c
	case "new-catalog":
		m.catalogCursor = c
	default:
		m.cursor = c
	}
}
func (m harborModel) updateSearch(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	key, ok := msg.(tea.KeyMsg)
	if !ok || !m.searchList() {
		return m, nil, false
	}
	k := key.String()
	if k == "/" && m.searchFocus != m.screen {
		m.searchFocus = m.screen
		m.input.SetValue(m.searchQuery())
		m.input.CursorEnd()
		m.input.Focus()
		return m, nil, true
	}
	if k == "esc" && (m.searchFocus == m.screen || m.searchQuery() != "") {
		id := m.searchSelectedID()
		m.setSearchQuery("")
		m.searchFocus = ""
		m.input.Blur()
		m.restoreSearchSelection(id)
		return m, nil, true
	}
	if k == "tab" || k == "shift+tab" {
		if m.searchFocus == m.screen {
			m.searchFocus = ""
			m.input.Blur()
		} else {
			m.searchFocus = m.screen
			m.input.SetValue(m.searchQuery())
			m.input.CursorEnd()
			m.input.Focus()
		}
		return m, nil, true
	}
	if m.searchFocus != m.screen {
		return m, nil, false
	}
	switch k {
	case "ctrl+c", "ctrl+g", "enter", "up", "down":
		return m, nil, false
	}
	before := m.searchQuery()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(key)
	if m.input.Value() != before {
		m.setSearchQuery(m.input.Value())
		// New queries start at the best-ranked result, like fzf change:top.
		// Refresh and return paths separately preserve the selected stable ID.
		switch m.screen {
		case "magpie", "new-instance":
			m.instanceCursor = 0
		case "new-catalog":
			m.catalogCursor = 0
		default:
			m.cursor = 0
		}
	}
	return m, cmd, true
}
func (m harborModel) searchLine(n, total int) string {
	focus, q := "List", m.searchQuery()
	if m.searchFocus == m.screen {
		focus = "Search"
		runes := []rune(m.input.Value())
		pos := min(m.input.Position(), len(runes))
		q = string(runes[:pos]) + "▏" + string(runes[pos:])
	}
	if q == "" {
		q = "/ to search"
	}
	prefix, suffix := focus+": ", fmt.Sprintf(" · %d/%d", n, total)
	budget := max(1, min(140, m.width-4)-ansi.StringWidth(prefix+suffix))
	if ansi.StringWidth(q) > budget {
		start := 0
		if pos := strings.Index(q, "▏"); pos >= 0 {
			start = max(0, ansi.StringWidth(q[:pos])-budget+2)
		}
		q = ansi.Truncate(ansi.Cut(q, start, ansi.StringWidth(q)), budget, "…")
	}
	return prefix + q + suffix
}
func (m harborModel) searchHints() string {
	if m.searchFocus == m.screen {
		return "↑↓ move  enter act  tab list  esc clear"
	}
	return "↑↓ move  enter act  / search  tab focus  ? help  q close"
}
func (m harborModel) listFooter(s string) string {
	if m.searchList() {
		return m.searchHints()
	}
	return s
}

// Highlight after fitting: offsets never include ANSI; restore the row's SGR.
func highlightSearch(line, q string) string {
	if q == "" || colorOff() {
		return line
	}
	hits := searchMatches(q, []string{ansi.Strip(line)})
	if len(hits) == 0 {
		return line
	}
	positions := map[int]bool{}
	for _, i := range hits[0].MatchedIndexes {
		positions[i] = true
	}
	return highlightPositions(line, positions)
}
func highlightPositions(line string, positions map[int]bool) string {
	var out strings.Builder
	index, active := 0, ""
	for len(line) > 0 {
		if line[0] == 27 {
			end := strings.IndexByte(line, 'm')
			if end >= 0 {
				seq := line[:end+1]
				out.WriteString(seq)
				if seq == "\x1b[0m" {
					active = ""
				} else {
					active += seq
				}
				line = line[end+1:]
				continue
			}
		}
		r := []rune(line)[0]
		if positions[index] {
			out.WriteString("\x1b[1;38;2;" + brandRGB() + "m")
			out.WriteRune(r)
			out.WriteString("\x1b[0m" + active)
		} else {
			out.WriteRune(r)
		}
		line = line[len(string(r)):]
		index++
	}
	return out.String()
}
func brandRGB() string {
	var r, g, b int
	fmt.Sscanf(harborPalette["brand"], "#%02x%02x%02x", &r, &g, &b)
	return fmt.Sprintf("%d;%d;%d", r, g, b)
}

// Map the original candidate's matched rune spans into fitted field prefixes.
// Hidden suffixes never receive a shifted highlight; metadata can match across
// name/path/model without asking the matcher to reinterpret a rendered line.
func candidatePositions(line, q string, fields []string) map[int]bool {
	positions := map[int]bool{}
	hits := searchMatches(q, []string{strings.Join(fields, " ")})
	if len(hits) == 0 {
		return positions
	}
	wanted := map[int]bool{}
	for _, i := range hits[0].MatchedIndexes {
		wanted[i] = true
	}
	plain := []rune(ansi.Strip(line))
	offset := 0
	for _, field := range fields {
		rs := []rune(cleanUI(field))
		if len(rs) == 0 {
			offset++
			continue
		}
		for start := 0; start < len(plain); start++ {
			if plain[start] != rs[0] {
				continue
			}
			n := 0
			for n < len(rs) && start+n < len(plain) && plain[start+n] == rs[n] {
				n++
			}
			if n == len(rs) || (n > 0 && start+n < len(plain) && plain[start+n] == '…') {
				for j := 0; j < n; j++ {
					if wanted[offset+j] {
						positions[start+j] = true
					}
				}
				break
			}
		}
		offset += len(rs) + 1
	}
	return positions
}
func (m harborModel) paintSearchBlock(lines []string, w int, mode string) []string {
	out := paintBlock(lines, w, mode)
	if !m.searchList() || m.searchQuery() == "" || colorOff() {
		return out
	}

	candidates := [][]string{}
	first, stride, start, count := 2, 1, 0, 0
	switch m.screen {
	case "sessions":
		first, stride = 3, 3
		count = max(1, (m.height-10)/3)
		start = m.listStart(m.cursor, count)
		rows := m.visible()
		for i := start; i < min(len(rows), start+count); i++ {
			s := rows[i]
			candidates = append(candidates, []string{s.Name, s.WorkingDirectory, s.MagpieModel})
		}
	case "magpie", "new-instance":
		count = max(1, m.height-18)
		if m.screen == "new-instance" {
			count = max(1, m.height-14)
		}
		start = m.listStart(m.instanceCursor, count)
		rows := m.instanceRows()
		for i := start; i < min(len(rows), start+count); i++ {
			r := rows[i]
			candidates = append(candidates, []string{r.Name, r.ID, r.Address, r.ConfigDir, r.Health})
		}
	case "new-catalog":
		first = 4
		count = max(1, m.height-12)
		start = m.listStart(m.catalogCursor, count)
		rows := m.filteredModels()
		for i := start; i < min(len(rows), start+count); i++ {
			c := rows[i]
			candidates = append(candidates, []string{c.ID, c.DisplayName, c.MagpieLabel, c.OwnedBy})
		}
	}
	for i := first; i < len(out); i++ {
		row := (i - first) / stride
		if row >= len(candidates) {
			break
		}
		out[i] = highlightPositions(out[i], candidatePositions(out[i], m.searchQuery(), candidates[row]))
	}
	return out
}
