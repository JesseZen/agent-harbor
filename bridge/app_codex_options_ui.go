package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func (m harborModel) codexOptionsPage() (uiPage, bool) {
	if m.screen != codexOptionsScreen {
		return uiPage{}, false
	}
	editor := m.codexEditor
	page := newUIPage(m.width, m.height)
	page.title(editor.title())
	actions, hint := editor.controls()
	page.shortcuts(actions, "")
	page.Hint = hint
	capacity := max(1, page.Height-len(page.shortcutBar().Lines))
	split := page.Width >= 90 && capacity >= 13 && editor.mode != "confirm" && editor.mode != "project"
	switch {
	case split:
		leftWidth := max(43, min(50, page.Width*42/100))
		left := newUIPage(leftWidth+2, capacity)
		right := newUIPage(page.Width-leftWidth-4, max(capacity, len(editor.directoryDraft)+10))
		m.codexOptionOverview(&left, true)
		focused := m.codexOptionDetail(&right, editor.mode == "")
		page.codexOptionColumns(left, right, capacity-1, focused)
		return page.finish(0), true
	case editor.mode == "":
		focused := m.codexOptionOverview(&page, false)
		editor.defaultNotes(&page)
		addCodexOptionNote(&page, codexOptionDescription(editor.field, editor.currentChoice()))
		if editor.field == optionCompact {
			addCodexOptionNote(&page, editor.compactNote())
		}
		if editor.field == optionReasoning {
			addCodexOptionNote(&page, editor.reasoningNotice())
		}
		if m.notice != "" {
			addCodexOptionNote(&page, m.notice)
			focused = len(page.Lines) - 1
		}
		return page.finish(max(0, focused-capacity+1)), true
	default:
		page.Height = max(page.Height, len(editor.directoryDraft)+10)
		focused := m.codexOptionDetail(&page, false)
		page.Height = m.height
		return page.finish(max(0, focused-capacity+1)), true
	}
}

func (editor codexOptionEditor) controls() ([]uiAction, string) {
	switch editor.mode {
	case "number", "path", "project":
		return []uiAction{{ID: "option:enter", Label: "Accept field", Key: "enter"}, {ID: "option:esc", Label: "Cancel edit", Key: "esc"}}, ""
	case "choose":
		return []uiAction{{ID: "option:enter", Label: "Select", Key: "enter"}, {ID: "option:esc", Label: "Cancel edit", Key: "esc"}}, "↑↓/jk choose"
	case "dirs":
		actions := []uiAction{
			{ID: "option:enter", Label: "Done", Key: "enter", Primary: true},
			{ID: "option:n", Label: "Add directory", Key: "n"},
			{ID: "option:esc", Label: "Cancel edit", Key: "esc"},
		}
		if len(editor.directoryDraft) > 0 {
			actions = append(actions, uiAction{ID: "option:d", Label: "Remove selected", Key: "d"})
			return actions, "↑↓/jk choose"
		}
		return actions, ""
	case "confirm":
		return []uiAction{{ID: "option:y", Label: "Save shared defaults", Key: "y"}, {ID: "option:esc", Label: "Cancel", Key: "esc"}}, ""
	default:
		apply := "Apply"
		if editor.defaults != nil {
			apply = "Save"
		}
		actions := []uiAction{
			{ID: "option:ctrl+s", Label: apply, Key: "ctrl+s", Primary: true},
			{ID: "option:enter", Label: "Edit", Key: "enter"},
			{ID: "option:esc", Label: "Cancel", Key: "esc"},
			{ID: "option:ctrl+r", Label: "Reset to inherited", Key: "ctrl+r"},
		}
		if editor.defaults != nil {
			actions = append(actions, uiAction{ID: "option:p", Label: "Project", Key: "p"}, uiAction{ID: "option:r", Label: "Reload saved", Key: "r"})
		}
		hint := "↑↓/jk fields"
		if len(editor.quickChoices()) > 1 {
			hint += " · ←→/hl change"
		}
		return actions, hint
	}
}

func (m harborModel) codexOptionOverview(page *uiPage, split bool) int {
	editor := m.codexEditor
	if editor.defaults != nil {
		label := editor.defaults.Profile
		if editor.defaults.Kind == profileExisting {
			label += " · shared"
		}
		page.line("Profile: " + cleanUI(label))
		page.line("Project: " + cleanUI(editor.defaults.Project))
	}
	fields := editor.fields()
	for _, field := range fields {
		page.FieldColumn = max(page.FieldColumn, 8+ansi.StringWidth(codexOptionLabels[field]))
	}
	focused := 0
	for _, field := range fields {
		switch field {
		case optionSandbox:
			page.groupBand("Execution", "")
		case optionWindow:
			page.groupBand("Context", "")
		case optionPostTurn:
			page.groupBand("Advanced", "")
		}
		active := field == editor.field
		if active {
			focused = len(page.Lines)
		}
		value := editor.value.displayField(field)
		if isContextOption(field) {
			numeric := editor
			numeric.field = field
			value = contextTokens(*numeric.number())
		}
		id := fmt.Sprintf("option-field:%d", field)
		if editor.mode == "" {
			page.field(id, "", codexOptionLabels[field], value, active, active && !split)
		} else {
			page.listRow("", "", codexOptionLabels[field], value, uiRowStyle{selected: active})
		}
	}
	return focused
}

func addCodexOptionNote(page *uiPage, text string) {
	if text == "" {
		return
	}
	for _, line := range strings.Split(ansi.Wrap(cleanUI(text), max(1, page.Width-3), ""), "\n") {
		page.line(paint(fg("muted"), line))
	}
}

func (editor codexOptionEditor) defaultNotes(page *uiPage) {
	if editor.defaults == nil {
		return
	}
	value := editor.defaults.Resolved.displayField(editor.field)
	key := codexNumberOverrides(editor.value)[editor.field-optionWindow].KeyPath
	origin := editor.defaults.Origins[key].Name
	source := origin.Type
	if origin.Project != "" {
		source = origin.Project
	} else if origin.File != "" {
		source = origin.File
	}
	if source != "" {
		value += " · " + source
	}
	addCodexOptionNote(page, "Effective now: "+value)
	addCodexOptionNote(page, "Saved defaults affect newly loaded conversations, not running ones.")
}

func (editor codexOptionEditor) compactNote() string {
	switch {
	case editor.value.compactExceedsWindow(editor.currentChoice()):
		return "Auto compact is above the requested window."
	case editor.value.recommendedCompact() != "":
		return fmt.Sprintf("%s is recommended for a %s window.", contextChoiceLabel(editor.value.recommendedCompact()), contextTokens(editor.value.ContextWindow))
	}
	return ""
}

func (editor codexOptionEditor) reasoningNotice() string {
	if editor.reasoning == nil {
		return "Loading model reasoning options…"
	}
	if editor.reasoning.err != nil {
		return editor.reasoning.err.Error() + "; Inherited is available."
	}
	if len(editor.reasoning.choices.Efforts) == 0 {
		return "No reasoning efforts advertised; Inherited is available."
	}
	return ""
}

func (m harborModel) codexOptionDetail(page *uiPage, preview bool) int {
	editor := m.codexEditor
	label := codexOptionLabels[editor.field]
	choice := editor.currentChoice()
	focused := len(page.Lines)
	if editor.mode == "confirm" {
		addCodexOptionNote(page, "This existing environment is shared. Saving changes Codex defaults for every client using:")
		addCodexOptionNote(page, editor.defaults.Path)
	} else {
		if editor.mode == "project" {
			label = "Project for effective defaults"
		}
		page.line(paint(fg("section").Bold(true), label))
		switch editor.mode {
		case "number", "path", "project":
			placeholder := "Percent (0–100) · empty = inherited"
			switch {
			case isContextOption(editor.field) && editor.mode == "number":
				placeholder = "Tokens, e.g. 272K · empty = inherited"
			case editor.mode == "path":
				placeholder = "/absolute/directory"
			case editor.mode == "project":
				placeholder = "/absolute/project"
			}
			focused = len(page.Lines) + 2
			for _, line := range strings.Split(textEntry(m.input, page.Width, placeholder), "\n") {
				page.line(documentLine(line))
			}
		default:
			if editor.field == optionDirectories {
				directories := editor.value.WritableDirs
				if !preview {
					directories = editor.directoryDraft
				}
				if len(directories) == 0 {
					page.emptyState("No additional directories")
				}
				for index, directory := range directories {
					id := ""
					if !preview {
						id = fmt.Sprintf("option-dir:%d", index)
					}
					active := !preview && index == editor.dirCursor
					if active {
						focused = len(page.Lines)
					}
					row := m.rowStyle(id, active, false)
					page.hit(id, 1, len(page.Lines), page.Width-2, 1)
					page.line(row.line(row.marker()+cleanUI(directory), "text", page.Width-2))
				}
			} else {
				if editor.field == optionReasoning && editor.reasoning != nil && editor.reasoning.choices.Model != "" {
					addCodexOptionNote(page, "Model: "+editor.reasoning.choices.Model)
				}
				choices := editor.fieldChoices()
				selected := editor.selectedChoice(choices)
				for index, candidate := range choices {
					id := fmt.Sprintf("option-choice:%d", index)
					active := !preview && index == editor.cursor
					if active {
						focused = len(page.Lines)
						if candidate != customCodexOption {
							choice = candidate
						}
					}
					label, note := editor.choiceDisplay(candidate, selected)
					applied := candidate == selected
					page.listRow(id, selectorMark(applied), label, note, m.rowStyle(id, active, applied))
				}
			}
		}
		if editor.mode != "project" {
			page.line("")
			addCodexOptionNote(page, codexOptionDescription(editor.field, choice))
			if editor.field == optionReasoning {
				addCodexOptionNote(page, editor.reasoningNotice())
			}
			if preview {
				editor.defaultNotes(page)
			}
		}
	}
	if m.notice != "" {
		addCodexOptionNote(page, m.notice)
		if editor.mode != "choose" && editor.mode != "dirs" {
			focused = len(page.Lines) - 1
		}
	}
	return focused
}

func (editor codexOptionEditor) choiceDisplay(choice, selected string) (string, string) {
	label := codexOptionChoiceLabel(choice)
	if isContextOption(editor.field) {
		label = contextChoiceLabel(choice)
	}
	if editor.field == optionPostTurn && choice == "0" {
		label = "Off (0%)"
	}
	if editor.field == optionCompact {
		switch {
		case editor.value.compactExceedsWindow(choice):
			return label, "Above requested window"
		case choice != "" && choice == editor.value.recommendedCompact():
			return label, "Recommended for " + contextTokens(editor.value.ContextWindow)
		}
	}
	if choice == customCodexOption && choice == selected {
		return label, editor.value.displayField(editor.field)
	}
	return label, ""
}

func (page *uiPage) codexOptionColumns(left, right uiPage, height, focused int) {
	top := len(page.Lines)
	leftWidth := left.Width - 2
	divider := paint(fg("border"), " │ ")
	start := max(0, focused-height+1)
	total := len(right.Lines)
	right.Lines = right.Lines[start:min(start+height, total)]
	right.scrollRail(0, len(right.Lines), total, start)
	rows := max(len(left.Lines), len(right.Lines))
	for index := 0; index < rows; index++ {
		leftLine, rightLine := "", ""
		if index < len(left.Lines) {
			leftLine = left.Lines[index]
		}
		if index < len(right.Lines) {
			rightLine = right.Lines[index]
		}
		page.line(leftLine + strings.Repeat(" ", max(0, leftWidth-ansi.StringWidth(leftLine))) + divider + rightLine)
	}
	for _, hit := range left.Hits {
		page.hit(hit.ID, hit.X, top+hit.Y, hit.W, hit.H)
	}
	for _, hit := range right.Hits {
		if hit.Y >= start && hit.Y < start+height {
			page.hit(hit.ID, hit.X+leftWidth+3, top+hit.Y-start, hit.W, hit.H)
		}
	}
}
