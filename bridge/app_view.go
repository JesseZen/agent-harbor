package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func cleanUI(s string) string {
	s = ansi.Strip(s)
	return strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, s)
}
func (m harborModel) details(s sessionView) string {
	lines := []string{"🚢 " + cleanUI(s.Name), "Profile: " + m.profileDisplayLabel(s.Profile), statusDot(orUnknown(s.Status))}
	lines = append(lines, kv("Presence", s.Runtime.Presence), kv("Runtime association", map[bool]string{true: "associated", false: "unassociated"}[s.Runtime.Associated]))
	if s.External {
		lines = append(lines, "Unmanaged")
	}
	if s.DiscoveryWarning != "" {
		lines = append(lines, s.DiscoveryWarning)
	}
	if s.External {
		return strings.Join(append(lines, "", "CLI: "+cleanUI(s.CLI), "Project: "+cleanUI(s.WorkingDirectory), "Connection intent: unknown", "Conversation intent: unknown", "Herdr pane: "+cleanUI(s.HerdrPane), "Enter focuses this agent. Manage it in its terminal; Harbor has no session record."), "\n")
	}
	if s.Reason != "" {
		lines = append(lines, "Needs attention: "+s.Reason)
	}
	lines = append(lines, "", "📂 WORKSPACE", kv("Project", s.WorkingDirectory), kv("CLI", strings.TrimSpace(s.CLI+" "+s.CLIVersion)))
	lines = append(lines, "", "🔌 CONNECTION", kv("Connection", s.Mode))
	if s.Mode == "magpie" {
		modelLabel := "Selected ID"
		if joinedCodex(s.session) {
			modelLabel = "Initial model"
		}
		lines = append(lines, kv(modelLabel, s.MagpieModel), kv("Instance", s.InstanceID), kv("Address", orUnknown(s.MagpieURL)))
		if s.InstanceSnapshot != nil {
			lines = append(lines, kv("Recorded config", s.InstanceSnapshot.ConfigDir))
		} else {
			lines = append(lines, kv("Identity", "unknown"))
		}
		if m.routeSessionID == s.ID && m.route.Observed {
			lines = append(lines, "", "🧭 OBSERVED ROUTE")
			if m.route.Main != nil {
				lines = append(lines, routeDetails("Main request", m.route.Main))
			} else {
				lines = append(lines, kv("Main request", "not observed"))
			}
			for _, route := range m.route.Auxiliary {
				lines = append(lines, routeDetails("Auxiliary request", route))
			}
		} else {
			lines = append(lines, kv("Observed route", "not observed"))
			if m.routeSessionID == s.ID && m.route.Reason != "" {
				lines = append(lines, note(m.route.Reason))
			}
		}
		lines = append(lines, note("An ID does not verify an unchanged destination."), note("Auxiliary calls may use other providers."), note("Audit in Magpie: Usage / Requests"))
	}
	if s.Conversation != nil {
		lines = append(lines, "", "UPSTREAM CONVERSATION", kv("Client", s.Conversation.Reference.Agent), kv("Reference kind", s.Conversation.Reference.Kind), kv("Reference", s.Conversation.Reference.Value), kv("Identity evidence", s.Conversation.Status), kv("Provenance", s.Conversation.Provenance), note("Recovery query: development candidate; query success proves syntax only."))
		if _, err := savedReferenceForReview(s.session); err == nil {
			lines = append(lines, note("v reviews the saved identity before recovery."))
		}
	}
	lines = append(lines, "", "💬 CONVERSATION", kv("Conversation", orUnknown(s.CodexSessionID)), kv("ID source", orUnknown(s.ConversationIDSource)), kv("Writer check", orUnknown(s.WriterLockCheck)))
	lines = append(lines, "", "🛟 RECOVERY", kv("Herdr agent", orUnknown(s.HerdrAgentName)), kv("Recovery", orUnknown(s.Recovery)))
	if s.HerdrPane != "" && !s.Running {
		lines = append(lines, kv("Previous Herdr pane", s.HerdrPane), note("c inspects a possible restored shell before closing it."))
	}
	for _, c := range s.Candidates {
		lines = append(lines, kv("Candidate", c.ID), kv("  Project", orUnknown(c.Directory)))
		if !c.LastSeen.IsZero() {
			lines = append(lines, kv("  Last seen", c.LastSeen.UTC().Format("2006-01-02 15:04:05 UTC")))
		}
	}
	if len(s.Candidates) == 0 {
		for _, id := range s.CandidateIDs {
			lines = append(lines, kv("Candidate", id+" (directory evidence unavailable)"))
		}
	}
	if s.IdentityWarning != "" {
		lines = append(lines, "⚠ Identity: "+s.IdentityWarning)
	}
	if s.NotifyWarning != "" {
		lines = append(lines, "⚠ Notify warning: "+s.NotifyWarning)
	}
	return strings.Join(lines, "\n")
}
func (m harborModel) View() string {
	if m.settings != nil {
		return m.settings.View()
	}
	if p, ok := m.additionalPage(); ok {
		return p.view()
	}
	if m.screen == "confirm" || m.screen == "confirm-evidence" {
		return m.reviewLayout().view()
	}
	if m.screen == "sessions" || m.screen == "sessions-more" {
		return m.sessionsLayout().view()
	}
	// Historical model states use the same semantic page shell as live routes.
	if strings.HasPrefix(m.screen, "new-") && !m.formActive {
		copy := m
		copy.formActive = true
		if p, ok := copy.newSessionPage(); ok {
			return p.view()
		}
	}
	body := m.notice
	if strings.HasPrefix(m.screen, "new-") {
		body = m.newView() + "\n\n" + m.notice
	}
	return secondaryDocument(m.width, m.height, "Harbor", body, []uiAction{{ID: "secondary:esc", Label: "Back", Key: "esc"}}, m.uiFocus, m.scroll).view()
}

func fitBlock(s string, width, height int) string {
	out := []string{}
	for _, line := range strings.Split(s, "\n") {
		line = cleanUI(line)
		trim := strings.TrimLeft(line, " ")
		if hang := len(line) - len(trim) + 2; (strings.HasPrefix(trim, "• ") || strings.HasPrefix(trim, "⚠ ")) && width > hang+10 {
			// Hanging indent keeps wrapped notes aligned under their text.
			parts := strings.Split(ansi.Wrap(line, width, ""), "\n")
			if len(parts) > 1 {
				rest := strings.Split(ansi.Wrap(strings.Join(parts[1:], " "), width-hang, ""), "\n")
				for i := range rest {
					rest[i] = strings.Repeat(" ", hang) + rest[i]
				}
				parts = append(parts[:1], rest...)
			}
			out = append(out, parts...)
			continue
		}
		out = append(out, strings.Split(ansi.Wrap(line, max(1, width), ""), "\n")...)
	}
	if len(out) > height {
		out = out[:height]
		if height > 0 {
			out[height-1] = ansi.Truncate(out[height-1], max(1, width-1), "") + "…"
		}
	}
	return strings.Join(out, "\n")
}

func (m harborModel) helpView() string {
	parent := m
	parent.screen = m.previous
	actions := []uiAction{}
	if p, ok := parent.additionalPage(); ok {
		actions = p.Actions
	}
	if m.previous == "confirm" || m.previous == "confirm-evidence" {
		actions = parent.reviewActions()
	}
	if len(actions) > 0 {
		lines := []string{"Harbor · " + m.previous, "", "Tab / Shift+Tab  Move control focus", "↑↓  Select a list row or scroll text", "Enter  Apply the focused control", "/  Search a list; text input owns ordinary characters", "Esc  Clear search, leave its field, then cancel the local page", "PgUp/PgDown  Scroll long documents", ""}
		seen := map[string]bool{}
		for _, a := range actions {
			if a.Key != "" && !seen[a.ID] {
				seen[a.ID] = true
				lines = append(lines, actionHints([]uiAction{a}))
			}
		}
		if strings.HasPrefix(m.previous, "new-") {
			lines = append(lines, "", "Only Create saves and launches a session; field edits and connection Apply update the draft.")
		}
		return strings.Join(lines, "\n")
	}
	return m.sessionsHelp()
}

func (m harborModel) confirmView() string {
	model := m.reviewed.MagpieModel
	if (m.action == "resume" || m.action == "query-resume") && joinedCodex(m.reviewed) {
		model = "selection restored by Codex"
	}
	if (m.action == "resume" || m.action == "query-resume") && m.reviewedSelection != nil {
		model = m.reviewedSelection.Model
	}
	lines := []string{"🚢 " + m.reviewed.Name, "", kv("Project", m.reviewed.WorkingDirectory), kv("Client", orUnknown(m.reviewed.CLI)), kv("Connection", strings.TrimSpace(m.reviewed.Mode+" "+model))}
	if m.reviewed.Mode == "local" && (m.action == "resume" || m.action == "query-resume" || m.action == "review-reference") {
		lines = append(lines, note("Native route or login may have changed; verify both."), note("A provider name does not verify its destination."))
	}
	if m.action == "review-reference" && m.reviewed.Conversation != nil {
		c := m.reviewed.Conversation
		lines = append(lines, kv("Saved source", c.Reference.Source), kv("Saved agent", c.Reference.Agent), kv("Saved kind", c.Reference.Kind), kv("Saved reference", c.Reference.Value), kv("Observation", c.Status))
		if m.reviewed.Mode == "magpie" {
			lines = append(lines, kv("Instance", m.reviewed.InstanceID+" "+m.reviewed.MagpieURL))
			if m.reviewed.InstanceSnapshot != nil {
				lines = append(lines, kv("Recorded config", m.reviewed.InstanceSnapshot.ConfigDir))
			}
		} else if m.nativeReview != "" {
			lines = append(lines, note(m.nativeReview))
		}
		lines = append(lines, "", note("The saved identity is not currently verified. Review the original conversation and connection."), note("Choose Use saved conversation, then Save reviewed reference. This does not resume the conversation."))
		if m.referenceAccepted {
			lines = append(lines, "Saved identity uncertainty explicitly accepted.")
		}
		return strings.Join(lines, "\n")
	}
	if (m.action == "resume" || m.action == "query-resume") && m.reviewed.Mode == "local" && m.reviewed.CLI == "codex" && m.nativeReview != "" {
		lines = append(lines, note(m.nativeReview))
	}
	if m.reviewed.Conversation != nil {
		reference := m.reviewed.Conversation.Reference
		lines = append(lines, kv("Reference kind", orUnknown(reference.Kind)), kv("Conversation", orUnknown(reference.Value)))
	} else {
		lines = append(lines, kv("Conversation", orUnknown(m.reviewed.CodexSessionID)))
	}
	lines = append(lines, "")
	switch m.action {
	case "close-previous":
		lines = append(lines, "🧹 Close previous idle shell", kv("Pane", m.previousPane.PaneID), note("The original CLI is no longer running."), note("This closes only the inspected shell pane."), note("Conversation history and the Harbor record are kept."))
	case "stop":
		lines = append(lines, "⚡ Stop this process", note("The conversation stays resumable."))
	case "remove":
		lines = append(lines, "🗑 Remove only the Harbor record", note("Codex history is kept."))
	case "query-resume":
		lines = append(lines, "Development query candidate", note("Query proves syntax only. Conversation existence, login and external writers cannot be verified."), note("Choose Use recorded conversation before resuming this object. Login and other writers remain unverified."))
		if m.queryAccepted {
			lines = append(lines, "Uncertainty explicitly accepted.")
		}
		fallthrough
	case "resume":
		lines = append(lines, "🚀 Resume this conversation", note("Confirm the intended account and no other writer."))
		if m.reviewed.Mode == "magpie" {
			lines = append(lines, kv("Instance", m.reviewed.InstanceID+" "+m.reviewed.MagpieURL), note("Groups may change members; an ID is not proof."), note("Review in Magpie before confirming."))
			if joinedCodex(m.reviewed) {
				lines = append(lines, note("Codex restores the conversation's last model and reasoning setting."))
			}
			if m.reviewedSelection != nil {
				effort := m.reviewedSelection.Effort
				if effort == "" {
					effort = "not recorded; Codex settings may apply"
				}
				lines = append(lines, kv("Session default", m.reviewedSelection.Model), kv("Reasoning", effort))
				if m.selectionFallback {
					lines = append(lines, note("Using Harbor's original model by explicit choice; later Codex changes are unknown."))
				}
			} else if m.selectionReviewErr != nil {
				lines = append(lines, note("Session profile unavailable: "+m.selectionReviewErr.Error()))
				if errors.Is(m.selectionReviewErr, errCodexProfileMissing) {
					lines = append(lines, note("Recorded model may differ. Press b to explicitly choose it, or Esc to cancel."))
				} else {
					lines = append(lines, note("Resolve the profile or identity problem before recovery."))
				}
			}
		}
	}
	return strings.Join(lines, "\n")
}

// Clamp the stored offset as well as the rendered page, so one Up always
// moves immediately after repeated Down presses at the end of a document.
func (m harborModel) scrollLimit() int {
	body := ""
	switch m.screen {
	case "error":
		body = "⚠ Something needs your attention\n\n" + m.notice
	case "detail":
		if s, ok := m.selected(); ok {
			body = m.details(s)
		}
	case "help":
		body = m.helpView()
	case "magpie-confirm", "magpie-detail":
		body = m.magpieView()
	case "confirm":
		body = m.confirmView()
	case "new-review", "new-notice", "new-instance-detail":
		body = m.newView()
	}
	return max(0, len(strings.Split(fitBlock(body, max(20, min(140, m.width-4)), 10000), "\n"))-max(1, m.height-7))
}

func routeDetails(label string, value any) string {
	r, ok := value.(routeRecord)
	if !ok {
		return label + "  " + fmt.Sprint(value)
	}
	lines := []string{label, "  Provider/model  " + orUnknown(r.Model)}
	if r.Kind != "" {
		lines = append(lines, "  Kind  "+r.Kind)
	}
	if r.Asked != "" {
		lines = append(lines, "  Requested  "+r.Asked)
	}
	if r.Group != "" {
		lines = append(lines, "  Group  "+r.Group)
	}
	if r.Served != "" {
		lines = append(lines, "  Served model  "+r.Served)
	}
	for _, attempt := range r.Tries {
		lines = append(lines, fmt.Sprintf("  Attempt  %s · HTTP %d", attempt.Model, attempt.Status))
	}
	lines = append(lines, "  Request time  unavailable from route endpoint")
	return strings.Join(lines, "\n")
}

func (m harborModel) deleteDetails() string  { return historyRemovalExplanation }
func (m harborModel) deleteScrollLimit() int { return 0 }
