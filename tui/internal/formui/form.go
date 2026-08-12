package formui

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/asheshgoplani/agent-deck/internal/i18n"
	"github.com/charmbracelet/lipgloss"
)

type Kind int

const (
	Text Kind = iota
	Secret
	Integer
	Toggle
	Select
	MultiSelect
	Reference
	Repeater
	ReadOnly
)

type Option struct {
	Label    string
	Value    string
	Selected bool
}

type Field struct {
	ID          string
	Label       string
	Value       string
	Display     string
	Placeholder string
	Help        string
	Unit        string
	Section     string
	Kind        Kind
	Required    bool
	ReadOnly    bool
	Advanced    bool
	Expanded    bool
	Options     []Option
	OptionIndex int
	Error       string
	EmptyText   string
}

type Spec struct {
	Title            string
	Context          string
	Notice           string
	Fields           []Field
	Focus            int
	AdvancedExpanded bool
	Width            int
	Height           int
	Scroll           int
	Footer           string
}

type Layout struct {
	View        string
	Scroll      int
	FieldLines  map[string]int
	OptionLines map[int]int
	FooterLine  int
}

// CycleValue returns the adjacent fixed choice, wrapping at both ends.
func CycleValue(current string, options []string, delta int) (string, bool) {
	if len(options) == 0 || delta == 0 {
		return current, false
	}
	index := -1
	for i, option := range options {
		if option == current {
			index = i
			break
		}
	}
	if index < 0 {
		if delta < 0 {
			return options[len(options)-1], true
		}
		return options[0], true
	}
	index = (index + delta) % len(options)
	if index < 0 {
		index += len(options)
	}
	return options[index], true
}

const (
	preferredWidth = 84
	minimumWidth   = 44
	terminalGutter = 4
	borderPad      = 8
)

var colors = struct {
	cyan, text, muted, purple, surface, input, red lipgloss.Color
}{
	cyan: "#7dcfff", text: "#c0caf5", muted: "#a9b1d6", purple: "#bb9af7",
	surface: "#24283b", input: "#1a1b26", red: "#f7768e",
}

// DialogFrame is the shared New Session/resource-form dialog shell.
func DialogFrame(content string, width, verticalPadding, horizontalPadding int) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colors.cyan).
		Background(colors.surface).
		Padding(verticalPadding, horizontalPadding).
		Width(width).
		Render(content)
}

func Render(spec Spec) Layout {
	width := effectiveWidth(spec.Width)
	innerWidth := max(20, width-borderPad)
	visibleFields := make([]Field, 0, len(spec.Fields))
	for _, field := range spec.Fields {
		if !field.Advanced || spec.AdvancedExpanded {
			visibleFields = append(visibleFields, field)
		}
	}
	if spec.Focus < 0 {
		spec.Focus = 0
	}
	if spec.Focus >= len(visibleFields) && len(visibleFields) > 0 {
		spec.Focus = len(visibleFields) - 1
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(colors.cyan)
	contextStyle := lipgloss.NewStyle().Foreground(colors.purple)
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(colors.purple)
	labelStyle := lipgloss.NewStyle().Foreground(colors.text)
	activeStyle := labelStyle.Foreground(colors.cyan).Bold(true)
	helpStyle := lipgloss.NewStyle().Foreground(colors.muted).Faint(true)
	errorStyle := lipgloss.NewStyle().Foreground(colors.red)
	optionStyle := lipgloss.NewStyle().Foreground(colors.muted)
	selectedOptionStyle := lipgloss.NewStyle().Foreground(colors.cyan).Bold(true)
	stacked := innerWidth < 64
	labelWidth := min(22, max(14, innerWidth/3))
	controlWidth := max(12, innerWidth-2)
	if !stacked {
		controlWidth = max(20, innerWidth-labelWidth)
	}
	inputStyle := lipgloss.NewStyle().Foreground(colors.text).Background(colors.surface).Width(max(6, controlWidth))
	dropdownStyle := lipgloss.NewStyle().Foreground(colors.text).Background(colors.surface).Width(max(6, controlWidth))
	activeDropdownStyle := dropdownStyle.Foreground(colors.cyan).Bold(true)
	readOnlyStyle := inputStyle.Foreground(colors.muted).Faint(true)

	headerLines := []string{titleStyle.Render(spec.Title)}
	if spec.Context != "" {
		headerLines = append(headerLines, contextStyle.Render(spec.Context))
	}
	if spec.Notice != "" {
		headerLines = append(headerLines, errorStyle.Render(truncate(spec.Notice, innerWidth)))
	}
	headerLines = append(headerLines, "")
	lines := make([]string, 0, len(visibleFields)*2)
	fieldLines := map[string]int{}
	optionLines := map[int]int{}
	lastSection := ""
	focusLine := 0
	for index, field := range visibleFields {
		if field.Section != "" && field.Section != lastSection {
			lines = append(lines, sectionStyle.Render(sectionRule(localized("form.section."+strings.ToLower(strings.ReplaceAll(field.Section, " ", "_")), field.Section), innerWidth)))
			lastSection = field.Section
		}
		active := index == spec.Focus
		marker := "  "
		style := labelStyle
		if active {
			marker = "▶ "
			style = activeStyle
		}
		required := ""
		if field.Required {
			required = " *"
		}
		labelText := marker + field.Label + required
		if !stacked {
			labelText = truncate(labelText, labelWidth)
		}
		label := style.Render(labelText)
		valueStyle := inputStyle
		if isDropdown(field.Kind) {
			valueStyle = dropdownStyle
			if active {
				valueStyle = activeDropdownStyle
			}
		} else if field.Kind == Toggle && active {
			valueStyle = valueStyle.Foreground(colors.cyan).Bold(true)
		} else if field.ReadOnly || field.Kind == ReadOnly {
			valueStyle = readOnlyStyle
		} else if strings.TrimSpace(field.Value) == "" {
			valueStyle = valueStyle.Foreground(colors.muted).Faint(true)
		}
		textWidth := max(6, controlWidth)
		control := ""
		if isEditableInput(field) {
			control = renderInputControl(field, textWidth, active)
		} else {
			control = valueStyle.Render(controlText(field, textWidth))
		}
		if stacked {
			lines = append(lines, label)
			fieldLines[field.ID] = len(lines)
			lines = append(lines, "  "+control)
		} else {
			fieldLines[field.ID] = len(lines)
			labelCell := lipgloss.NewStyle().Background(colors.surface).Width(labelWidth).Render(label)
			row := lipgloss.JoinHorizontal(lipgloss.Top, labelCell, control)
			lines = append(lines, lipgloss.NewStyle().Background(colors.surface).Width(innerWidth).Render(row))
		}
		if active {
			focusLine = len(lines) - 1
		}
		if active && field.Expanded {
			indent := "  "
			if !stacked {
				indent = strings.Repeat(" ", labelWidth)
			}
			panelWidth := max(8, controlWidth)
			panelInner := max(4, panelWidth-2)
			panelStyle := lipgloss.NewStyle().Foreground(colors.muted)
			lines = append(lines, panelStyle.Render(indent+"╭"+strings.Repeat("─", panelInner)+"╮"))
			if len(field.Options) == 0 {
				empty := field.EmptyText
				if empty == "" {
					empty = localized("form.select.empty", "No matching options")
				}
				content := truncate("  "+empty, panelInner)
				content += strings.Repeat(" ", max(0, panelInner-lipgloss.Width(content)))
				lines = append(lines, panelStyle.Render(indent+"│"+content+"│"))
			}
			for optionIndex, option := range field.Options {
				prefix := "  "
				optionRender := optionStyle
				if optionIndex == field.OptionIndex {
					prefix = "› "
					optionRender = selectedOptionStyle.Background(lipgloss.Color("#30364d"))
					focusLine = len(lines)
				}
				mark := ""
				if option.Selected && field.Kind == MultiSelect {
					mark = " [x]"
				}
				optionLines[len(lines)] = optionIndex
				content := truncate(prefix+option.Label+mark, panelInner)
				content += strings.Repeat(" ", max(0, panelInner-lipgloss.Width(content)))
				lines = append(lines, panelStyle.Render(indent+"│")+optionRender.Render(content)+panelStyle.Render("│"))
			}
			lines = append(lines, panelStyle.Render(indent+"╰"+strings.Repeat("─", panelInner)+"╯"))
		}
		if field.Error != "" {
			lines = append(lines, errorStyle.Render(fieldMessageIndent(stacked, labelWidth)+field.Error))
			if active {
				focusLine = len(lines) - 1
			}
		} else if active && field.Help != "" {
			lines = append(lines, helpStyle.Render(fieldMessageIndent(stacked, labelWidth)+truncate(field.Help, max(8, controlWidth))))
			focusLine = len(lines) - 1
		}
	}

	advancedCount := 0
	for _, field := range spec.Fields {
		if field.Advanced {
			advancedCount++
		}
	}
	if advancedCount > 0 && !spec.AdvancedExpanded {
		advanced := i18n.T("form.advanced.collapsed", map[string]string{"count": fmt.Sprintf("%d", advancedCount)})
		if advanced == "form.advanced.collapsed" {
			advanced = fmt.Sprintf("Advanced · %d fields · Tab to show", advancedCount)
		}
		lines = append(lines, "", helpStyle.Render("  "+advanced))
	}

	footer := spec.Footer
	if footer == "" {
		footer = localized("form.footer.default", "Tab fields  Enter select  Ctrl+S save  Esc cancel")
	}
	footerRows := groupedFooter(footer, innerWidth)
	bodyHeight := spec.Height - 6 - len(headerLines) - len(footerRows)
	if bodyHeight < 8 {
		bodyHeight = 8
	}
	visible, scroll := viewport(lines, bodyHeight, focusLine, spec.Scroll)
	footerRule := helpStyle.Render(strings.Repeat("─", max(8, innerWidth)))
	contentLines := append(append(headerLines, visible...), footerRule)
	for _, row := range footerRows {
		contentLines = append(contentLines, helpStyle.Render(row))
	}
	content := strings.Join(contentLines, "\n")
	box := DialogFrame(content, width, 1, 3)
	boxHeight := lipgloss.Height(box)
	originY := 0
	if spec.Height > boxHeight {
		originY = (spec.Height - boxHeight) / 2
	}
	bodyOrigin := originY + 2 + len(headerLines)
	absoluteFields := make(map[string]int, len(fieldLines))
	for id, line := range fieldLines {
		if line >= scroll && line < scroll+len(visible) {
			absoluteFields[id] = bodyOrigin + line - scroll
		}
	}
	absoluteOptions := make(map[int]int, len(optionLines))
	for line, option := range optionLines {
		if line >= scroll && line < scroll+len(visible) {
			absoluteOptions[bodyOrigin+line-scroll] = option
		}
	}
	footerLine := bodyOrigin + len(visible) + 1
	if spec.Width > 0 && spec.Height > 0 {
		box = lipgloss.Place(spec.Width, spec.Height, lipgloss.Center, lipgloss.Center, box)
	}
	return Layout{View: box, Scroll: scroll, FieldLines: absoluteFields, OptionLines: absoluteOptions, FooterLine: footerLine}
}

func displayValue(field Field) string {
	value := field.Value
	if field.Display != "" {
		value = field.Display
	}
	if field.Kind == Secret && value != "" && !maskedValue(value) {
		value = strings.Repeat("*", min(16, max(8, len([]rune(value)))))
	}
	if value == "" {
		value = fieldPlaceholder(field)
	}
	if field.Unit != "" && strings.TrimSpace(value) != "" {
		value += " " + field.Unit
	}
	if field.ReadOnly || field.Kind == ReadOnly {
		value += "  (read-only)"
	}
	return value
}

func controlText(field Field, width int) string {
	if isDropdown(field.Kind) {
		return dropdownText(field, width)
	}
	if field.Kind == Toggle {
		return toggleText(field, width)
	}
	usable := max(3, width-2)
	return truncate(displayValue(field), usable)
}

func toggleText(field Field, width int) string {
	enabled := strings.EqualFold(strings.TrimSpace(field.Value), "true")
	mark := "[ ]"
	state := localized("form.value.off", "Off")
	if enabled {
		mark = "[x]"
		state = localized("form.value.on", "On")
	}
	return truncate(mark+" "+state, max(3, width))
}

func renderInputControl(field Field, width int, active bool) string {
	inputBackground := lipgloss.NewStyle().Background(colors.input)
	promptStyle := inputBackground.Foreground(colors.muted)
	valueStyle := inputBackground.Foreground(colors.text)
	if active {
		promptStyle = promptStyle.Foreground(colors.cyan).Bold(true)
	}
	prompt := promptStyle.Render("> ")
	usable := max(1, width-2)
	value := truncate(displayValue(field), usable)
	placeholder := strings.TrimSpace(field.Value) == ""
	if !active {
		if placeholder {
			valueStyle = valueStyle.Foreground(colors.muted).Faint(true)
		}
		return inputBackground.Width(width).Render(prompt + valueStyle.Render(value))
	}

	cursorStyle := lipgloss.NewStyle().Foreground(colors.input).Background(colors.text)
	if placeholder {
		runes := []rune(value)
		if len(runes) == 0 {
			return inputBackground.Width(width).Render(prompt + cursorStyle.Render(" "))
		}
		first := cursorStyle.Render(string(runes[0]))
		rest := inputBackground.Foreground(colors.muted).Faint(true).Render(string(runes[1:]))
		return inputBackground.Width(width).Render(prompt + first + rest)
	}
	value = truncate(value, max(1, usable-1))
	return inputBackground.Width(width).Render(prompt + valueStyle.Render(value) + cursorStyle.Render(" "))
}

func dropdownText(field Field, width int) string {
	usable := max(4, width-4)
	value := truncate(displayValue(field), max(1, usable-2))
	content := value + strings.Repeat(" ", max(1, usable-lipgloss.Width(value)-1)) + "▾"
	return "[ " + content + " ]"
}

func isDropdown(kind Kind) bool {
	return kind == Select || kind == Reference || kind == MultiSelect
}

func isEditableInput(field Field) bool {
	if field.ReadOnly {
		return false
	}
	return field.Kind == Text || field.Kind == Secret || field.Kind == Integer
}

func fieldPlaceholder(field Field) string {
	if field.Placeholder != "" {
		return field.Placeholder
	}
	key := "form.placeholder.type"
	switch field.Kind {
	case Secret:
		key = "form.placeholder.secret"
	case Integer:
		key = "form.placeholder.integer"
	case Select, Reference:
		key = "form.placeholder.choose"
	case MultiSelect:
		key = "form.placeholder.multi"
	case Repeater:
		key = "form.placeholder.edit_list"
	case Toggle:
		return localized("form.value.off", "Off")
	}
	value := i18n.T(key, map[string]string{"field": field.Label})
	if value == key {
		return "Enter " + strings.ToLower(field.Label) + "..."
	}
	return value
}

func fieldMessageIndent(stacked bool, labelWidth int) string {
	if stacked {
		return "  "
	}
	return strings.Repeat(" ", labelWidth)
}

func groupedFooter(footer string, width int) []string {
	parts := strings.Split(footer, "  ")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			clean = append(clean, part)
		}
	}
	rows := []string{}
	row := ""
	for _, part := range clean {
		candidate := part
		if row != "" {
			candidate = row + "  │  " + part
		}
		if row != "" && lipgloss.Width(candidate) > width {
			rows = append(rows, row)
			row = part
		} else {
			row = candidate
		}
	}
	if row != "" {
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return []string{""}
	}
	return rows
}

func maskedValue(value string) bool {
	for _, r := range value {
		if r != '•' && r != '*' {
			return false
		}
	}
	return value != ""
}

func effectiveWidth(terminalWidth int) int {
	if terminalWidth <= 0 {
		return preferredWidth
	}
	available := terminalWidth - terminalGutter
	if available < minimumWidth {
		return max(20, available)
	}
	return min(preferredWidth, available)
}

func sectionRule(label string, width int) string {
	prefix := "  " + label + "  "
	ruleWidth := min(width, 38)
	return prefix + strings.Repeat("─", max(4, ruleWidth-lipgloss.Width(prefix)))
}

func viewport(lines []string, height, focus, scroll int) ([]string, int) {
	if len(lines) <= height {
		out := append([]string(nil), lines...)
		for len(out) < height {
			out = append(out, "")
		}
		return out, 0
	}
	if scroll < 0 {
		scroll = 0
	}
	if focus < scroll {
		scroll = focus
	}
	if focus >= scroll+height {
		scroll = focus - height + 1
	}
	if scroll+height > len(lines) {
		scroll = len(lines) - height
	}
	return append([]string(nil), lines[scroll:scroll+height]...), scroll
}

func FriendlyLabel(name string) string {
	if label := i18n.T("form.field." + name); label != "form.field."+name {
		return label
	}
	known := map[string]string{
		"id": "Resource ID", "name": "Name", "launcher": "CLI", "default_route_id": "Traffic route",
		"model_projection_id": "Model list", "compatibility_transform_ids": "Compatibility transforms",
		"native_config_root": "Native config location", "base_url": "Base URL", "api_key": "API key",
		"allow_private_network": "Allow private network",
		"token":                 "API token", "primary_target_id": "Primary upstream", "backup_target_id": "Backup upstream",
		"model_strategy": "Model handling", "upstream_model": "Upstream model", "routing": "Routing strategy",
		"queue_timeout_ms": "Queue timeout", "max_concurrency": "Maximum concurrency", "rpm": "Requests per minute",
	}
	if label, ok := known[name]; ok {
		return label
	}
	parts := strings.Split(strings.TrimSuffix(name, "_id"), "_")
	for index, part := range parts {
		if part == "" {
			continue
		}
		runes := []rune(part)
		runes[0] = unicode.ToUpper(runes[0])
		parts[index] = string(runes)
	}
	return strings.Join(parts, " ")
}

func CleanError(message string) (field, detail string) {
	message = strings.TrimSpace(message)
	if !strings.HasPrefix(message, "$.") {
		return "", message
	}
	parts := strings.SplitN(strings.TrimPrefix(message, "$."), ":", 2)
	field = parts[0]
	if bracket := strings.IndexByte(field, '['); bracket >= 0 {
		field = field[:bracket]
	}
	detail = "Invalid value"
	if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
		detail = strings.TrimSpace(parts[1])
	}
	detail = localizeError(detail)
	return field, detail
}

func localized(key, fallback string) string {
	if value := i18n.T(key); value != key {
		return value
	}
	return fallback
}

func localizeError(detail string) string {
	switch {
	case detail == "required":
		return localized("form.error.required", detail)
	case strings.Contains(detail, "invalid integer"):
		return localized("form.error.integer", detail)
	case strings.Contains(detail, "invalid reference") || detail == "unknown":
		return localized("form.error.reference", detail)
	case strings.Contains(detail, "duplicate_id"):
		return localized("form.error.duplicate_id", detail)
	case strings.Contains(detail, "must match ConfigID"):
		return localized("form.error.config_id", detail)
	default:
		return detail
	}
}

var invalidID = regexp.MustCompile(`[^a-z0-9_-]+`)

func UniqueID(name, prefix string, used map[string]bool) string {
	base := strings.ToLower(strings.TrimSpace(name))
	base = invalidID.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-_")
	if base == "" || base[0] < 'a' || base[0] > 'z' {
		base = strings.Trim(prefix+"-"+base, "-_")
	}
	if len(base) > 63 {
		base = strings.Trim(base[:63], "-_")
	}
	for suffix := 1; ; suffix++ {
		candidate := base
		if suffix > 1 {
			ending := fmt.Sprintf("-%d", suffix)
			candidate = strings.TrimRight(base[:min(len(base), 63-len(ending))], "-_") + ending
		}
		if !used[candidate] {
			return candidate
		}
	}
}

func truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width == 1 {
		return string(runes[:1])
	}
	if width <= 3 {
		return string(runes[:width])
	}
	return string(runes[:width-3]) + "..."
}
