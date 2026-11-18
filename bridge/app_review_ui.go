package main

import (
	"errors"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"time"
)

type appReviewedResult struct {
	generation uint64
	target     string
	result     tea.Msg
}

func (m harborModel) reviewActions() []uiAction {
	out := []uiAction{}
	if m.action == "review-reference" && !m.referenceAccepted {
		out = append(out, uiAction{ID: "review:a", Key: "a", Label: "Use saved conversation"})
	}
	if (m.action == "query-resume" || m.action == "resume") && errors.Is(m.selectionReviewErr, errCodexProfileMissing) && m.reviewedSelection == nil {
		out = append(out, uiAction{ID: "review:b", Key: "b", Label: "Use recorded model"})
	}
	label := map[string]string{"start": "Start session", "stop": "Stop process", "remove": "Remove record", "resume": "Resume conversation", "query-resume": "Resume conversation", "review-reference": "Save reviewed reference", "close-previous": "Close inspected shell"}[m.action]
	disabled := ""
	if m.action == "review-reference" && !m.referenceAccepted {
		disabled = "Choose the saved conversation first"
	}
	if (m.action == "query-resume" || m.action == "resume") && m.reviewed.Mode == "magpie" && m.reviewed.CLI == "codex" && !joinedCodex(m.reviewed) && m.reviewedSelection == nil {
		disabled = "Resolve the session model before recovery"
	}
	if (m.action == "query-resume" || m.action == "resume") && m.reviewed.Conversation == nil {
		disabled = missingConversationRecovery
	}
	return append(out, uiAction{ID: "review:enter", Key: "enter", Label: label, Primary: true, Disabled: disabled}, uiAction{ID: "evidence", Key: "i", Label: "Details"}, uiAction{ID: "review:esc", Key: "esc", Label: "Cancel"})
}
func (m harborModel) reviewEvidence() []string {
	lines := []string{}
	for _, line := range strings.Split(m.confirmView()+"\n\n"+m.notice, "\n") {
		lines = append(lines, strings.Split(ansi.Wrap(cleanUI(line), max(1, m.width-2), ""), "\n")...)
	}
	return lines
}
func (m harborModel) reviewLayout() uiPage {
	p := newUIPage(m.width, m.height)
	p.line(paint(fg("brand"), "⚓ Harbor › "+reviewActionLabel(m.action)))
	if m.screen == "confirm-evidence" {
		return secondaryDocument(m.width, m.height, reviewActionLabel(m.action)+" · Details", strings.Join(m.reviewEvidence(), "\n"), []uiAction{{ID: "review-back", Label: "Back to decision", Key: "esc"}}, "review-back", m.scroll)
	}
	p.line(paint(fg("text"), cleanUI(m.reviewed.Name)))
	p.field("", "📂", "Project", cleanUI(m.reviewed.WorkingDirectory), false, false)
	p.field("", "🤖", "Client", cleanUI(m.reviewed.CLI), false, false)
	connection := m.reviewed.Mode
	if connection == "local" {
		connection = "Native · client settings and login"
	} else {
		connection += " · " + m.reviewed.InstanceID
		if m.action != "resume" && m.action != "query-resume" {
			connection += " / " + m.reviewed.MagpieModel
		}
	}
	p.field("", "🔌", "Connection", cleanUI(connection), false, false)
	if m.reviewed.Mode == "magpie" && (m.action == "resume" || m.action == "query-resume") {
		model := "Unavailable · inspect Details"
		if joinedCodex(m.reviewed) {
			model = "Codex saved selection"
		} else if m.reviewedSelection != nil {
			model = m.reviewedSelection.Model
			if m.reviewedSelection.Effort != "" {
				model += " · " + m.reviewedSelection.Effort
			}
		}
		p.field("", "🧠", "Model", cleanUI(model), false, false)
	}
	conversation := m.reviewed.CodexSessionID
	if m.reviewed.Conversation != nil {
		conversation = m.reviewed.Conversation.Reference.Value
	}
	if m.action != "start" {
		p.field("", "💬", "Conversation", orUnknown(conversation), false, false)
	}
	impact := map[string]string{"start": "", "stop": "The conversation stays resumable.", "remove": "Codex history is kept. Remove only the Harbor record.", "review-reference": "Save this review only. This does not resume the conversation.", "query-resume": "", "resume": "", "close-previous": "Close only the inspected idle shell; keep conversation and record."}[m.action]
	for _, line := range strings.Split(ansi.Wrap(impact, max(1, m.width-2), ""), "\n") {
		if line != "" {
			p.line(paint(fg("muted"), line))
		}
	}
	if m.action == "query-resume" {
		for _, line := range strings.Split(ansi.Wrap("Current login and other writers: unverified.", max(1, m.width-2), ""), "\n") {
			p.line(paint(fg("warn"), line))
		}
		if m.reviewed.Conversation != nil && m.reviewed.Conversation.Status != "observed" {
			p.field("", "", "Saved identity", m.reviewed.Conversation.Status, false, false)
		}
	}
	if (m.action == "query-resume" || m.action == "resume") && m.reviewed.Mode == "magpie" {
		for _, line := range strings.Split(ansi.Wrap("Routing group destinations can change.", max(1, m.width-2), ""), "\n") {
			p.line(paint(fg("muted"), line))
		}
	}
	if m.action == "review-reference" && m.reviewed.Conversation != nil {
		p.line(paint(fg("warn"), "Saved evidence: "+m.reviewed.Conversation.Status+" · choose explicitly"))
	}
	if m.selectionReviewErr != nil && m.reviewedSelection == nil {
		p.line(paint(fg("warn"), ansi.Truncate("Model unavailable: "+cleanUI(m.selectionReviewErr.Error()), max(1, m.width-2), "…")))
	}
	if m.notice != "" {
		p.line(paint(fg("warn"), ansi.Truncate(cleanUI(m.notice), max(1, m.width-2), "…")))
	}

	actions := m.reviewActions()
	p.shortcuts(actions, m.uiFocus)
	p.Hint = ""
	layout := p.finish(m.scroll)
	if layout.MaxScroll > 0 {
		p.Hint = scrollHint
		return p.finish(m.scroll)
	}
	return layout
}
func (m harborModel) activateReview(id string) (tea.Model, tea.Cmd) {
	m.uiFocus = ""
	if id == "evidence" {
		m.screen = "confirm-evidence"
		m.scroll = 0
		return m, nil
	}
	if id == "review-back" {
		m.screen = "confirm"
		m.uiFocus = "evidence"
		m.scroll = 0
		return m, nil
	}
	if strings.HasPrefix(id, "review:") {
		return m.Update(uiKey(strings.TrimPrefix(id, "review:")))
	}
	return m, nil
}
func (m harborModel) updateReviewUI(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if m.screen != "confirm" && m.screen != "confirm-evidence" {
		return m, nil, false
	}
	if mouse, ok := msg.(tea.MouseMsg); ok {
		if mouse.Button == tea.MouseButtonWheelDown || mouse.Button == tea.MouseButtonWheelUp {
			key := tea.KeyDown
			if mouse.Button == tea.MouseButtonWheelUp {
				key = tea.KeyUp
			}
			next, cmd := m.Update(tea.KeyMsg{Type: key})
			return next, cmd, true
		}
		if mouse.Action != tea.MouseActionPress || mouse.Button != tea.MouseButtonLeft {
			return m, nil, true
		}
		if time.Since(m.reviewOpenedAt) < 200*time.Millisecond {
			return m, nil, true
		}
		next, cmd := m.activateReview(m.reviewLayout().target(mouse.X, mouse.Y))
		return next, cmd, true
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil, false
	}
	k := key.String()
	if m.screen == "confirm" && (k == "tab" || k == "shift+tab") {
		return m, nil, true
	}
	if m.screen == "confirm" && (k == "up" || k == "down" || k == "j" || k == "k") {
		if k == "up" || k == "k" {
			m.scroll = max(0, m.scroll-1)
		} else {
			m.scroll = min(m.reviewLayout().MaxScroll, m.scroll+1)
		}
		return m, nil, true
	}
	if m.screen == "confirm" && (k == "pgup" || k == "pgdown") {
		delta := max(1, m.height/2)
		if k == "pgup" {
			delta = -delta
		}
		m.scroll = max(0, min(m.reviewLayout().MaxScroll, m.scroll+delta))
		return m, nil, true
	}
	if m.screen == "confirm-evidence" {
		switch k {
		case "esc", "enter", "i":
			m.screen = "confirm"
			m.uiFocus = "evidence"
			m.scroll = 0
		case "down", "j":
			m.scroll = min(m.reviewLayout().MaxScroll, m.scroll+1)
		case "pgdown":
			m.scroll = min(m.reviewLayout().MaxScroll, m.scroll+max(1, m.height/2))
		case "pgup":
			m.scroll = max(0, m.scroll-max(1, m.height/2))
		case "up", "k":
			m.scroll = max(0, m.scroll-1)
		}
		return m, nil, true
	}
	if k == "i" {
		next, cmd := m.activateReview("evidence")
		return next, cmd, true
	}
	if k == "a" || k == "b" {
		m.uiFocus = "review:enter"
		next, cmd := m.update(msg)
		return next, cmd, true
	}
	if k == "enter" && time.Since(m.reviewOpenedAt) < 200*time.Millisecond {
		return m, nil, true
	}
	if k == "enter" {
		m.uiFocus = ""
		n, c := m.update(msg)
		return n, c, true
	}
	return m, nil, false
}

func reviewActionLabel(action string) string {
	label := map[string]string{"start": "Start session", "stop": "Stop process", "remove": "Remove record", "resume": "Resume conversation", "query-resume": "Resume conversation", "review-reference": "Review saved conversation", "close-previous": "Close inspected shell", "rename": "Rename session"}[action]
	if label == "" {
		return "Review session"
	}
	return label
}
