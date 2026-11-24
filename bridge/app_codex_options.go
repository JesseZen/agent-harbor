package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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

const customCodexOption = "Custom…"

func isContextOption(field int) bool {
	return field == optionWindow || field == optionCompact
}

func contextPresets(field int) []string {
	if field == optionWindow {
		return []string{"", "300000", "500000"}
	}
	return []string{"", "272000", "450000"}
}

func contextTokens(value *int64) string {
	if value != nil && *value%1000 == 0 {
		return fmt.Sprintf("%dK", *value/1000)
	}
	return codexOptionNumber(value, "")
}

func contextChoiceLabel(choice string) string {
	if tokenCount, err := strconv.ParseInt(choice, 10, 64); err == nil {
		return contextTokens(&tokenCount)
	}
	return codexOptionChoiceLabel(choice)
}

func (o codexOptions) recommendedCompact() string {
	if o.ContextWindow != nil {
		index := slices.Index(contextPresets(optionWindow), strconv.FormatInt(*o.ContextWindow, 10))
		if index > 0 {
			return contextPresets(optionCompact)[index]
		}
	}
	return ""
}

func (o codexOptions) compactExceedsWindow(choice string) bool {
	limit, err := strconv.ParseInt(choice, 10, 64)
	return err == nil && o.ContextWindow != nil && limit > *o.ContextWindow
}

type codexOptionEditor struct {
	value            codexOptions
	field            int
	mode             string
	cursor           int
	dirCursor        int
	directoryDraft   []string
	reasoningRequest *codexReasoningRequest
	reasoning        *codexReasoningLoaded
	returnScroll     int
	returnScreen     string
	session          *sessionView
	binding          profileBinding
	defaults         *profileCodexDefaults
	project          string
}

type codexReasoningRequest struct {
	draft   sessionDraft
	session *sessionView
}

type codexReasoningLoaded struct {
	request *codexReasoningRequest
	choices codexReasoningChoices
	err     error
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
	case optionWindow, optionCompact, optionPostTurn:
		if value := *editor.number(); value != nil {
			return strconv.FormatInt(*value, 10)
		}
		return ""
	case optionReasoning:
		return editor.value.ReasoningEffort
	}
	return ""
}

func (editor codexOptionEditor) quickChoices() []string {
	switch editor.field {
	case optionSandbox:
		return codexSandboxModes
	case optionApproval:
		return codexApprovalPolicies
	case optionReasoning:
		if editor.reasoning != nil && len(editor.reasoning.choices.Efforts) > 0 {
			return append([]string{""}, editor.reasoning.choices.Efforts...)
		}
	case optionWindow, optionCompact:
		return contextPresets(editor.field)
	}
	return nil
}

func (editor codexOptionEditor) fieldChoices() []string {
	switch editor.field {
	case optionWindow, optionCompact:
		return append(editor.quickChoices(), customCodexOption)
	case optionPostTurn:
		return []string{"", "0", customCodexOption}
	case optionReasoning:
		if choices := editor.quickChoices(); len(choices) > 0 {
			return choices
		}
		return []string{""}
	default:
		return editor.quickChoices()
	}
}

func (editor codexOptionEditor) selectedChoice(choices []string) string {
	choice := editor.currentChoice()
	if !slices.Contains(choices, choice) && slices.Contains(choices, customCodexOption) {
		return customCodexOption
	}
	return choice
}

func (editor *codexOptionEditor) selectChoice(value string) {
	switch editor.field {
	case optionSandbox:
		editor.value.SandboxMode = value
	case optionApproval:
		editor.value.ApprovalPolicy = value
	case optionReasoning:
		editor.value.ReasoningEffort = value
	case optionWindow, optionCompact, optionPostTurn:
		if value == "" {
			*editor.number() = nil
		} else if tokenCount, err := strconv.ParseInt(value, 10, 64); err == nil {
			*editor.number() = &tokenCount
		}
	}
}

func (editor *codexOptionEditor) cycleChoice(direction int) {
	choices := editor.quickChoices()
	if len(choices) == 0 {
		return
	}
	current := editor.currentChoice()
	index := slices.Index(choices, current)
	if index < 0 && isContextOption(editor.field) {
		value, _ := strconv.ParseInt(current, 10, 64)
		lower, upper := 0, 0
		for presetIndex, preset := range choices[1:] {
			tokens, _ := strconv.ParseInt(preset, 10, 64)
			if tokens < value {
				lower = presetIndex + 1
			} else {
				upper = presetIndex + 1
				break
			}
		}
		if direction > 0 {
			index = upper
		} else {
			index = lower
		}
		editor.selectChoice(choices[index])
		return
	}
	if index < 0 {
		index = 0
		if direction > 0 {
			index = len(choices) - 1
		}
	}
	editor.selectChoice(choices[(index+direction+len(choices))%len(choices)])
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

func codexOptionDescription(field int, choice string) string {
	switch field {
	case optionSandbox:
		switch choice {
		case "read-only":
			return "Codex tools can read files. The sandbox blocks writes."
		case "workspace-write":
			return "Allows writes in the workspace and extra dirs. Codex settings control network access."
		case "danger-full-access":
			return "Removes Codex file and network restrictions. Host restrictions still apply."
		default:
			return "File and network access for Codex tools. Inherited uses Codex settings."
		}
	case optionApproval:
		switch choice {
		case "on-request":
			return "Codex can ask for permission to run a command outside the sandbox."
		case "never":
			return "Codex skips approval prompts. Sandbox restrictions still apply."
		default:
			return "When Codex can ask for permission to run commands. Inherited uses Codex settings."
		}
	case optionReasoning:
		return "Model reasoning effort. Higher levels can take longer and use more tokens."
	case optionWindow:
		return "Requested tokens, capped by the model. See usable capacity in Codex /status."
	case optionCompact:
		return "Token threshold for automatic history compaction. The model window may cap it."
	case optionPostTurn:
		if percent, err := strconv.Atoi(choice); err == nil {
			if percent == 0 {
				return "Turns off the end-of-reply check. Auto compact still applies."
			}
			return fmt.Sprintf("After a reply, compact if at least %d%% of the usable window is occupied or the usual token limit is reached.", percent)
		}
		return "After a reply, check context usage against a percentage of the usable window."
	default:
		return "Extra writable paths for Workspace write. Use absolute paths on the session's host."
	}
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
	editor := &m.codexEditor
	if loaded, ok := msg.(codexReasoningLoaded); ok {
		if loaded.request == editor.reasoningRequest {
			editor.reasoning = &loaded
			if editor.field == optionReasoning && editor.mode == "choose" {
				editor.openChoices()
			}
		}
		return m, nil, true
	}
	if m.screen != codexOptionsScreen {
		return m, nil, false
	}
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
					cmd := m.requestCodexReasoning()
					return m, cmd, true
				}
				key = "enter"
			case "option-choice":
				if editor.mode == "" {
					editor.openChoices()
					editor.cursor, m.notice = index, ""
					cmd := m.requestCodexReasoning()
					return m, cmd, true
				}
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
				editor.directoryDraft = nil
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
	if key == "up" || key == "k" {
		delta = -1
	} else if key == "down" || key == "j" {
		delta = 1
	}
	switch editor.mode {
	case "confirm":
		if key == "y" {
			return m.applyCodexEditor()
		}
	case "choose":
		choices := editor.fieldChoices()
		if delta != 0 {
			editor.cursor = min(max(0, editor.cursor+delta), len(choices)-1)
		}
		if key == "enter" {
			choice := choices[editor.cursor]
			m.notice = ""
			if choice == customCodexOption {
				cmd := m.codexOptionText("number", editor.currentChoice())
				return m, cmd, true
			}
			editor.selectChoice(choice)
			editor.mode = ""
		}
	case "dirs":
		if delta != 0 {
			editor.dirCursor = min(max(0, editor.dirCursor+delta), max(0, len(editor.directoryDraft)-1))
		}
		switch key {
		case "enter":
			editor.value.WritableDirs = editor.directoryDraft
			editor.directoryDraft, editor.mode = nil, ""
		case "n":
			cmd := m.codexOptionText("path", "")
			return m, cmd, true
		case "d":
			if len(editor.directoryDraft) > 0 {
				editor.directoryDraft = slices.Delete(slices.Clone(editor.directoryDraft), editor.dirCursor, editor.dirCursor+1)
				editor.dirCursor = min(editor.dirCursor, max(0, len(editor.directoryDraft)-1))
			}
		}
	case "path":
		if key == "enter" {
			directories := append(slices.Clone(editor.directoryDraft), m.input.Value())
			if err := (codexOptions{WritableDirs: directories}).validate("codex", false); err != nil {
				m.notice = err.Error()
			} else {
				editor.directoryDraft, editor.dirCursor = directories, len(directories)-1
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
				if editor.field != optionPostTurn && strings.HasSuffix(strings.ToUpper(value), "K") {
					value = strings.TrimSpace(value[:len(value)-1]) + "000"
				}
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
			cmd := m.requestCodexReasoning()
			return m, cmd, true
		}
		switch key {
		case "left", "h", "right", "l":
			if len(editor.quickChoices()) > 1 {
				direction := 1
				if key == "left" || key == "h" {
					direction = -1
				}
				editor.cycleChoice(direction)
				m.notice = ""
			}
			cmd := m.requestCodexReasoning()
			return m, cmd, true
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
			if editor.field == optionDirectories {
				editor.mode, editor.dirCursor = "dirs", 0
				editor.directoryDraft = slices.Clone(editor.value.WritableDirs)
			} else {
				editor.openChoices()
				cmd := m.requestCodexReasoning()
				return m, cmd, true
			}
		}
	}
	return m, nil, true
}

func (editor *codexOptionEditor) openChoices() {
	choices := editor.fieldChoices()
	editor.mode = "choose"
	editor.cursor = max(0, slices.Index(choices, editor.selectedChoice(choices)))
}

func (m *harborModel) requestCodexReasoning() tea.Cmd {
	editor := &m.codexEditor
	if editor.field != optionReasoning || editor.reasoningRequest != nil || editor.reasoning != nil {
		return nil
	}
	service, ok := m.service.(codexOptionService)
	if !ok {
		editor.reasoning = &codexReasoningLoaded{err: errors.New("Model catalog unavailable")}
		return nil
	}
	request := &codexReasoningRequest{draft: m.draft, session: editor.session}
	editor.reasoningRequest = request
	return func() tea.Msg {
		result := codexReasoningLoaded{request: request}
		if request.session != nil {
			result.choices, result.err = service.SessionCodexReasoningChoices(request.session.session)
		} else {
			result.choices, result.err = service.CodexReasoningChoices(request.draft)
		}
		return result
	}
}

func (m harborModel) applyCodexEditor() (tea.Model, tea.Cmd, bool) {
	editor := m.codexEditor
	if err := editor.value.validate("codex", false); err != nil {
		m.codexEditorError(err)
		cmd := m.requestCodexReasoning()
		return m, cmd, true
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
			return next.requestCodexReasoning()
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
		field := slices.Index(codexOptionLabels, option.field)
		if slices.Contains(m.codexEditor.fields(), field) {
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
