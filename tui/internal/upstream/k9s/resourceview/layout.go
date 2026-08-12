package resourceview

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type tableLayout struct {
	borderLine     int
	headerLine     int
	bodyStartLine  int
	bodyEndLine    int
	separatorLine  int
	footerLine     int
	bottomLine     int
	contentWidth   int
	scrollStart    int
	visibleColumns []int
	widths         []int
	rowRegions     []rect
	headerRegions  []rect
	footerFilter   rect
	footerActions  []footerActionRegion
	borderTitle    string
}

type footerActionRegion struct {
	action Action
	region rect
}

func (model *Model) contentWidth() int {
	return max(1, model.width-2)
}

func (model *Model) computeLayout() tableLayout {
	layout := tableLayout{
		borderLine:    0,
		headerLine:    1,
		bodyStartLine: 2,
		footerLine:    max(1, model.height-2),
		bottomLine:    max(1, model.height-1),
		contentWidth:  model.contentWidth(),
		borderTitle:   fmt.Sprintf("%s(%s)[%d]", model.title, model.scope, len(model.visible)),
	}
	layout.separatorLine = -1
	if model.height >= 6 {
		layout.separatorLine = layout.footerLine - 1
		layout.bodyEndLine = max(layout.bodyStartLine, layout.separatorLine-1)
	} else {
		layout.bodyEndLine = max(layout.bodyStartLine, layout.footerLine-1)
	}
	availableRows := max(0, layout.bodyEndLine-layout.bodyStartLine+1)
	layout.scrollStart = 0
	if model.cursor >= availableRows && availableRows > 0 {
		layout.scrollStart = model.cursor - availableRows + 1
	}

	layout.visibleColumns, layout.widths = model.layoutColumns()
	layout.rowRegions = make([]rect, 0, availableRows)
	for index := layout.scrollStart; index < min(len(model.visible), layout.scrollStart+availableRows); index++ {
		line := layout.bodyStartLine + (index - layout.scrollStart)
		layout.rowRegions = append(layout.rowRegions, rect{x: 1, y: line, width: layout.contentWidth, height: 1})
	}

	// Left border (1) + row mark prefix (2).
	x := 1 + 2
	layout.headerRegions = make([]rect, len(layout.visibleColumns))
	for visibleIndex, columnIndex := range layout.visibleColumns {
		width := layout.widths[visibleIndex]
		layout.headerRegions[visibleIndex] = rect{x: x, y: layout.headerLine, width: width, height: 1}
		x += width
		if visibleIndex < len(layout.visibleColumns)-1 {
			x++
		}
		_ = columnIndex
	}

	filterStart, filterWidth := model.footerFilterSpan()
	layout.footerFilter = rect{x: 1 + filterStart, y: layout.footerLine, width: filterWidth, height: 1}
	layout.footerActions = model.footerActionRegions(layout.footerLine)
	return layout
}

func (model *Model) footerActionRegions(line int) []footerActionRegion {
	segments := model.footerActionSegments()
	regions := make([]footerActionRegion, 0, len(segments))
	for _, segment := range segments {
		regions = append(regions, footerActionRegion{
			action: segment.action,
			region: rect{x: 1 + segment.start, y: line, width: segment.width, height: 1},
		})
	}
	return regions
}

type footerSegment struct {
	action Action
	start  int
	width  int
}

type footerActionLabel struct {
	action Action
	key    string
	desc   string
}

func (model *Model) footerActionLabels() []footerActionLabel {
	candidates := []struct {
		action Action
		key    string
		desc   string
		show   bool
	}{
		{ActionCreate, "n", "new", model.footerActions.Create},
		{ActionEdit, "e", "edit", model.footerActions.Edit},
		{ActionDelete, "d", "del", model.footerActions.Delete},
		{ActionPublish, "^s", "pub", model.footerActions.Publish},
	}
	labels := make([]footerActionLabel, 0, len(candidates))
	for _, entry := range candidates {
		if entry.show {
			labels = append(labels, footerActionLabel{action: entry.action, key: entry.key, desc: entry.desc})
		}
	}
	return labels
}

func footerChipWidth(key, desc string) int {
	// Padding(0,1) around key + spacer + desc.
	return ansi.StringWidth(key) + 2 + 1 + ansi.StringWidth(desc)
}

func (model *Model) footerActionSegments() []footerSegment {
	cursor := ansi.StringWidth(model.footerPrefix())
	labels := model.footerActionLabels()
	segments := make([]footerSegment, 0, len(labels))
	sepWidth := ansi.StringWidth(" │ ")
	for index, entry := range labels {
		if index > 0 {
			cursor += sepWidth
		}
		width := footerChipWidth(entry.key, entry.desc)
		segments = append(segments, footerSegment{action: entry.action, start: cursor, width: width})
		cursor += width
	}
	return segments
}

func (model *Model) footerPrefix() string {
	status := "0/0"
	if len(model.visible) > 0 {
		status = strings.Join([]string{itoa(model.cursor + 1), itoa(len(model.visible))}, "/")
	}
	return status + " "
}

func (model *Model) footerFilterSpan() (int, int) {
	if !model.footerActions.Filter {
		return 0, 0
	}
	start := ansi.StringWidth(model.footerPrefix())
	segments := model.footerActionSegments()
	sepWidth := ansi.StringWidth(" │ ")
	if len(segments) > 0 {
		last := segments[len(segments)-1]
		start = last.start + last.width + sepWidth
	}
	return start, footerChipWidth("/", "filter")
}

func (model *Model) renderBorder(layout tableLayout) string {
	title := layout.borderTitle
	if model.dirty {
		title = "*" + title
	}
	if model.filter != "" || model.filtering {
		title += "  /" + model.filter
		if model.filtering {
			title += "_"
		}
	}
	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(TokenBorder))
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(TokenHeader))

	// ┌─ Title ────────┐  (total width = model.width)
	prefix := borderStyle.Render("┌─ ")
	renderedTitle := titleStyle.Render(title)
	gap := " "
	topBudget := max(0, model.width-ansi.StringWidth("┌─ ")-ansi.StringWidth(title)-ansi.StringWidth(gap)-ansi.StringWidth("┐"))
	suffix := borderStyle.Render(gap + strings.Repeat("─", topBudget) + "┐")
	return ansi.Truncate(prefix+renderedTitle+suffix, model.width, "")
}

func (model *Model) renderSeparator(layout tableLayout) string {
	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(TokenBorder))
	inner := strings.Repeat("─", layout.contentWidth)
	return borderStyle.Render("├" + inner + "┤")
}

func (model *Model) renderBottom(layout tableLayout) string {
	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(TokenBorder))
	inner := strings.Repeat("─", layout.contentWidth)
	return borderStyle.Render("└" + inner + "┘")
}

func (model *Model) wrapContent(content string, layout tableLayout) string {
	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(TokenBorder))
	left := borderStyle.Render("│")
	right := borderStyle.Render("│")
	padded := content
	width := ansi.StringWidth(content)
	if width < layout.contentWidth {
		padded += strings.Repeat(" ", layout.contentWidth-width)
	} else if width > layout.contentWidth {
		padded = ansi.Truncate(content, layout.contentWidth, "")
	}
	return left + padded + right
}

func (model *Model) emptyContentLine(layout tableLayout) string {
	return model.wrapContent(strings.Repeat(" ", layout.contentWidth), layout)
}

func (model *Model) cellColor(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "healthy", "ok", "connected":
		return TokenHealthy
	case "degraded", "warning", "stale":
		return TokenWarning
	case "error", "failed", "disconnected":
		return TokenError
	default:
		return TokenText
	}
}
