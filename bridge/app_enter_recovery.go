package main

import (
	"errors"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func (m harborModel) isEnterRecovery() bool {
	return m.action == "query-resume" && (len(m.recoveryCandidates) > 0 || m.recoveryBlocked != "")
}

func (m harborModel) choosingRecoveryTarget() bool {
	return m.isEnterRecovery() && len(m.recoveryCandidates) > 1 && m.recoveryBlocked == "" && !m.recoveryRetry
}

func (m harborModel) selectedRecoveryTarget() session {
	if m.isEnterRecovery() && m.recoveryCursor >= 0 && m.recoveryCursor < len(m.recoveryCandidates) {
		return recoveryTarget(m.reviewed, m.recoveryCandidates[m.recoveryCursor].Reference)
	}
	return m.reviewed
}

func (m harborModel) inspectEnter(expected sessionView, recheck bool) (tea.Model, tea.Cmd) {
	m.reviewed = expected.session
	m.screen, m.notice = "busy", ""
	return m, func() tea.Msg {
		v, err := m.service.GetContext(m.refreshContext, expected.ID)
		msg := appSessionInspected{expected: expected, current: v, err: err, recheck: recheck}
		if err != nil {
			return msg
		}
		if v.Runtime.Presence == "observed" {
			if (!v.External && !v.Runtime.Associated && !v.Runtime.RunVerified) || (!recheck && expected.Runtime.Presence == "observed" && !sameFocusTarget(expected, v)) {
				msg.err = errors.New("terminal target changed; recheck before entering")
			}
			return msg
		}
		if v.Mode == "local" && v.CLI == "codex" {
			msg.nativeConfig = m.sessionNativeSummary(v.session)
		}
		msg.pane, msg.blocked = inspectRecoveryEnd(v, m.service.PreviousPane)
		if !v.Started && v.Runtime.Presence != "not_started" && msg.blocked == nil {
			msg.blocked = errors.New("session startup state is unavailable; recheck the workspace")
		}
		if v.Started {
			var candidateErr error
			msg.candidates, candidateErr = recoveryCandidates(v.session)
			if msg.blocked == nil {
				msg.blocked = candidateErr
			}
			if v.Profile == nil && msg.blocked == nil {
				msg.blocked = errors.New("review the original Profile binding in Profiles before recovery")
			}
			var names map[string]string
			if len(msg.candidates) > 0 {
				var metadataErr error
				names, msg.fingerprint, metadataErr = m.service.RecoveryMetadata(v.session)
				if msg.blocked == nil {
					msg.blocked = metadataErr
				}
			}
			for index := range msg.candidates {
				candidate := &msg.candidates[index]
				candidate.Title = names[candidate.Reference.Value]
				for _, evidence := range v.Candidates {
					if evidence.ID == candidate.Reference.Value && evidence.LastSeen.After(candidate.Seen) {
						candidate.Seen = evidence.LastSeen
					}
				}
				if c := v.Conversation; c != nil && c.Status == "observed" && c.Reference == candidate.Reference && c.ObservedAt.After(candidate.Seen) {
					candidate.Seen = c.ObservedAt
				}
				if v.Mode == "magpie" && v.CLI == "codex" && !joinedCodex(v.session) && msg.blocked == nil {
					selection, err := m.service.CodexSelection(m.refreshContext, recoveryTarget(v.session, candidate.Reference))
					candidate.SelectionErr = err
					if err == nil {
						candidate.Selection = &selection
					}
				}
			}
		}
		return msg
	}
}

func (m harborModel) finishEnterInspection(msg appSessionInspected) (tea.Model, tea.Cmd) {
	v := msg.current
	m.recoveryCandidates, m.recoveryCursor, m.recoveryBlocked, m.recoveryRetry = nil, -1, "", false
	m.previousPane = previousPane{}
	if !msg.recheck && msg.err == nil {
		if v.Runtime.Presence != "observed" && !sameSession(msg.expected.session, v.session) {
			msg.err = errors.New("record changed during inspection")
		}
		found := false
		for _, row := range m.sessions {
			if row.ID == msg.expected.ID {
				found = true
				if v.Runtime.Presence != "observed" && !sameSession(row.session, v.session) {
					msg.err = errors.New("record changed during inspection")
				}
			}
		}
		if !found {
			msg.err = errors.New("record disappeared during inspection")
		}
		if msg.err != nil && !msg.expected.Started {
			m.screen, m.notice = "sessions", msg.err.Error()
			return m, nil
		}
	}
	if msg.err == nil && v.Runtime.Presence == "observed" {
		return m, func() tea.Msg { return appOpened{m.service.FocusObserved(v)} }
	}
	m.action, m.screen, m.scroll = "query-resume", "confirm", 0
	m.nativeReview = msg.nativeConfig
	m.recoveryFingerprint = msg.fingerprint
	m.reviewedSelection, m.selectionReviewErr, m.selectionFallback = nil, nil, false
	if msg.err != nil {
		m.recoveryBlocked = msg.err.Error()
		return m, nil
	}
	m.reviewed, m.reviewedObservation = v.session, &v.Runtime
	m.previousPane = msg.pane
	m.recoveryCandidates = msg.candidates
	if msg.blocked != nil {
		m.recoveryBlocked = msg.blocked.Error()
	} else if !v.Started {
		m.action = "start"
	} else if len(msg.candidates) == 1 {
		m = m.chooseRecoveryCandidate(0)
	}
	return m, nil
}

func (m harborModel) chooseRecoveryCandidate(index int) harborModel {
	if index < 0 || index >= len(m.recoveryCandidates) || index == m.recoveryCursor || m.recoveryBlocked != "" || m.recoveryRetry {
		return m
	}
	m.recoveryCursor = index
	m.reviewedSelection = m.recoveryCandidates[index].Selection
	m.selectionReviewErr = m.recoveryCandidates[index].SelectionErr
	m.selectionFallback = false
	return m
}

func recoveryCandidateText(candidate recoveryCandidate) string {
	label := candidate.Label
	if !candidate.Seen.IsZero() {
		label += " · " + candidate.Seen.Local().Format("Jan 2 15:04")
	}
	if candidate.Title != "" {
		label += " · " + candidate.Title
	}
	return cleanUI(label)
}

func (m harborModel) recoveryBody(p *uiPage) {
	if m.recoveryBlocked != "" {
		for _, line := range strings.Split(ansi.Wrap(cleanUI(m.recoveryBlocked), max(1, p.Width-2), ""), "\n") {
			p.line(paint(fg("warn"), line))
		}
		return
	}
	if m.choosingRecoveryTarget() {
		for index, candidate := range m.recoveryCandidates {
			id := "recovery-candidate:" + strconv.Itoa(index)
			row := m.rowStyle(id, index == m.recoveryCursor, false)
			y := len(p.Lines)
			p.line(row.line(row.marker()+recoveryCandidateText(candidate), "text", p.Width-2))
			p.hit(id, 1, y, p.Width-2, 1)
		}
	} else if m.recoveryCursor >= 0 && m.recoveryCursor < len(m.recoveryCandidates) {
		p.field("", "💬", "Conversation", recoveryCandidateText(m.recoveryCandidates[m.recoveryCursor]), false, false)
	}
	if m.previousPane.ShellPID > 0 {
		p.line("The old shell stays open.")
	}
}
