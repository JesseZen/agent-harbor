package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const codexOptionsScreen = "codex-options"
const codexOptionsSaved = "Codex options saved for the next launch."

const (
	optionSandbox = iota
	optionApproval
	optionReasoning
	optionWindow
	optionCompact
	optionPostTurn
	optionDirectories
)

var codexOptionLabels = []string{"Sandbox", "Approval", "Reasoning", "Context window", "Auto compact", "Post-turn compact", "Writable dirs"}

type codexOptionEditor struct {
	value        codexOptions
	field        int
	mode         string
	cursor       int
	dirCursor    int
	choices      []string
	model        string
	returnScroll int
	returnScreen string
	session      *sessionView
	binding      profileBinding
	defaults     *profileCodexDefaults
	project      string
}

type codexOptionService interface {
	RememberCodexOptions(profileBinding, codexOptions) error
	CodexReasoningChoices(sessionDraft) (codexReasoningChoices, error)
	SessionCodexReasoningChoices(session) (codexReasoningChoices, error)
	SaveSessionCodexOptions(sessionView, codexOptions) error
	ProfileCodexDefaults(profileBinding, string) (profileCodexDefaults, error)
	SaveProfileCodexDefaults(profileCodexDefaults, codexOptions) (profileCodexDefaults, error)
}

func codexOptionChoiceLabel(value string) string {
	switch value {
	case "":
		return "Inherited"
	case "read-only":
		return "Read only"
	case "workspace-write":
		return "Workspace write"
	case "danger-full-access":
		return "Full access"
	case "on-request":
		return "On request"
	case "never":
		return "Never"
	}
	return value
}

func codexOptionNumber(value *int64, suffix string) string {
	if value == nil {
		return "Inherited"
	}
	return strconv.FormatInt(*value, 10) + suffix
}

func (o codexOptions) displayField(field int) string {
	switch field {
	case optionSandbox:
		return codexOptionChoiceLabel(o.SandboxMode)
	case optionApproval:
		return codexOptionChoiceLabel(o.ApprovalPolicy)
	case optionReasoning:
		return codexOptionChoiceLabel(o.ReasoningEffort)
	case optionWindow:
		return codexOptionNumber(o.ContextWindow, " tokens")
	case optionCompact:
		return codexOptionNumber(o.AutoCompactTokenLimit, " tokens")
	case optionPostTurn:
		if o.PostTurnCompactPercent != nil && *o.PostTurnCompactPercent == 0 {
			return "Off (0%)"
		}
		return codexOptionNumber(o.PostTurnCompactPercent, "%")
	case optionDirectories:
		if len(o.WritableDirs) == 1 {
			return cleanUI(o.WritableDirs[0])
		}
		if len(o.WritableDirs) > 1 {
			return fmt.Sprintf("%d extra directories", len(o.WritableDirs))
		}
	}
	return "Inherited"
}

func (o codexOptions) summary() string {
	var values []string
	for _, value := range []string{o.SandboxMode, o.ApprovalPolicy} {
		if value != "" {
			values = append(values, codexOptionChoiceLabel(value))
		}
	}
	if o.ContextWindow != nil {
		if *o.ContextWindow%1000 == 0 {
			values = append(values, fmt.Sprintf("%dk", *o.ContextWindow/1000))
		} else {
			values = append(values, o.displayField(optionWindow))
		}
	}
	if o.ReasoningEffort != "" {
		values = append(values, o.ReasoningEffort)
	}
	if count := boolCount(o.AutoCompactTokenLimit != nil, o.PostTurnCompactPercent != nil, len(o.WritableDirs) != 0); count > 0 {
		values = append(values, fmt.Sprintf("%d advanced", count))
	}
	if len(values) == 0 {
		return "Inherited"
	}
	return strings.Join(values, " · ")
}

func boolCount(values ...bool) int {
	count := 0
	for _, value := range values {
		if value {
			count++
		}
	}
	return count
}

func (editor codexOptionEditor) title() string {
	switch editor.returnScreen {
	case "profile-list":
		return "Profiles › Codex defaults"
	case "detail":
		return "Session details › Codex options"
	default:
		return "New session › Options"
	}
}

func (editor codexOptionEditor) fields() []int {
	if editor.returnScreen == "profile-list" {
		return []int{optionWindow, optionCompact, optionPostTurn}
	}
	return []int{optionSandbox, optionApproval, optionReasoning, optionWindow, optionCompact, optionPostTurn, optionDirectories}
}

func (editor codexOptionEditor) currentChoice() string {
	switch editor.field {
	case optionSandbox:
		return editor.value.SandboxMode
	case optionApproval:
		return editor.value.ApprovalPolicy
	default:
		return editor.value.ReasoningEffort
	}
}

func (editor *codexOptionEditor) selectChoice(value string) {
	switch editor.field {
	case optionSandbox:
		editor.value.SandboxMode = value
	case optionApproval:
		editor.value.ApprovalPolicy = value
	case optionReasoning:
		editor.value.ReasoningEffort = value
	}
}

func (editor *codexOptionEditor) number() **int64 {
	switch editor.field {
	case optionWindow:
		return &editor.value.ContextWindow
	case optionCompact:
		return &editor.value.AutoCompactTokenLimit
	default:
		return &editor.value.PostTurnCompactPercent
	}
}

func codexOptionDescription(field int) string {
	switch field {
	case optionSandbox:
		return "Full access disables the filesystem sandbox."
	case optionApproval:
		return "Never disables approval prompts, not the sandbox."
	case optionReasoning:
		return "Choices come from the current model's catalog."
	case optionWindow:
		return "Configured tokens; the model can cap the usable window. See /status in Codex."
	case optionCompact:
		return "Trigger threshold in tokens; Codex can cap it based on the window."
	case optionPostTurn:
		return "0 disables only the end-of-turn check; the token threshold still applies."
	default:
		return "Additional writable directories on this server, for Workspace write."
	}
}

func (m harborModel) codexOptionsPage() (uiPage, bool) {
	if m.screen != codexOptionsScreen {
		return uiPage{}, false
	}
	editor := m.codexEditor
	p := newUIPage(m.width, m.height)
	p.title(editor.title())
	var actions []uiAction
	focused := 0
	addNote := func(text string) {
		for _, line := range strings.Split(ansi.Wrap(cleanUI(text), max(1, p.Width-3), ""), "\n") {
			p.line(paint(fg("muted"), line))
		}
	}
	switch editor.mode {
	case "number", "path", "project":
		label, placeholder := codexOptionLabels[editor.field], "Empty = inherited"
		if editor.mode == "path" {
			placeholder = "/absolute/directory"
		} else if editor.mode == "project" {
			label, placeholder = "Project for effective defaults", "/absolute/project"
		}
		p.line(label)
		for _, line := range strings.Split(textEntry(m.input, m.width, placeholder), "\n") {
			p.line(documentLine(line))
		}
		focused = len(p.Lines) - 1
		actions = []uiAction{{ID: "option:enter", Label: "Accept field", Key: "enter"}, {ID: "option:esc", Label: "Cancel edit", Key: "esc"}}
	case "choose":
		if editor.model != "" {
			p.line("Model: " + cleanUI(editor.model))
		}
		for index, choice := range editor.choices {
			id := fmt.Sprintf("option-choice:%d", index)
			selected := choice == editor.currentChoice()
			if index == editor.cursor {
				focused = len(p.Lines)
			}
			p.listRow(id, selectorMark(selected), codexOptionChoiceLabel(choice), "", m.rowStyle(id, index == editor.cursor, selected))
		}
		addNote(codexOptionDescription(editor.field))
		actions = []uiAction{{ID: "option:enter", Label: "Select", Key: "enter"}, {ID: "option:esc", Label: "Cancel edit", Key: "esc"}}
		p.Hint = "↑↓/jk choose"
	case "dirs":
		p.line("Writable dirs")
		if len(editor.value.WritableDirs) == 0 {
			p.emptyState("No additional directories")
		}
		for index, directory := range editor.value.WritableDirs {
			id := fmt.Sprintf("option-dir:%d", index)
			if index == editor.dirCursor {
				focused = len(p.Lines)
			}
			p.listRow(id, "", cleanUI(directory), "", m.rowStyle(id, index == editor.dirCursor, false))
		}
		actions = []uiAction{{ID: "option:n", Label: "Add directory", Key: "n"}, {ID: "option:esc", Label: "Back", Key: "esc"}}
		if len(editor.value.WritableDirs) > 0 {
			actions = append(actions, uiAction{ID: "option:d", Label: "Remove selected", Key: "d"})
			p.Hint = "↑↓/jk choose"
		}
	case "confirm":
		addNote("This existing environment is shared. Saving changes Codex defaults for every client using:")
		addNote(editor.defaults.Path)
		actions = []uiAction{{ID: "option:y", Label: "Save shared defaults", Key: "y"}, {ID: "option:esc", Label: "Cancel", Key: "esc"}}
	default:
		if editor.defaults != nil {
			label := editor.defaults.Profile
			if editor.defaults.Kind == profileExisting {
				label += " · shared"
			}
			p.line("Profile: " + cleanUI(label))
			p.line("Project: " + cleanUI(editor.defaults.Project))
		}
		for _, field := range editor.fields() {
			p.FieldColumn = max(p.FieldColumn, 8+ansi.StringWidth(codexOptionLabels[field]))
		}
		for _, field := range editor.fields() {
			if field == editor.field {
				focused = len(p.Lines)
			}
			p.field(fmt.Sprintf("option-field:%d", field), "", codexOptionLabels[field], editor.value.displayField(field), field == editor.field, true)
		}
		if editor.defaults != nil {
			value := editor.defaults.Resolved.displayField(editor.field)
			edits := codexNumberOverrides(editor.value)
			key := edits[editor.field-optionWindow].KeyPath
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
			addNote("Effective now: " + value)
			addNote("Saved defaults affect newly loaded conversations, not running ones.")
		} else if editor.value.SandboxMode == "danger-full-access" {
			addNote(codexOptionDescription(optionSandbox))
		} else {
			addNote(codexOptionDescription(editor.field))
		}
		apply := "Apply"
		if editor.defaults != nil {
			apply = "Save"
		}
		actions = []uiAction{{ID: "option:ctrl+s", Label: apply, Key: "ctrl+s", Primary: true}, {ID: "option:ctrl+r", Label: "Reset to inherited", Key: "ctrl+r"}, {ID: "option:esc", Label: "Cancel", Key: "esc"}}
		if editor.defaults != nil {
			actions = append(actions, uiAction{ID: "option:p", Label: "Project", Key: "p"}, uiAction{ID: "option:r", Label: "Reload saved", Key: "r"})
		}
		p.Hint = "↑↓/jk fields · Enter edit"
	}
	if m.notice != "" {
		addNote(m.notice)
		focused = max(focused, len(p.Lines)-1)
	}
	p.shortcuts(actions, "")
	capacity := max(1, p.Height-len(p.shortcutBar().Lines))
	return p.finish(max(0, focused-capacity+1)), true
}

func (m *harborModel) openCodexOptions(editor codexOptionEditor) {
	editor.returnScroll = m.scroll
	m.codexEditor = editor
	m.screen, m.scroll, m.uiFocus, m.notice = codexOptionsScreen, 0, "", ""
}

func (m harborModel) codexEditorText() bool {
	return m.screen == codexOptionsScreen && slices.Contains([]string{"number", "path", "project"}, m.codexEditor.mode)
}

func (m *harborModel) codexOptionText(mode, text string) tea.Cmd {
	m.codexEditor.mode = mode
	m.input.SetValue(text)
	m.input.CursorEnd()
	return m.input.Focus()
}

func (m harborModel) codexOptionsInput(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if m.screen != codexOptionsScreen {
		return m, nil, false
	}
	editor := &m.codexEditor
	var key string
	switch event := msg.(type) {
	case tea.KeyMsg:
		key = event.String()
	case tea.MouseMsg:
		if event.Button == tea.MouseButtonWheelUp || event.Button == tea.MouseButtonWheelDown {
			key = "up"
			if event.Button == tea.MouseButtonWheelDown {
				key = "down"
			}
		} else if event.Action == tea.MouseActionPress && event.Button == tea.MouseButtonLeft {
			page, _ := m.codexOptionsPage()
			target := page.target(event.X, event.Y)
			kind, value, _ := strings.Cut(target, ":")
			index, _ := strconv.Atoi(value)
			switch kind {
			case "option-field":
				if editor.field != index {
					editor.field, m.notice = index, ""
					return m, nil, true
				}
				key = "enter"
			case "option-choice":
				if editor.cursor != index {
					editor.cursor = index
					return m, nil, true
				}
				key = "enter"
			case "option-dir":
				editor.dirCursor = index
			case "option":
				key = value
			}
		}
	default:
		return m, nil, false
	}
	if key == "esc" {
		m.notice = ""
		if editor.mode == "" {
			m.screen, m.scroll = editor.returnScreen, editor.returnScroll
			m.codexEditor = codexOptionEditor{}
		} else {
			if editor.mode == "path" {
				editor.mode = "dirs"
			} else {
				editor.mode = ""
			}
			m.input.Blur()
		}
		return m, nil, true
	}
	if m.codexEditorText() && key != "enter" {
		if event, ok := msg.(tea.KeyMsg); ok {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(event)
			return m, cmd, true
		}
		return m, nil, true
	}
	if key == "" {
		return m, nil, true
	}
	var delta int
	if key == "up" {
		delta = -1
	} else if key == "down" {
		delta = 1
	}
	switch editor.mode {
	case "confirm":
		if key == "y" {
			return m.applyCodexEditor()
		}
	case "choose":
		if delta != 0 {
			editor.cursor = min(max(0, editor.cursor+delta), len(editor.choices)-1)
		}
		if key == "enter" {
			editor.selectChoice(editor.choices[editor.cursor])
			editor.mode, m.notice = "", ""
		}
	case "dirs":
		if delta != 0 {
			editor.dirCursor = min(max(0, editor.dirCursor+delta), max(0, len(editor.value.WritableDirs)-1))
		}
		if key == "n" {
			cmd := m.codexOptionText("path", "")
			return m, cmd, true
		}
		if key == "d" && len(editor.value.WritableDirs) > 0 {
			editor.value.WritableDirs = slices.Delete(slices.Clone(editor.value.WritableDirs), editor.dirCursor, editor.dirCursor+1)
			editor.dirCursor = min(editor.dirCursor, max(0, len(editor.value.WritableDirs)-1))
		}
	case "path":
		if key == "enter" {
			directories := append(slices.Clone(editor.value.WritableDirs), m.input.Value())
			if err := (codexOptions{WritableDirs: directories}).validate("codex", false); err != nil {
				m.notice = err.Error()
			} else {
				editor.value.WritableDirs, editor.dirCursor = directories, len(directories)-1
				editor.mode, m.notice = "dirs", ""
				m.input.Blur()
			}
		}
	case "project":
		if key == "enter" {
			return m.loadCodexDefaults(editor.binding, m.input.Value(), true)
		}
	case "number":
		if key == "enter" {
			previous := *editor.number()
			value := strings.TrimSpace(m.input.Value())
			if value == "" {
				*editor.number() = nil
			} else {
				number, err := strconv.ParseInt(value, 10, 64)
				if err != nil {
					m.notice = "Enter a whole number, or leave empty to inherit."
					return m, nil, true
				}
				*editor.number() = &number
			}
			if err := codexNumberOptionsFrom(editor.value).validate("codex", false); err != nil {
				*editor.number() = previous
				m.notice = err.Error()
			} else {
				editor.mode, m.notice = "", ""
				m.input.Blur()
			}
		}
	default:
		if delta != 0 {
			fields := editor.fields()
			index := slices.Index(fields, editor.field)
			editor.field = fields[(index+len(fields)+delta)%len(fields)]
			m.notice = ""
		}
		switch key {
		case "ctrl+r":
			editor.value, m.notice = codexOptions{}, ""
		case "ctrl+s":
			return m.applyCodexEditor()
		case "r":
			if editor.defaults != nil {
				return m.loadCodexDefaults(editor.binding, editor.project, false)
			}
		case "p":
			if editor.defaults != nil {
				cmd := m.codexOptionText("project", editor.project)
				return m, cmd, true
			}
		case "enter":
			m.notice = ""
			switch editor.field {
			case optionSandbox, optionApproval:
				choices := codexSandboxModes
				if editor.field == optionApproval {
					choices = codexApprovalPolicies
				}
				editor.openChoices(choices, "")
			case optionReasoning:
				return m.loadCodexReasoning()
			case optionDirectories:
				editor.mode, editor.dirCursor = "dirs", 0
			default:
				text := ""
				if value := *editor.number(); value != nil {
					text = strconv.FormatInt(*value, 10)
				}
				cmd := m.codexOptionText("number", text)
				return m, cmd, true
			}
		}
	}
	return m, nil, true
}

func (editor *codexOptionEditor) openChoices(choices []string, model string) {
	editor.choices, editor.model, editor.mode = choices, model, "choose"
	editor.cursor = max(0, slices.Index(choices, editor.currentChoice()))
}

func (m harborModel) loadCodexReasoning() (tea.Model, tea.Cmd, bool) {
	service, ok := m.service.(codexOptionService)
	if !ok {
		m.codexEditor.openChoices([]string{""}, "")
		m.notice = "Model catalog unavailable; you can still choose Inherited."
		return m, nil, true
	}
	var choices codexReasoningChoices
	target := m.codexEditor.session
	return m.serviceTask(func() error {
		var err error
		if target != nil {
			choices, err = service.SessionCodexReasoningChoices(target.session)
		} else {
			choices, err = service.CodexReasoningChoices(m.draft)
		}
		return err
	}, func(next *harborModel, err error) tea.Cmd {
		next.codexEditor.openChoices(append([]string{""}, choices.Efforts...), choices.Model)
		if err != nil {
			next.notice = err.Error() + "; Inherited is available."
		} else if len(choices.Efforts) == 0 {
			next.notice = "No reasoning efforts advertised; choose Inherited."
		}
		return nil
	})
}

func (m harborModel) applyCodexEditor() (tea.Model, tea.Cmd, bool) {
	editor := m.codexEditor
	if err := editor.value.validate("codex", false); err != nil {
		m.codexEditorError(err)
		return m, nil, true
	}
	service, ok := m.service.(codexOptionService)
	if !ok {
		m.notice = "Codex options are unavailable in this environment."
		return m, nil, true
	}
	if editor.defaults != nil && editor.defaults.Kind == profileExisting && editor.mode != "confirm" {
		m.codexEditor.mode = "confirm"
		return m, nil, true
	}
	var saved profileCodexDefaults
	return m.serviceTask(func() error {
		switch {
		case editor.defaults != nil:
			var err error
			saved, err = service.SaveProfileCodexDefaults(*editor.defaults, editor.value)
			return err
		case editor.session != nil:
			return service.SaveSessionCodexOptions(*editor.session, editor.value)
		default:
			if m.draft.Profile == nil {
				return ErrProfileNotReady
			}
			return service.RememberCodexOptions(*m.draft.Profile, editor.value)
		}
	}, func(next *harborModel, err error) tea.Cmd {
		next.codexEditor.mode = ""
		if err != nil {
			next.codexEditorError(err)
			return nil
		}
		next.notice = ""
		if editor.defaults != nil {
			next.codexEditor.defaults, next.codexEditor.value = &saved, saved.Saved
			next.notice = "Codex defaults saved."
			return nil
		}
		next.screen, next.scroll = editor.returnScreen, editor.returnScroll
		next.codexEditor = codexOptionEditor{}
		if editor.session != nil {
			next.notice = codexOptionsSaved
			return next.load()
		}
		next.draft.CodexOptions = editor.value.saved()
		next.environment.CodexOptions = editor.value
		next.draft.RecoveryChoice = ""
		return nil
	})
}

func (m *harborModel) codexEditorError(err error) {
	m.notice = err.Error()
	var option *codexOptionError
	if errors.As(err, &option) {
		if field := slices.Index(codexOptionLabels, option.field); slices.Contains(m.codexEditor.fields(), field) {
			m.codexEditor.field = field
		}
	}
}

func (m harborModel) loadCodexDefaults(binding profileBinding, project string, keepEdits bool) (tea.Model, tea.Cmd, bool) {
	service, ok := m.service.(codexOptionService)
	if !ok {
		m.notice = "Codex defaults are unavailable in this environment."
		return m, nil, true
	}
	var result profileCodexDefaults
	return m.serviceTask(func() error {
		if project == "" && m.screen != codexOptionsScreen {
			environment, err := m.service.ProfileEnvironment(binding)
			if err != nil {
				return err
			}
			project = environment.Directory
		}
		var err error
		result, err = service.ProfileCodexDefaults(binding, project)
		return err
	}, func(next *harborModel, err error) tea.Cmd {
		if err != nil {
			next.notice = err.Error()
			return nil
		}
		if next.screen != codexOptionsScreen {
			next.openCodexOptions(codexOptionEditor{returnScreen: "profile-list", field: optionWindow, binding: binding})
		}
		if keepEdits && next.codexEditor.defaults != nil {
			if result.Version != next.codexEditor.defaults.Version {
				next.notice = errCodexConfigConflict.Error()
				next.codexEditor.mode = ""
				return nil
			}
		} else {
			next.codexEditor.value = result.Saved
		}
		next.codexEditor.defaults, next.codexEditor.project = &result, result.Project
		next.codexEditor.mode, next.notice = "", ""
		next.input.Blur()
		return nil
	})
}
