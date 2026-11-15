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
	if m.action == "query-resume" && !m.queryAccepted {
		out = append(out, uiAction{ID: "review:a", Key: "a", Label: "Use recorded conversation"})
	}
	if (m.action == "query-resume" || m.action == "resume") && errors.Is(m.selectionReviewErr, errCodexProfileMissing) && m.reviewedSelection == nil {
		out = append(out, uiAction{ID: "review:b", Key: "b", Label: "Use recorded model"})
	}
	label := map[string]string{"start": "Start session", "stop": "Stop process", "remove": "Remove record", "resume": "Resume conversation", "query-resume": "Resume conversation", "review-reference": "Save reviewed reference", "close-previous": "Close inspected shell"}[m.action]
	disabled := ""
	if m.action == "review-reference" && !m.referenceAccepted {
		disabled = "Choose the saved conversation first"
	}
	if m.action == "query-resume" && !m.queryAccepted {
		disabled = "Choose the recorded conversation first"
	}
	if (m.action == "query-resume" || m.action == "resume") && m.reviewed.Mode == "magpie" && m.reviewed.CLI == "codex" && !joinedCodex(m.reviewed) && m.reviewedSelection == nil {
		disabled = "Resolve the session model before recovery"
	}
	return append(out, uiAction{ID: "review:enter", Key: "enter", Label: label, Primary: true, Disabled: disabled}, uiAction{ID: "evidence", Key: "i", Label: "Inspect evidence"}, uiAction{ID: "review:esc", Key: "esc", Label: "Cancel"})
}
func (m harborModel) reviewEvidence() []string {
	return strings.Split(ansi.Wrap(cleanUI(m.confirmView()+"\n\n"+m.notice), max(1, m.width-2), ""), "\n")
}
func (m harborModel) reviewLayout() uiPage {
	p := newUIPage(m.width, m.height)
	p.line(paint(fg("brand"), "⚓ Harbor · Review "+m.action))
	if m.screen == "confirm-evidence" {
		lines := m.reviewEvidence()
		count := max(1, m.height-3)
		start := min(m.scroll, max(0, len(lines)-count))
		for _, line := range lines[start:min(len(lines), start+count)] {
			p.line(line)
		}
		for len(p.Lines) < m.height-2 {
			p.line("")
		}
		p.button(uiAction{ID: "review-back", Label: "Back to decision", Key: "esc"}, true)
		p.line(paintKeys("↑↓ scroll  Esc back"))
		return p
	}
	p.line(paint(fg("text"), cleanUI(m.reviewed.Name)))
	p.field("", "📂", "Project", cleanUI(m.reviewed.WorkingDirectory), false, false)
	p.field("", "🤖", "Client", cleanUI(m.reviewed.CLI), false, false)
	connection := m.reviewed.Mode
	if connection == "local" {
		connection = "Native · client settings and login"
	} else {
		connection += " · " + m.reviewed.InstanceID + " / " + m.reviewed.MagpieModel
	}
	p.field("", "🔌", "Connection", cleanUI(connection), false, false)
	conversation := m.reviewed.CodexSessionID
	if m.reviewed.Conversation != nil {
		conversation = m.reviewed.Conversation.Reference.Value
	}
	if m.action != "start" {
		p.field("", "💬", "Conversation", orUnknown(conversation), false, false)
	}
	impact := map[string]string{"start": "Start this client with the recorded connection.", "stop": "The conversation stays resumable.", "remove": "Codex history is kept. Remove only the Harbor record.", "review-reference": "Save this review only. This does not resume the conversation.", "query-resume": "Query proves syntax only. Login, conversation existence and other writers are not verified.", "resume": "Resume this conversation with its recorded connection.", "close-previous": "Close only the inspected idle shell; keep conversation and record."}[m.action]
	for _, line := range strings.Split(ansi.Wrap(impact, max(1, m.width-2), ""), "\n") {
		p.line(line)
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
	if m.reviewed.Mode == "local" && (m.action == "resume" || m.action == "query-resume" || m.action == "review-reference") {
		for _, line := range strings.Split(ansi.Wrap("Native route or login may have changed; verify both. A provider name does not verify its destination. "+cleanUI(m.nativeReview), max(1, m.width-2), ""), "\n") {
			p.line(line)
		}
	}
	actions := m.reviewActions()
	// Reserve actions first; short popups fold the object summary above them.
	actionRows := len(actions)
	bodyLimit := max(1, m.height-actionRows-1)
	if len(p.Lines) > bodyLimit {
		p.Lines = p.Lines[:bodyLimit]
		p.Hits = nil
	}
	for _, a := range actions {
		p.button(a, m.uiFocus == a.ID)
	}
	p.line(paintKeys("Tab focus  Enter action  i evidence  Esc Cancel"))
	return p
}
func (m harborModel) activateReview(id string) (tea.Model, tea.Cmd) {
	m.uiFocus = ""
	m.mouse = mouseClick{}
	if id == "evidence" {
		m.screen = "confirm-evidence"
		m.scroll = 0
		return m, nil
	}
	if id == "review-back" {
		m.screen = "confirm"
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
	if k == "tab" || k == "shift+tab" {
		hits := m.reviewLayout().Hits
		ids := []string{}
		for _, r := range hits {
			if r.ID != "" {
				ids = append(ids, r.ID)
			}
		}
		if len(ids) == 0 {
			return m, nil, true
		}
		at := -1
		for i, id := range ids {
			if id == m.uiFocus {
				at = i
			}
		}
		delta := 1
		if k == "shift+tab" {
			delta = -1
		}
		if at < 0 {
			at = 0
			delta = 0
		}
		m.uiFocus = ids[(at+delta+len(ids))%len(ids)]
		return m, nil, true
	}
	if m.screen == "confirm-evidence" {
		switch k {
		case "esc", "enter", "i":
			m.screen = "confirm"
			m.scroll = 0
		case "down", "j":
			m.scroll = min(max(0, len(m.reviewEvidence())-max(1, m.height-3)), m.scroll+1)
		case "up", "k":
			m.scroll = max(0, m.scroll-1)
		}
		return m, nil, true
	}
	if k == "i" || k == "down" || k == "j" || k == "up" || k == "k" {
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
	if k == "enter" && m.uiFocus != "" {
		id := m.uiFocus
		m.uiFocus = ""
		next, cmd := m.activateReview(id)
		return next, cmd, true
	}
	return m, nil, false
}
