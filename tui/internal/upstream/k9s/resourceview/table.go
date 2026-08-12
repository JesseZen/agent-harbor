// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s
//
// Package resourceview is a Bubble Tea port of K9s' complete selectable table
// module: row identity, cursor retention, marks, filtering, selected-column
// sorting, responsive column priority, mouse hit testing and table rendering.
package resourceview

import (
	"cmp"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type Alignment int

const (
	AlignLeft Alignment = iota
	AlignRight
)

type Column struct {
	Title    string
	MinWidth int
	Priority int
	Align    Alignment
}

type Row struct {
	ID    string
	Cells []string
}

type Model struct {
	title          string
	scope          string
	columns        []Column
	rows           []Row
	visible        []Row
	marked         map[string]struct{}
	cursor         int
	selectedColumn int
	sortColumn     int
	sortAscending  bool
	filtering      bool
	filter         string
	width          int
	height         int
	disconnected   bool
	errMessage     string
	dirty          bool
	layout         tableLayout
	lastAction     Action
	lastClickTime  time.Time
	footerActions  FooterActions
}

func New(title string, columns []Column) *Model {
	return NewScoped(title, "all", columns)
}

func NewScoped(title, scope string, columns []Column) *Model {
	cloned := append([]Column(nil), columns...)
	for index := range cloned {
		cloned[index].MinWidth = max(cloned[index].MinWidth, ansi.StringWidth(cloned[index].Title))
	}
	return &Model{
		title:      title,
		scope:      scope,
		columns:    cloned,
		marked:     make(map[string]struct{}),
		sortColumn: -1,
		width:      80,
		height:     20,
		footerActions: FooterActions{
			Create: true, Edit: true, Delete: true, Publish: true, Filter: true, Mark: true,
		},
	}
}

func (model *Model) SetTitle(title string) { model.title = title }

func (model *Model) SetScope(scope string) { model.scope = scope }

func (model *Model) SetDirty(dirty bool) { model.dirty = dirty }

func (model *Model) SetFooterActions(actions FooterActions) {
	model.footerActions = actions
	model.layout = tableLayout{}
	if !actions.Filter && model.filtering {
		model.filtering = false
		model.filter = ""
		model.rebuildVisible(model.SelectedID())
	}
	if !actions.Mark {
		clear(model.marked)
	}
}

func (model *Model) SetDisconnected(disconnected bool, message string) {
	model.disconnected = disconnected
	model.errMessage = message
}

func (model *Model) Disconnected() bool { return model.disconnected }

func (model *Model) LastAction() Action { return model.lastAction }

func (model *Model) SetSize(width, height int) {
	model.width = max(1, width)
	model.height = max(1, height)
}

func (model *Model) SetRows(rows []Row) {
	selected := model.SelectedID()
	model.rows = make([]Row, len(rows))
	known := make(map[string]struct{}, len(rows))
	for index := range rows {
		model.rows[index] = rows[index]
		model.rows[index].Cells = append([]string(nil), rows[index].Cells...)
		known[rows[index].ID] = struct{}{}
	}
	for id := range model.marked {
		if _, ok := known[id]; !ok {
			delete(model.marked, id)
		}
	}
	model.rebuildVisible(selected)
}

func (model *Model) Update(message tea.Msg) bool {
	model.lastAction = ActionNone
	switch message := message.(type) {
	case tea.MouseMsg:
		return model.handleMouse(message)
	case tea.KeyMsg:
		return model.handleKey(message)
	default:
		return false
	}
}

func (model *Model) handleKey(key tea.KeyMsg) bool {
	if model.filtering {
		switch key.Type {
		case tea.KeyEsc, tea.KeyDelete:
			// Esc and Delete both abandon filter mode (clear query).
			model.filtering = false
			model.filter = ""
			model.rebuildVisible("")
		case tea.KeyEnter:
			model.filtering = false
		case tea.KeyBackspace:
			// Mac's Delete key is Backspace: empty query exits filter mode.
			if runes := []rune(model.filter); len(runes) > 0 {
				model.filter = string(runes[:len(runes)-1])
				model.rebuildVisible("")
			} else {
				model.filtering = false
				model.filter = ""
				model.rebuildVisible("")
			}
		case tea.KeyUp:
			model.move(-1)
		case tea.KeyDown:
			model.move(1)
		case tea.KeyRunes:
			model.filter += string(key.Runes)
			model.rebuildVisible("")
		}
		return true
	}

	switch key.String() {
	case "up", "k":
		model.move(-1)
	case "down", "j":
		model.move(1)
	case "home", "g":
		model.cursor = 0
	case "end", "G":
		model.cursor = max(0, len(model.visible)-1)
	case "pgup":
		model.cursor = max(0, model.cursor-max(1, model.bodyHeight()-1))
	case "pgdown":
		model.cursor = min(max(0, len(model.visible)-1), model.cursor+max(1, model.bodyHeight()-1))
	case " ":
		if !model.footerActions.Mark {
			return false
		}
		model.toggleMark()
	case "[":
		model.moveColumn(-1)
	case "]":
		model.moveColumn(1)
	case "s":
		model.sortSelectedColumn()
	case "/":
		if !model.footerActions.Filter {
			return false
		}
		model.filtering = true
		model.filter = ""
		model.rebuildVisible("")
		model.lastAction = ActionFilter
	default:
		return false
	}
	return true
}

func (model *Model) handleMouse(message tea.MouseMsg) bool {
	if message.Button == tea.MouseButtonWheelUp {
		model.move(-3)
		return true
	}
	if message.Button == tea.MouseButtonWheelDown {
		model.move(3)
		return true
	}
	if message.Action != tea.MouseActionPress {
		return false
	}

	hit := model.HitTest(message.X, message.Y)
	switch hit.Kind {
	case HitRow:
		rowIndex := hit.RowIndex
		if rowIndex >= 0 && rowIndex < len(model.visible) {
			model.cursor = rowIndex
			if time.Since(model.lastClickTime) < 400*time.Millisecond {
				model.lastAction = ActionDetails
			}
			model.lastClickTime = time.Now()
		}
		return true
	case HitHeader:
		model.selectedColumn = hit.ColumnIndex
		model.sortSelectedColumn()
		return true
	case HitFooterFilter:
		model.filtering = true
		model.filter = ""
		model.rebuildVisible("")
		model.lastAction = ActionFilter
		return true
	case HitFooterAction:
		model.lastAction = hit.Action
		return true
	default:
		return false
	}
}

func (model *Model) HitTest(x, y int) Hit {
	layout := model.layout
	if len(layout.rowRegions) == 0 {
		layout = model.computeLayout()
		model.layout = layout
	}
	for index, region := range layout.rowRegions {
		if region.contains(x, y) {
			return Hit{Kind: HitRow, RowIndex: layout.scrollStart + index, X: x, Y: y}
		}
	}
	for index, region := range layout.headerRegions {
		if region.contains(x, y) {
			columnIndex := 0
			if index < len(layout.visibleColumns) {
				columnIndex = layout.visibleColumns[index]
			}
			return Hit{Kind: HitHeader, ColumnIndex: columnIndex, X: x, Y: y}
		}
	}
	if layout.footerFilter.contains(x, y) {
		centerX, centerY := layout.footerFilter.center()
		return Hit{Kind: HitFooterFilter, X: centerX, Y: centerY}
	}
	for _, region := range layout.footerActions {
		if region.region.contains(x, y) {
			centerX, centerY := region.region.center()
			return Hit{Kind: HitFooterAction, Action: region.action, X: centerX, Y: centerY}
		}
	}
	return Hit{Kind: HitNone, X: x, Y: y}
}

func (model *Model) LayoutFooterLine() int {
	layout := model.layout
	if layout.footerLine == 0 {
		layout = model.computeLayout()
	}
	return layout.footerLine
}

func (model *Model) footerActionHit(action Action) Hit {
	return model.FooterActionHit(action)
}

func (model *Model) FooterActionHit(action Action) Hit {
	layout := model.layout
	if len(layout.footerActions) == 0 {
		layout = model.computeLayout()
		model.layout = layout
	}
	for _, region := range layout.footerActions {
		if region.action == action {
			centerX, centerY := region.region.center()
			return Hit{Kind: HitFooterAction, Action: action, X: centerX, Y: centerY}
		}
	}
	return Hit{Kind: HitNone}
}

func (model *Model) FooterFilterHit() Hit {
	layout := model.layout
	if layout.footerFilter.width == 0 {
		layout = model.computeLayout()
		model.layout = layout
	}
	if layout.footerFilter.width == 0 {
		return Hit{Kind: HitNone}
	}
	centerX, centerY := layout.footerFilter.center()
	return Hit{Kind: HitFooterFilter, X: centerX, Y: centerY}
}

func (model *Model) move(delta int) {
	if len(model.visible) == 0 {
		model.cursor = 0
		return
	}
	model.cursor = min(len(model.visible)-1, max(0, model.cursor+delta))
}

func (model *Model) moveColumn(delta int) {
	if len(model.columns) == 0 {
		return
	}
	model.selectedColumn = (model.selectedColumn + delta + len(model.columns)) % len(model.columns)
}

func (model *Model) sortSelectedColumn() {
	if len(model.columns) == 0 {
		return
	}
	if model.sortColumn == model.selectedColumn {
		model.sortAscending = !model.sortAscending
	} else {
		model.sortColumn = model.selectedColumn
		model.sortAscending = true
	}
	model.rebuildVisible(model.SelectedID())
}

func (model *Model) toggleMark() {
	id := model.SelectedID()
	if id == "" {
		return
	}
	if _, ok := model.marked[id]; ok {
		delete(model.marked, id)
	} else {
		model.marked[id] = struct{}{}
	}
}

func (model *Model) rebuildVisible(selected string) {
	query := strings.ToLower(strings.TrimSpace(model.filter))
	model.visible = model.visible[:0]
	for _, row := range model.rows {
		if query == "" || strings.Contains(strings.ToLower(row.ID+" "+strings.Join(row.Cells, " ")), query) {
			model.visible = append(model.visible, row)
		}
	}
	if model.sortColumn >= 0 {
		column := model.sortColumn
		ascending := model.sortAscending
		sort.SliceStable(model.visible, func(left, right int) bool {
			comparison := cmp.Compare(strings.ToLower(cell(model.visible[left], column)), strings.ToLower(cell(model.visible[right], column)))
			if ascending {
				return comparison < 0
			}
			return comparison > 0
		})
	}
	model.cursor = 0
	if selected != "" {
		for index, row := range model.visible {
			if row.ID == selected {
				model.cursor = index
				break
			}
		}
	}
}

func cell(row Row, index int) string {
	if index < 0 || index >= len(row.Cells) {
		return ""
	}
	return row.Cells[index]
}

func (model *Model) SelectedID() string {
	if model.cursor < 0 || model.cursor >= len(model.visible) {
		return ""
	}
	return model.visible[model.cursor].ID
}

func (model *Model) MarkedIDs() []string {
	ids := make([]string, 0, len(model.marked))
	for id := range model.marked {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (model *Model) VisibleRowIDs() []string {
	ids := make([]string, len(model.visible))
	for index, row := range model.visible {
		ids[index] = row.ID
	}
	return ids
}

func (model *Model) Filtering() bool { return model.filtering }

// Filter returns the current filter query text.
func (model *Model) Filter() string { return model.filter }

func (model *Model) View() string {
	if model.disconnected {
		return model.renderDisconnected()
	}
	model.layout = model.computeLayout()
	layout := model.layout
	lines := make([]string, 0, model.height)
	lines = append(lines, model.renderBorder(layout))
	lines = append(lines, model.wrapContent(model.renderHeader(layout.visibleColumns, layout.widths), layout))
	lines = append(lines, model.renderBody(layout)...)
	if layout.separatorLine >= 0 {
		lines = append(lines, model.renderSeparator(layout))
	}
	if model.height > 2 {
		lines = append(lines, model.wrapContent(model.renderFooter(), layout))
	}
	if model.height > 1 {
		lines = append(lines, model.renderBottom(layout))
	}
	if len(lines) > model.height {
		lines = lines[:model.height]
	}
	return strings.Join(lines, "\n")
}

func (model *Model) bodyHeight() int {
	layout := model.layout
	return max(0, layout.bodyEndLine-layout.bodyStartLine+1)
}

func (model *Model) renderDisconnected() string {
	message := model.errMessage
	if message == "" {
		message = "disconnected from Core"
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(TokenError))
	line := style.Render(ansi.Truncate(message, model.width, "…"))
	border := lipgloss.NewStyle().Foreground(lipgloss.Color(TokenBorder))
	top := border.Render("─" + message + strings.Repeat("─", max(0, model.width-ansi.StringWidth(message)-1)))
	return strings.Join([]string{top, line}, "\n")
}

func (model *Model) renderHeader(columns, widths []int) string {
	// Muted band + accent labels: selected row stays the brightest chrome.
	sep := lipgloss.NewStyle().
		Foreground(lipgloss.Color(TokenHeader)).
		Background(lipgloss.Color(TokenHeaderBg)).
		Render("┊")
	parts := make([]string, 0, len(columns)*2)
	for visibleIndex, columnIndex := range columns {
		if visibleIndex > 0 {
			parts = append(parts, sep)
		}
		label := model.columns[columnIndex].Title
		if columnIndex == model.sortColumn {
			if model.sortAscending {
				label += "↑"
			} else {
				label += "↓"
			}
		}
		style := lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color(TokenHeader)).
			Background(lipgloss.Color(TokenHeaderBg)).
			Width(widths[visibleIndex])
		if columnIndex == model.selectedColumn {
			style = style.Underline(true)
		}
		parts = append(parts, style.Render(ansi.Truncate(label, widths[visibleIndex], "…")))
	}
	// Match row mark/cursor prefix so columns align with body cells.
	prefix := lipgloss.NewStyle().Background(lipgloss.Color(TokenHeaderBg)).Render("  ")
	line := prefix + strings.Join(parts, "")
	width := model.contentWidth()
	pad := max(0, width-ansi.StringWidth(line))
	if pad > 0 {
		line += lipgloss.NewStyle().Background(lipgloss.Color(TokenHeaderBg)).Render(strings.Repeat(" ", pad))
	}
	return ansi.Truncate(line, width, "")
}

func (model *Model) renderBody(layout tableLayout) []string {
	lines := make([]string, 0, layout.bodyEndLine-layout.bodyStartLine+1)
	for index := layout.scrollStart; index < min(len(model.visible), layout.scrollStart+(layout.bodyEndLine-layout.bodyStartLine+1)); index++ {
		row := model.renderRow(model.visible[index], index == model.cursor, layout.visibleColumns, layout.widths)
		lines = append(lines, model.wrapContent(row, layout))
	}
	for len(lines) < layout.bodyEndLine-layout.bodyStartLine+1 {
		lines = append(lines, model.emptyContentLine(layout))
	}
	return lines
}

func (model *Model) renderRow(row Row, selected bool, columns, widths []int) string {
	// Continuous selected band + glyph column rules (┊ idle / │ selected).
	rule := TokenRuleIdle
	glyph := "┊"
	if selected {
		rule = TokenRuleSelected
		glyph = "│"
	}
	sepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(rule))
	if selected {
		sepStyle = sepStyle.Background(lipgloss.Color(TokenSelected))
	}
	sep := sepStyle.Render(glyph)

	parts := make([]string, 0, len(columns)*2)
	for visibleIndex, columnIndex := range columns {
		if visibleIndex > 0 {
			parts = append(parts, sep)
		}
		value := ansi.Truncate(cell(row, columnIndex), widths[visibleIndex], "…")
		style := lipgloss.NewStyle().Foreground(lipgloss.Color(model.cellColor(value))).Width(widths[visibleIndex])
		if selected {
			style = style.Background(lipgloss.Color(TokenSelected)).Bold(true)
		}
		if model.columns[columnIndex].Align == AlignRight {
			style = style.Align(lipgloss.Right)
		}
		parts = append(parts, style.Render(value))
	}
	prefix := "  "
	prefixStyle := lipgloss.NewStyle()
	if _, ok := model.marked[row.ID]; ok {
		prefix = "● "
		prefixStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(TokenHeader))
	}
	if selected {
		prefixStyle = prefixStyle.Background(lipgloss.Color(TokenSelected)).Bold(true)
		if _, ok := model.marked[row.ID]; ok {
			prefixStyle = prefixStyle.Foreground(lipgloss.Color(TokenHeader))
		}
	}
	line := prefixStyle.Render(prefix) + strings.Join(parts, "")
	width := model.contentWidth()
	if selected {
		pad := max(0, width-ansi.StringWidth(line))
		if pad > 0 {
			line += lipgloss.NewStyle().Background(lipgloss.Color(TokenSelected)).Render(strings.Repeat(" ", pad))
		}
	}
	return ansi.Truncate(line, width, "")
}

func (model *Model) renderFooter() string {
	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(TokenOnAccent)).
		Background(lipgloss.Color(TokenAccent)).
		Bold(true).
		Padding(0, 1)
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(TokenText))
	sepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(TokenBorder))
	sep := sepStyle.Render(" │ ")

	chips := make([]string, 0, 8)
	for _, entry := range model.footerActionLabels() {
		chips = append(chips, keyStyle.Render(entry.key)+" "+descStyle.Render(entry.desc))
	}
	if model.footerActions.Filter {
		chips = append(chips, keyStyle.Render("/")+" "+descStyle.Render("filter"))
	}
	chips = append(chips,
		keyStyle.Render("[ ]")+" "+descStyle.Render("column"),
		keyStyle.Render("s")+" "+descStyle.Render("sort"),
	)
	if model.footerActions.Mark {
		chips = append(chips, keyStyle.Render("space")+" "+descStyle.Render("mark"))
	}
	prefix := lipgloss.NewStyle().Foreground(lipgloss.Color(TokenText)).Faint(true).Render(model.footerPrefix())
	footer := prefix + strings.Join(chips, sep)
	if marked := len(model.marked); marked > 0 {
		footer += sep + descStyle.Render(itoa(marked)+" marked")
	}
	return ansi.Truncate(footer, model.contentWidth(), "…")
}

func (model *Model) layoutColumns() ([]int, []int) {
	indices := make([]int, len(model.columns))
	widths := make([]int, len(model.columns))
	budget := model.contentWidth()
	for index, column := range model.columns {
		indices[index] = index
		widths[index] = column.MinWidth
		for _, row := range model.visible {
			widths[index] = min(48, max(widths[index], ansi.StringWidth(cell(row, index))))
		}
	}
	// Reserve 2 cells for the mark/cursor prefix inside the content box.
	budget = max(1, budget-2)
	for totalWidth(widths) > budget && len(indices) > 1 {
		removeAt := 1
		for index := 2; index < len(indices); index++ {
			if model.columns[indices[index]].Priority > model.columns[indices[removeAt]].Priority {
				removeAt = index
			}
		}
		indices = append(indices[:removeAt], indices[removeAt+1:]...)
		widths = append(widths[:removeAt], widths[removeAt+1:]...)
	}
	if totalWidth(widths) > budget && len(widths) > 0 {
		widths[0] = max(1, widths[0]-(totalWidth(widths)-budget))
	}
	return indices, widths
}

func totalWidth(widths []int) int {
	if len(widths) == 0 {
		return 0
	}
	total := len(widths) - 1
	for _, width := range widths {
		total += width
	}
	return total
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0, 8)
	for value > 0 {
		digits = append(digits, byte('0'+value%10))
		value /= 10
	}
	for left, right := 0, len(digits)-1; left < right; left, right = left+1, right-1 {
		digits[left], digits[right] = digits[right], digits[left]
	}
	return string(digits)
}
