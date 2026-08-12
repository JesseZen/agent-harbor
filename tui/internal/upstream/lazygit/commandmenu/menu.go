// Package commandmenu is a Bubble Tea port of lazygit's temporary menu model,
// with Commands chrome ported from SpaceXAI Grok Build's pager modal/picker.
//
// Interaction upstream: github.com/jesseduffield/lazygit v0.63.1
// Commit: aafe61082e7ed383d318fd40e48f85645e6afc7b
// License: MIT, Copyright (c) 2018 Jesse Duffield.
//
// Visual chrome upstream: github.com/xai-org/grok-build (Apache-2.0)
// Ported from:
//   crates/codegen/xai-grok-pager/src/views/modal_window.rs
//   crates/codegen/xai-grok-pager/src/views/picker.rs
//   crates/codegen/xai-grok-pager/src/app/modals.rs (CommandPalette arm)
//   crates/codegen/xai-grok-pager-render/src/theme/groknight.rs
package commandmenu

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"
)

// Grok layout tokens (picker.rs / modal_window.rs). Colors live in chrome.go.
const (
	// picker.rs SEARCH_BAR_LABEL
	searchBarLabel = " search: "
	// picker.rs leaf marker (glyphs::diamond_filled)
	rowDiamond = "◆ "
	// modal_window.rs close mark (ballot_x); use ASCII form for broad TTY coverage
	closeMark = "[x]"
	// modal_window.rs render_modal_shortcuts separator
	footerSep = "  |  "
	// picker.rs ScrollDown/ScrollUp step
	scrollStep = 3
	// modal_window.rs close button cell count: " [x] "
	closeWidth = 5
)

// CommandPalette ModalSizing from modals.rs (non-compact).
const (
	paletteWidthPct    = 0.50
	paletteMaxWidth    = 80
	paletteMinWidth    = 44
	paletteVMargin     = 4
	paletteHPad        = 2
	paletteVPad        = 1
	paletteFooterLines = 2
)

// rect is a screen-space hit target (Grok ratatui::layout::Rect subset).
type rect struct {
	X, Y, W, H int
}

func (r rect) contains(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// hitAreas ports PickerHitAreas + ModalWindowState close/popup rects.
type hitAreas struct {
	popup       rect
	closeButton rect
	list        rect
	// itemRows[i] is the screen rect for a selectable row; itemIndex[i] is
	// the filtered-index for that row (picker entry_indices).
	itemRows  []rect
	itemIndex []int
	totalLines int
	listHeight int
}

type Widget int

const (
	NoWidget Widget = iota
	RadioSelected
	RadioUnselected
	CheckboxSelected
	CheckboxUnselected
)

type Item struct {
	ID             string
	LabelColumns   []string
	Keys           []string
	// Shortcut is the right-column command hint (Grok PaletteEntry.shortcut),
	// e.g. "/theme", "Ctrl+N", "Ctrl+P → worktree". Not the menu quick-select Keys.
	Shortcut       string
	Section        string
	DisabledReason string
	Widget         Widget
	OpensMenu      bool
	// KeepOpen leaves the palette open after select (host runs the action in place).
	KeepOpen bool
	Aliases  []string
}

type Options struct {
	Title                     string
	Prompt                    string
	Items                     []Item
	HideCancel                bool
	AllowFilteringKeybindings bool
	EmptyText                 string
	FooterText                string
	FilterBadge               string
}

type Result struct {
	SelectedID     string
	DisabledReason string
	Closed         bool
}

type Model struct {
	title                     string
	prompt                    string
	emptyText                 string
	footerText                string
	filterBadge               string
	items                     []Item
	filtered                  []int
	cursor                    int
	scroll                    int  // first visible list-line (headers + items)
	scrollPinned              bool // true after mouse-wheel (picker scroll_offset = Some)
	closeHovered              bool
	lastMouseX                int
	lastMouseY                int
	mouseSeen                 bool
	hits                      hitAreas
	screenW                   int
	screenH                   int
	open                      bool
	filtering                 bool
	filter                    string
	allowFilteringKeybindings bool
}

func New(options Options) *Model {
	items := cloneItems(options.Items)
	if !options.HideCancel {
		items = append(items, Item{ID: "__cancel", LabelColumns: []string{"Cancel"}, Keys: []string{"esc"}})
	}
	for index := range items {
		if len(items[index].LabelColumns) == 0 {
			items[index].LabelColumns = []string{items[index].ID}
		}
		for len(items[index].LabelColumns) < 2 {
			items[index].LabelColumns = append(items[index].LabelColumns, "")
		}
	}
	empty := options.EmptyText
	if empty == "" {
		empty = "No matching commands"
	}
	footer := options.FooterText
	if footer == "" {
		// modals.rs CommandPalette shortcuts: ↑/↓ nav | Enter select | Esc close
		footer = "↑/↓ nav" + footerSep + "Enter select" + footerSep + "Esc close"
	}
	prompt := options.Prompt
	if prompt == "" {
		prompt = searchBarLabel
	}
	badge := options.FilterBadge
	model := &Model{
		title:                     options.Title,
		prompt:                    prompt,
		emptyText:                 empty,
		footerText:                footer,
		filterBadge:               badge,
		items:                     items,
		allowFilteringKeybindings: options.AllowFilteringKeybindings,
	}
	model.rebuildFilter()
	return model
}

func cloneItems(source []Item) []Item {
	items := make([]Item, len(source))
	for index := range source {
		items[index] = source[index]
		items[index].LabelColumns = append([]string(nil), source[index].LabelColumns...)
		items[index].Keys = append([]string(nil), source[index].Keys...)
		items[index].Aliases = append([]string(nil), source[index].Aliases...)
	}
	return items
}

func (model *Model) Open() {
	model.open = true
	model.filtering = true
	model.filter = ""
	model.cursor = 0
	model.scroll = 0
	model.scrollPinned = false
	model.closeHovered = false
	model.mouseSeen = false
	model.hits = hitAreas{}
	model.rebuildFilter()
}

func (model *Model) Close() {
	model.open = false
	model.filtering = false
	model.filter = ""
	model.scroll = 0
	model.scrollPinned = false
	model.closeHovered = false
	model.mouseSeen = false
	model.hits = hitAreas{}
}

func (model *Model) IsOpen() bool { return model.open }

func (model *Model) Filter() string { return model.filter }

// NavState returns filter/cursor/scroll for Grok-style palette snapshot restore.
func (model *Model) NavState() (filter string, cursor, scroll int) {
	return model.filter, model.cursor, model.scroll
}

// RestoreNav reapplies a prior NavState after rebuilding a palette frame.
func (model *Model) RestoreNav(filter string, cursor, scroll int) {
	model.filter = filter
	model.rebuildFilter()
	if len(model.filtered) == 0 {
		model.cursor = 0
	} else {
		model.cursor = min(max(0, cursor), len(model.filtered)-1)
	}
	model.scroll = max(0, scroll)
	model.scrollPinned = false
}

// SetFooterText updates the centered footer hint (e.g. Esc close vs Esc back).
func (model *Model) SetFooterText(text string) {
	model.footerText = text
}

// SelectID moves the cursor to the filtered row with the given item ID.
func (model *Model) SelectID(id string) {
	for visibleIndex, itemIndex := range model.filtered {
		if model.items[itemIndex].ID == id {
			model.cursor = visibleIndex
			return
		}
	}
}

func (model *Model) Update(message tea.Msg) Result {
	if !model.open {
		return Result{}
	}
	switch msg := message.(type) {
	case tea.MouseMsg:
		return model.updateMouse(msg)
	case tea.KeyMsg:
		return model.updateKey(msg)
	default:
		return Result{}
	}
}

func (model *Model) updateKey(key tea.KeyMsg) Result {
	model.scrollPinned = false
	switch key.Type {
	case tea.KeyEsc:
		model.Close()
		return Result{Closed: true}
	case tea.KeyBackspace, tea.KeyDelete:
		if runes := []rune(model.filter); len(runes) > 0 {
			model.filter = string(runes[:len(runes)-1])
			model.rebuildFilter()
		}
		return Result{}
	case tea.KeyEnter:
		return model.selectCurrent()
	case tea.KeyUp:
		model.move(-1)
		return Result{}
	case tea.KeyDown:
		model.move(1)
		return Result{}
	case tea.KeyTab:
		model.completeFilter()
		return Result{}
	case tea.KeyPgUp:
		model.scrollBy(-model.hits.listHeight)
		return Result{}
	case tea.KeyPgDown:
		model.scrollBy(model.hits.listHeight)
		return Result{}
	case tea.KeyRunes:
		model.filter += string(key.Runes)
		model.rebuildFilter()
		return Result{}
	}

	switch key.String() {
	case "up", "ctrl+p", "k":
		if model.filter == "" {
			model.move(-1)
		}
	case "down", "ctrl+n", "j":
		if model.filter == "" {
			model.move(1)
		}
	case "enter":
		return model.selectCurrent()
	case "tab":
		model.completeFilter()
	case "esc", "q":
		model.Close()
		return Result{Closed: true}
	default:
		if model.filter == "" {
			model.selectByKey(key.String())
		}
	}
	return Result{}
}

// updateMouse ports modal_window.rs handle_modal_mouse + picker.rs handle_picker_input mouse arm.
func (model *Model) updateMouse(msg tea.MouseMsg) Result {
	x, y := msg.X, msg.Y
	model.lastMouseX, model.lastMouseY = x, y
	model.mouseSeen = true

	if msg.Button == tea.MouseButtonWheelUp || msg.Type == tea.MouseWheelUp {
		model.scrollBy(-scrollStep)
		return Result{}
	}
	if msg.Button == tea.MouseButtonWheelDown || msg.Type == tea.MouseWheelDown {
		model.scrollBy(scrollStep)
		return Result{}
	}

	isMotion := msg.Action == tea.MouseActionMotion || msg.Type == tea.MouseMotion
	isPress := msg.Action == tea.MouseActionPress || msg.Type == tea.MouseLeft
	if !isMotion && !isPress {
		// Still remember position for render-time hover (AllMotion / odd terminals).
		return Result{}
	}
	if isPress && msg.Button != tea.MouseButtonLeft && msg.Type != tea.MouseLeft {
		return Result{}
	}

	// Hover/selection from current hit areas (previous frame geometry is fine;
	// ViewOver also re-applies from lastMouse after rebuilding hits).
	model.applyPointerHover()

	if isMotion {
		return Result{}
	}

	hits := model.hits
	if hits.popup.W == 0 {
		return Result{}
	}
	onClose := hits.closeButton.contains(x, y)
	// modal_window.rs: click close → CloseRequested
	if onClose {
		model.Close()
		return Result{Closed: true}
	}
	// modal_window.rs: click outside popup → CloseRequested
	if !hits.popup.contains(x, y) {
		model.Close()
		return Result{Closed: true}
	}
	// picker.rs: click item → Selected
	for i, row := range hits.itemRows {
		if !row.contains(x, y) {
			continue
		}
		idx := hits.itemIndex[i]
		if idx < 0 {
			return Result{}
		}
		model.cursor = idx
		model.scrollPinned = false
		return model.selectCurrent()
	}
	return Result{}
}

// applyPointerHover ports picker Moved + modal close hover using last mouse cell.
func (model *Model) applyPointerHover() (changed bool) {
	if !model.mouseSeen || model.hits.popup.W == 0 {
		return false
	}
	x, y := model.lastMouseX, model.lastMouseY
	onClose := model.hits.closeButton.contains(x, y)
	if model.closeHovered != onClose {
		model.closeHovered = onClose
		changed = true
	}
	for i, row := range model.hits.itemRows {
		if !row.contains(x, y) {
			continue
		}
		idx := model.hits.itemIndex[i]
		if idx >= 0 && idx != model.cursor {
			model.cursor = idx
			model.scrollPinned = false
			changed = true
		}
		return changed
	}
	return changed
}

func (model *Model) scrollBy(delta int) {
	model.scrollPinned = true
	listH := model.hits.listHeight
	total := model.hits.totalLines
	if listH <= 0 {
		// Geometry not ready yet — apply raw delta; ViewOver will clamp.
		model.scroll = max(0, model.scroll+delta)
		return
	}
	maxScroll := max(0, total-listH)
	model.scroll = min(maxScroll, max(0, model.scroll+delta))
}

func (model *Model) completeFilter() {
	if len(model.filtered) == 0 {
		return
	}
	item := model.items[model.filtered[model.cursor]]
	candidate := item.ID
	if candidate == "" || candidate == "__cancel" {
		if len(item.LabelColumns) > 0 {
			candidate = item.LabelColumns[0]
		}
	}
	if len(item.Aliases) > 0 {
		candidate = item.Aliases[0]
	}
	model.filter = candidate
	model.rebuildFilter()
	// Prefer exact id match after complete.
	for visibleIndex, itemIndex := range model.filtered {
		if model.items[itemIndex].ID == item.ID {
			model.cursor = visibleIndex
			return
		}
	}
}

func (model *Model) selectByKey(key string) {
	for visibleIndex, itemIndex := range model.filtered {
		for _, candidate := range model.items[itemIndex].Keys {
			if candidate == key {
				model.cursor = visibleIndex
				return
			}
		}
	}
}

func (model *Model) selectCurrent() Result {
	if len(model.filtered) == 0 {
		return Result{}
	}
	item := model.items[model.filtered[model.cursor]]
	if item.DisabledReason != "" {
		return Result{DisabledReason: item.DisabledReason}
	}
	if item.ID == "__cancel" {
		model.Close()
		return Result{Closed: true}
	}
	// OpensMenu / KeepOpen: keep chrome open so the host can push a stack
	// frame, swap items, or run an in-place action without resetting nav.
	if item.OpensMenu || item.KeepOpen {
		return Result{SelectedID: item.ID}
	}
	model.Close()
	return Result{SelectedID: item.ID, Closed: true}
}

func (model *Model) move(delta int) {
	if len(model.filtered) == 0 {
		model.cursor = 0
		return
	}
	model.cursor = (model.cursor + delta + len(model.filtered)) % len(model.filtered)
}

func (model *Model) rebuildFilter() {
	model.filtered = model.filtered[:0]
	query := strings.TrimSpace(model.filter)
	keySearch := model.allowFilteringKeybindings && strings.HasPrefix(query, "@")
	if keySearch {
		query = strings.TrimPrefix(query, "@")
	}
	candidates := make([]string, len(model.items))
	for index, item := range model.items {
		if keySearch {
			candidates[index] = strings.Join(item.Keys, " ")
			continue
		}
		parts := []string{item.ID, item.Section, item.Shortcut}
		parts = append(parts, item.LabelColumns...)
		parts = append(parts, item.Aliases...)
		candidates[index] = strings.Join(parts, " ")
	}
	if query == "" {
		for index := range model.items {
			model.filtered = append(model.filtered, index)
		}
	} else {
		matches := fuzzy.Find(query, candidates)
		for _, match := range matches {
			model.filtered = append(model.filtered, match.Index)
		}
	}
	if model.cursor >= len(model.filtered) {
		model.cursor = max(0, len(model.filtered)-1)
	}
	model.scroll = min(model.scroll, max(0, len(model.filtered)-1))
}

// computeCommandPaletteDims ports modal_window.rs compute_modal_dims with the
// CommandPalette ModalSizing from modals.rs (width_pct 0.50, max 80, min 44,
// v_margin 4).
func computeCommandPaletteDims(areaWidth, areaHeight int) (modalWidth, modalHeight int) {
	if areaWidth < 0 {
		areaWidth = 0
	}
	if areaHeight < 0 {
		areaHeight = 0
	}
	maxWidth := min(max(0, areaWidth-4), paletteMaxWidth)
	preferred := int(float64(areaWidth) * paletteWidthPct)
	modalWidth = preferred
	if modalWidth > maxWidth {
		modalWidth = maxWidth
	}
	if modalWidth < paletteMinWidth {
		modalWidth = paletteMinWidth
	}
	if modalWidth > areaWidth {
		modalWidth = areaWidth
	}
	modalHeight = max(0, areaHeight-paletteVMargin*2)
	return modalWidth, modalHeight
}

func (model *Model) ViewOver(background string, width, height int) string {
	if !model.open {
		return background
	}
	model.screenW, model.screenH = width, height
	panelWidth, panelHeight := computeCommandPaletteDims(width, height)
	// modal_window.rs: refuse to render a meaningful popup below 20×6.
	if panelWidth < 20 || panelHeight < 6 {
		model.hits = hitAreas{}
		return background
	}
	// Two-pass render: first pass builds hit geometry; pointer hover may change
	// selection / [x] highlight, which requires a second paint (Grok updates
	// hover state against the current frame's hit targets).
	panel := model.renderPanel(panelWidth, panelHeight, 0, 0)
	panelX, panelY := overlayOrigin(panel, width, height)
	model.offsetHits(panelX, panelY)
	if model.applyPointerHover() {
		panel = model.renderPanel(panelWidth, panelHeight, 0, 0)
		panelX, panelY = overlayOrigin(panel, width, height)
		model.offsetHits(panelX, panelY)
	}
	return overlay(background, panel, width, height)
}

func overlayOrigin(panel string, width, height int) (x, y int) {
	actualW := lipgloss.Width(panel)
	actualH := lipgloss.Height(panel)
	return max(0, (width-actualW)/2), max(0, (height-actualH)/2)
}

func (model *Model) offsetHits(dx, dy int) {
	model.hits.popup.X += dx
	model.hits.popup.Y += dy
	model.hits.closeButton.X += dx
	model.hits.closeButton.Y += dy
	model.hits.list.X += dx
	model.hits.list.Y += dy
	for i := range model.hits.itemRows {
		model.hits.itemRows[i].X += dx
		model.hits.itemRows[i].Y += dy
	}
}

type listLine struct {
	text      string
	itemIndex int // filtered index, or -1 for section header / empty
}

// renderPanel ports Grok CommandPalette chrome:
// square border + title on top border + [x], search bar, divider, ◆ rows,
// section headers, centered footer shortcuts. Height fills the modal
// (compute_modal_dims); overflowing list rows scroll + scrollbar.
func (model *Model) renderPanel(panelWidth, panelHeight, panelX, panelY int) string {
	hPad := paletteHPad
	innerWidth := max(1, panelWidth-2)
	innerHeight := max(1, panelHeight-2)

	border := lipgloss.NewStyle().Foreground(chromeGrayDim()).Background(chromeBGBase())
	bg := lipgloss.NewStyle().Background(chromeBGBase())
	blank := bg.Render(strings.Repeat(" ", innerWidth))

	contentHeight := max(0, innerHeight-paletteVPad-paletteFooterLines)
	listHeight := max(0, contentHeight-2)

	probe, _ := model.buildListLines(max(1, innerWidth-2*hPad))
	showScrollbar := len(probe) > listHeight
	textWidth := max(1, innerWidth-2*hPad)
	if showScrollbar {
		textWidth = max(1, textWidth-1)
	}
	listLines, cursorLine := model.buildListLines(textWidth)
	model.ensureScroll(cursorLine, listHeight, len(listLines))

	var body []string
	for i := 0; i < paletteVPad; i++ {
		body = append(body, blank)
	}
	body = append(body, padLine(model.renderSearchRow(textWidth), hPad, innerWidth))
	body = append(body, border.Render(strings.Repeat("─", innerWidth)))

	listStartBody := len(body)
	start := model.scroll
	if start > len(listLines) {
		start = 0
		model.scroll = 0
	}
	end := min(len(listLines), start+listHeight)

	closeX := panelX + panelWidth - closeWidth - 2
	model.hits = hitAreas{
		popup:       rect{X: panelX, Y: panelY, W: panelWidth, H: panelHeight},
		closeButton: rect{X: closeX, Y: panelY, W: closeWidth, H: 1},
		list:        rect{X: panelX + 1, Y: panelY + 1 + listStartBody, W: innerWidth, H: listHeight},
		totalLines:  len(listLines),
		listHeight:  listHeight,
	}

	for row := 0; row < listHeight; row++ {
		lineIdx := start + row
		var rowText string
		itemIdx := -1
		if lineIdx < end {
			itemIdx = listLines[lineIdx].itemIndex
			if showScrollbar {
				rowText = padLine(listLines[lineIdx].text, hPad, innerWidth-1) +
					scrollbarCell(row, listHeight, start, len(listLines))
			} else {
				rowText = padLine(listLines[lineIdx].text, hPad, innerWidth)
			}
		} else if showScrollbar {
			rowText = bg.Render(strings.Repeat(" ", innerWidth-1)) + scrollbarCell(row, listHeight, start, len(listLines))
		} else {
			rowText = blank
		}
		body = append(body, rowText)
		if itemIdx >= 0 {
			model.hits.itemRows = append(model.hits.itemRows, rect{
				X: panelX + 1, Y: panelY + 1 + listStartBody + row, W: innerWidth, H: 1,
			})
			model.hits.itemIndex = append(model.hits.itemIndex, itemIdx)
		}
	}

	for i := 0; i < paletteFooterLines-1; i++ {
		body = append(body, blank)
	}
	footerWidth := max(1, innerWidth-2*hPad)
	body = append(body, padLine(model.renderFooter(footerWidth), hPad, innerWidth))

	for len(body) < innerHeight {
		body = append(body, blank)
	}
	if len(body) > innerHeight {
		body = body[:innerHeight]
		body[innerHeight-1] = padLine(model.renderFooter(footerWidth), hPad, innerWidth)
	}

	top := model.renderTopBorder(panelWidth)
	bottom := border.Render("└" + strings.Repeat("─", innerWidth) + "┘")
	side := border.Render("│")
	lines := []string{top}
	for _, row := range body {
		lines = append(lines, side+row+side)
	}
	lines = append(lines, bottom)
	return strings.Join(lines, "\n")
}

func scrollbarCell(row, viewport, offset, total int) string {
	track := lipgloss.NewStyle().Foreground(chromeScrollBG()).Background(chromeScrollBG())
	thumb := lipgloss.NewStyle().Foreground(chromeScrollFG()).Background(chromeScrollFG())
	if total <= viewport || viewport <= 0 {
		return track.Render(" ")
	}
	thumbH := max(1, viewport*viewport/total)
	maxOffset := total - viewport
	thumbY := 0
	if maxOffset > 0 {
		thumbY = offset * (viewport - thumbH) / maxOffset
	}
	if row >= thumbY && row < thumbY+thumbH {
		return thumb.Render("▐")
	}
	return track.Render(" ")
}

func (model *Model) buildListLines(contentWidth int) (lines []listLine, cursorLine int) {
	cursorLine = 0
	previousSection := ""
	if contentWidth <= 0 {
		contentWidth = 1
	}
	if len(model.filtered) == 0 {
		empty := lipgloss.NewStyle().
			Foreground(chromeGray()).
			Background(chromeBGBase()).
			Render(ansi.Truncate(model.emptyText, contentWidth, "…"))
		return []listLine{{text: padRight(empty, contentWidth), itemIndex: -1}}, 0
	}
	for visibleIndex := range model.filtered {
		item := model.items[model.filtered[visibleIndex]]
		if item.Section != "" && item.Section != previousSection {
			lines = append(lines, listLine{
				text:      model.renderSectionHeader(item.Section, contentWidth),
				itemIndex: -1,
			})
			previousSection = item.Section
		}
		if visibleIndex == model.cursor {
			cursorLine = len(lines)
		}
		label := item.LabelColumns[0]
		if widget := strings.TrimSpace(widgetLabel(item.Widget)); widget != "" {
			label = widget + " " + label
		}
		lines = append(lines, listLine{
			text: model.renderPickerRow(
				label, itemRightLabel(item), visibleIndex == model.cursor, item.DisabledReason != "", contentWidth,
			),
			itemIndex: visibleIndex,
		})
	}
	return lines, cursorLine
}

func (model *Model) ensureScroll(cursorLine, listHeight, totalLines int) {
	if listHeight <= 0 {
		model.scroll = 0
		return
	}
	maxScroll := max(0, totalLines-listHeight)
	if model.scrollPinned {
		model.scroll = min(maxScroll, max(0, model.scroll))
		return
	}
	if cursorLine < model.scroll {
		model.scroll = cursorLine
	}
	if cursorLine >= model.scroll+listHeight {
		model.scroll = cursorLine - listHeight + 1
	}
	model.scroll = min(maxScroll, max(0, model.scroll))
}

func (model *Model) renderTopBorder(panelWidth int) string {
	// modal_window.rs: ┌─ Title ─…… [x] ─┐; close at width - closeWidth - 2.
	innerWidth := max(1, panelWidth-2)
	border := lipgloss.NewStyle().Foreground(chromeGrayDim()).Background(chromeBGBase())
	titleStyle := lipgloss.NewStyle().Foreground(chromeText()).Background(chromeBGBase()).Bold(true)

	title := strings.TrimSpace(model.title)
	leftDecor := "─ "
	rightDecor := " ─"
	titleBlock := ansi.StringWidth(leftDecor) + ansi.StringWidth(title) + ansi.StringWidth(rightDecor)
	closeStart := max(titleBlock, innerWidth-closeWidth-1)
	preClose := max(0, closeStart-titleBlock)
	postClose := max(0, innerWidth-closeStart-closeWidth)

	// modal_window.rs render_close_button: hover → bright + bold.
	// Use Reverse so the highlight is obvious even on limited color profiles.
	var closePainted string
	if model.closeHovered {
		pad := lipgloss.NewStyle().Background(chromeBGVisual()).Render(" ")
		mark := lipgloss.NewStyle().
			Foreground(chromeText()).
			Background(chromeBGVisual()).
			Bold(true).
			Reverse(true).
			Render(closeMark)
		closePainted = pad + mark + pad
	} else {
		closePainted = border.Render(" " + closeMark + " ")
	}

	return border.Render("┌") +
		border.Render(leftDecor) +
		titleStyle.Render(title) +
		border.Render(rightDecor) +
		border.Render(strings.Repeat("─", preClose)) +
		closePainted +
		border.Render(strings.Repeat("─", postClose)) +
		border.Render("┐")
}

func (model *Model) renderSearchRow(contentWidth int) string {
	// Grok picker.rs: inverse-video cell at the caret —
	//   Style::default().fg(bg_base).bg(text_primary)
	// Do NOT use Reverse+█: reverse swaps so █ paints with panel FG and
	// disappears into the search bar background.
	label := model.prompt
	if label == "" {
		label = searchBarLabel
	}
	labelW := ansi.StringWidth(label)
	maxQuery := max(0, contentWidth-labelW-1) // reserve 1 col for cursor
	query := model.filter
	if ansi.StringWidth(query) > maxQuery {
		query = ansi.Truncate(query, maxQuery, "")
	}
	labelStyled := lipgloss.NewStyle().
		Foreground(chromeGray()).
		Background(chromeBGBase()).
		Render(label)
	textStyled := lipgloss.NewStyle().
		Foreground(chromeText()).
		Background(chromeBGBase()).
		Render(query)
	// Full block uses the foreground ink — keep FG=text so the caret stays
	// bright. Matching BG makes it a solid Grok-style bar even if a terminal
	// ignores one of the two attributes.
	cursorStyled := lipgloss.NewStyle().
		Foreground(chromeText()).
		Background(chromeText()).
		Render("█")
	used := labelW + ansi.StringWidth(query) + 1
	pad := max(0, contentWidth-used)
	rest := lipgloss.NewStyle().Background(chromeBGBase()).Render(strings.Repeat(" ", pad))
	return labelStyled + textStyled + cursorStyled + rest
}

func (model *Model) renderSectionHeader(label string, width int) string {
	// picker.rs Header: " {label} " + remaining ─
	title := " " + label + " "
	titleW := ansi.StringWidth(title)
	if titleW >= width {
		title = ansi.Truncate(title, width, "…")
		return lipgloss.NewStyle().
			Foreground(chromeGray()).
			Background(chromeBGBase()).
			Bold(true).
			Render(title)
	}
	sep := strings.Repeat("─", width-titleW)
	return lipgloss.NewStyle().Foreground(chromeGray()).Background(chromeBGBase()).Bold(true).Render(title) +
		lipgloss.NewStyle().Foreground(chromeGrayDim()).Background(chromeBGBase()).Render(sep)
}

func (model *Model) renderPickerRow(label, right string, selected, disabled bool, width int) string {
	// picker.rs render_picker_row: ◆ + label … right_label; selected uses elevated surface.
	bg := chromeBGBase()
	labelFG := chromeText()
	metaFG := chromeGray()
	bold := false
	if selected {
		bg = chromeBGVisual()
		bold = true
	}
	diamondFG := chromeGrayDim()
	prefix := rowDiamond
	prefixW := ansi.StringWidth(prefix)
	rightW := ansi.StringWidth(right)
	gap := 0
	if rightW > 0 {
		gap = 2
	}
	maxLabel := max(1, width-prefixW-rightW-gap)
	truncated := ansi.Truncate(label, maxLabel, "…")
	pad := max(0, maxLabel-ansi.StringWidth(truncated))
	mid := truncated + strings.Repeat(" ", pad)
	if rightW > 0 {
		mid += strings.Repeat(" ", gap) + right
	}
	// Ensure full-width painted background.
	total := prefix + mid
	if w := ansi.StringWidth(total); w < width {
		total += strings.Repeat(" ", width-w)
	} else if w > width {
		total = ansi.Truncate(total, width, "")
	}

	style := lipgloss.NewStyle().Foreground(labelFG).Background(bg)
	if bold {
		style = style.Bold(true)
	}
	if disabled {
		style = style.Faint(true).Strikethrough(true)
	}
	diamondStyle := lipgloss.NewStyle().Foreground(diamondFG).Background(bg)
	metaStyle := lipgloss.NewStyle().Foreground(metaFG).Background(bg)

	// Paint segments so diamond/meta keep their fg while sharing selection bg.
	if rightW > 0 && gap > 0 {
		leftPart := truncated + strings.Repeat(" ", pad) + strings.Repeat(" ", gap)
		rightPart := right
		tail := ""
		painted := ansi.StringWidth(prefix) + ansi.StringWidth(leftPart) + ansi.StringWidth(rightPart)
		if painted < width {
			tail = strings.Repeat(" ", width-painted)
		}
		return diamondStyle.Render(prefix) + style.Render(leftPart) + metaStyle.Render(rightPart) + style.Render(tail)
	}
	return diamondStyle.Render(prefix) + style.Render(strings.TrimPrefix(total, prefix))
}

func (model *Model) renderFooter(contentWidth int) string {
	// modal_window.rs: key bold text_secondary, verb gray; joined by "  |  ", centered.
	key := lipgloss.NewStyle().Foreground(chromeTextMuted()).Background(chromeBGBase()).Bold(true)
	verb := lipgloss.NewStyle().Foreground(chromeGray()).Background(chromeBGBase())
	sep := verb.Render(footerSep)
	parts := []string{
		key.Render("↑/↓") + verb.Render(" nav"),
		key.Render("Enter") + verb.Render(" select"),
		key.Render("Esc") + verb.Render(" close"),
	}
	line := strings.Join(parts, sep)
	if ansi.StringWidth(line) > contentWidth {
		plain := "↑/↓ nav | Enter select | Esc close"
		if model.footerText != "" {
			plain = model.footerText
		}
		return verb.Render(ansi.Truncate(plain, contentWidth, "…"))
	}
	pad := max(0, contentWidth-ansi.StringWidth(line))
	left := pad / 2
	right := pad - left
	space := lipgloss.NewStyle().Background(chromeBGBase())
	return space.Render(strings.Repeat(" ", left)) + line + space.Render(strings.Repeat(" ", right))
}

func itemRightLabel(item Item) string {
	// Align with Grok CommandPalette: right side shows the command/shortcut
	// string, never the lazygit single-key quick-select binding.
	if item.Shortcut != "" {
		return item.Shortcut
	}
	return ""
}

func padLine(content string, hPad, innerWidth int) string {
	bg := lipgloss.NewStyle().Background(chromeBGBase())
	left := bg.Render(strings.Repeat(" ", hPad))
	rightPad := max(0, innerWidth-hPad-ansi.StringWidth(content))
	// content already includes its own trailing pad for rows; still fill remainder.
	return left + content + bg.Render(strings.Repeat(" ", rightPad))
}

func padRight(content string, width int) string {
	pad := max(0, width-ansi.StringWidth(content))
	if pad == 0 {
		return content
	}
	return content + lipgloss.NewStyle().Background(chromeBGBase()).Render(strings.Repeat(" ", pad))
}

func widgetLabel(widget Widget) string {
	switch widget {
	case RadioSelected:
		return "●"
	case RadioUnselected:
		return "○"
	case CheckboxSelected:
		return "[✓]"
	case CheckboxUnselected:
		return "[ ]"
	default:
		return ""
	}
}

func overlay(background, panel string, width, height int) string {
	backgroundLines := strings.Split(background, "\n")
	for len(backgroundLines) < height {
		backgroundLines = append(backgroundLines, "")
	}
	backgroundLines = backgroundLines[:height]
	panelLines := strings.Split(panel, "\n")
	panelWidth := lipgloss.Width(panel)
	panelHeight := len(panelLines)
	x := max(0, (width-panelWidth)/2)
	y := max(0, (height-panelHeight)/2)
	for index, panelLine := range panelLines {
		row := y + index
		if row >= height {
			break
		}
		base := backgroundLines[row]
		left := ansi.Cut(base, 0, x)
		left += strings.Repeat(" ", max(0, x-ansi.StringWidth(left)))
		right := ansi.Cut(base, x+panelWidth, width)
		backgroundLines[row] = ansi.Truncate(left+panelLine+right, max(1, width), "")
	}
	for index := range backgroundLines {
		backgroundLines[index] = ansi.Truncate(backgroundLines[index], max(1, width), "")
	}
	return strings.Join(backgroundLines, "\n")
}

func (model *Model) VisibleIDs() []string {
	ids := make([]string, 0, len(model.filtered))
	for _, index := range model.filtered {
		ids = append(ids, model.items[index].ID)
	}
	sort.Strings(ids)
	return ids
}
