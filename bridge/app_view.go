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
	lines := []string{"🚢 " + cleanUI(s.Name), statusDot(orUnknown(s.Status))}
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
	if m.screen == "confirm" || m.screen == "confirm-evidence" {
		return m.reviewLayout().view()
	}
	if m.screen == "sessions" || m.screen == "sessions-more" {
		return m.sessionsLayout().view()
	}
	width := max(20, min(140, m.width-4))
	crumb := "Sessions"
	footer := "↑↓ move  enter act  i details  / filter  , settings  ? help  q close"
	body := ""
	switch m.screen {
	case "accounts", "accounts-result", "accounts-busy":
		crumb = "Codex accounts · experimental"
		footer = "s switch  l login  esc back"
		body = "Codex accounts\n\nSwitch between replies, after all running Codex turns have finished.\n\n" +
			"s  Select an account with codex-auth\nl  Log in and add an account with Codex\n\n" +
			"Selection affects the shared Codex home. Only experimental Harbor sessions can be checked here.\n" +
			"Refreshing running sessions relies on an undocumented Codex behavior and may change."
		if m.screen == "accounts-result" {
			body = "Account action result\n\n" + m.accountResult
			footer = "esc back"
		} else if m.screen == "accounts-busy" {
			body = "Waiting for the account action."
			footer = ""
		}
	case "magpie-context-loading", "magpie-context", "magpie", "magpie-bind", "magpie-confirm", "magpie-busy", "magpie-filter", "magpie-detail":
		crumb = "Magpie"
		body = m.magpieView()
		footer = "↑↓ move/scroll  enter act  esc back  ? help  q close"
		if m.screen == "magpie-filter" || m.screen == "magpie-bind" {
			footer = "enter apply  esc cancel  ? help  ctrl+c close"
		}
		if m.screen == "magpie-detail" {
			footer = "↑↓ scroll  esc back  ? help  q close"
		}
		if m.screen == "magpie-busy" {
			body = "Working… Waiting for Magpie."
		}
	case "new-form", "new-directory", "new-project-choices", "new-project-loading", "new-instance-loading", "new-catalog-loading", "new-project", "new-cli", "new-connection", "new-name", "new-review", "new-instance", "new-catalog", "new-catalog-filter", "new-notice", "new-instance-detail", "new-instance-filter":
		crumb = "New session"
		body = m.newView()
		footer = "enter continue  esc back  ? help  ctrl+c close"
		if m.screen == "new-form" {
			footer = "tab / ↑↓ move  enter edit  esc cancel"
			if m.formFocus == 5 {
				footer = "enter create  tab / ↑↓ move  esc cancel"
			}
			if m.formFocus == 6 {
				footer = "enter cancel  tab / ↑↓ move"
			}
		}
		if m.screen == "new-name" && m.formActive {
			footer = "enter accept name  esc cancel edit"
		}
		if m.screen == "new-directory" {
			footer = "tab list  enter select  esc cancel"
			if m.picker.list {
				footer = "← parent → in  tab query  enter pick  esc back"
			}
		}
		if m.screen == "new-instance" {
			footer = "↑↓ move  / filter  i details  enter select  esc back"
		}
		if m.screen == "new-instance-detail" {
			footer = "↑↓ scroll  esc back  ? help  ctrl+c close"
		}
	case "error":
		crumb = "Action needs attention"
		body = "⚠ Something needs your attention\n\n" + m.notice
		footer = "↑↓ scroll  r refresh  esc dismiss  q close"
		if m.errorReturn == "magpie" && m.noticeCanStart {
			footer = "↑↓ scroll  r refresh  s start managed  esc dismiss"
		}
	case "help":
		crumb = "Keyboard help"
		footer = "↑↓ scroll  esc back  q close"
		body = m.helpView()
	case "delete-history":
		crumb = "Client history"
		body = historyRemovalExplanation
		footer = "enter / esc back"

	case "filter", "rename":
		crumb = "" + m.screen
		body = m.input.View()
		footer = "enter apply  esc cancel  ? help"
	case "confirm":
		crumb = "Review " + m.action
		body = m.confirmView()
		footer = "↑↓ scroll  enter confirm  esc cancel"
		if m.action == "query-resume" {
			footer = "↑↓ scroll  a accept  enter resume  esc cancel"
		}
		if (m.action == "query-resume" || m.action == "resume") && errors.Is(m.selectionReviewErr, errCodexProfileMissing) {
			footer = "↑↓ scroll  b use recorded  a accept  enter resume  esc cancel"
		}
		if m.action == "review-reference" {
			crumb = "Review saved identity"
			footer = "↑↓ scroll  a accept  enter save  esc cancel"
		}
	case "busy":
		body = "Working…  Waiting for the service to finish."
	case "detail":
		if s, ok := m.selected(); ok {
			crumb = "Session details"
			body = m.details(s)
		}
		footer = "esc back  ? help  q close"
	default:
		rows := m.visible()
		lw := width
		split := m.width >= 100
		if split {
			lw = max(35, width*45/100)
		}
		left := []string{fmt.Sprintf("🚢 Sessions · %d", len(rows)), m.searchLine(len(rows), len(m.sessions)), ""}
		if len(rows) == 0 {
			left = append(left, "No matching sessions", note("Press / to change the filter, or n to start one."))
		}
		count := max(1, (m.height-10)/3)
		start := m.listStart(m.cursor, count)
		for i := start; i < min(len(rows), start+count); i++ {
			s := rows[i]
			mark := "  "
			if i == m.cursor {
				mark = "› "
			}
			status := statusDot(orUnknown(s.Status))
			name := ansi.Truncate(cleanUI(mark+s.Name), max(8, lw-ansi.StringWidth(status)-3), "…")
			left = append(left, name+strings.Repeat(" ", max(2, lw-1-ansi.StringWidth(name)-ansi.StringWidth(status)))+status)
			meta := "    📂 " + s.WorkingDirectory + " · " + s.CLI
			if s.Mode == "magpie" {
				meta += " · 🐦 " + orUnknown(s.MagpieModel)
			} else if s.Mode != "" {
				meta += " · " + s.Mode
			}
			left = append(left, ansi.Truncate(cleanUI(meta), lw-1, "…"))
			reason := ""
			if s.Reason != "" {
				reason = ansi.Truncate(cleanUI("    ⚠ "+s.Reason), lw-1, "…")
			}
			left = append(left, reason)
		}
		body = strings.Join(left, "\n")
		if split {
			if s, ok := m.selected(); ok {
				h := m.height - 7
				lp := padLines(m.paintSearchBlock(strings.Split(fitBlock(body, lw, h), "\n"), lw-1, ""), h)
				rw := width - lw - 3
				detailLines := strings.Split(fitBlock(m.details(s), rw, 10000), "\n")
				start := min(m.detailScroll, max(0, len(detailLines)-h))
				rp := paintBlock(detailLines[start:min(len(detailLines), start+h)], rw, "")
				sep := paint(fg("muted"), "│")
				for i := range lp {
					r := ""
					if i < len(rp) {
						r = rp[i]
					}
					lp[i] = lp[i] + strings.Repeat(" ", max(0, lw-ansi.StringWidth(lp[i]))) + sep + "  " + r
				}
				return harborFrame(crumb, lp, m.feedbackLine(), m.noticeLine(), m.listFooter(footer), width, m.height, false)
			}
		}
	}
	if m.screen == "new-form" && m.height < 20 {
		rows := strings.Split(body, "\n")[2:9]
		context := "Native shares settings/login: " + m.environment.LocalHome
		if m.selectedClient() == "codex" && m.environment.NativeConfig != "" {
			context = m.environment.NativeConfig
		}
		if m.draft.Connection == "magpie" {
			context = "Auxiliary routes may differ; groups may change."
		}
		if m.notice != "" {
			context = "! details · " + m.notice
		}
		lines := []string{paint(fg("brand").Bold(true), " ⚓ HARBOR › New session")}
		lines = append(lines, paintBlock(rows, width, "")...)
		lines = append(lines, paint(fg("muted"), ansi.Truncate(cleanUI(context), width, "…")), paintKeys(ansi.Truncate(footer, width, "…")))
		return strings.Join(lines, "\n")
	}
	if m.searchList() {
		footer = m.searchHints()
	}
	mode := ""
	if m.screen == "help" {
		mode = "help"
	}
	if m.screen == "error" || m.screen == "detail" || m.screen == "magpie-detail" || m.screen == "new-instance-detail" || m.screen == "help" || m.screen == "magpie-confirm" || m.screen == "confirm" || m.screen == "new-review" || m.screen == "new-notice" {
		wrapped := strings.Split(fitBlock(body, width, 10000), "\n")
		start := min(m.scroll, max(0, len(wrapped)-max(1, m.height-7)))
		body = strings.Join(wrapped[start:], "\n")
	}
	content := m.paintSearchBlock(strings.Split(fitBlock(body, width, m.height-7), "\n"), width, mode)
	content = m.paintDirectoryMatches(content)
	return harborFrame(crumb, content, m.feedbackLine(), m.noticeLine(), m.listFooter(footer), width, m.height, false)
}

func (m harborModel) noticeLine() string {
	if m.searchList() && m.searchFocus == m.screen {
		return "Type to search · Tab switches focus · Esc clears."
	}
	if m.screen == "error" {
		return "Esc returns to the previous screen."
	}

	if strings.HasPrefix(m.screen, "magpie") {
		return "s start  x stop  b bind  u default  w web  r refresh"
	}
	if strings.HasPrefix(m.screen, "new-") {
		if m.formActive {
			return "Only Create saves this session."
		}
		return "Nothing is saved until the final review."
	}
	if m.screen == "sessions" {
		if s, ok := m.selected(); ok && !s.External && !s.Running && s.Runtime.Presence != "observed" {
			if _, err := savedReferenceForReview(s.session); err == nil {
				return "v review saved identity  n new  m Magpie  ? help"
			}
		}
	}
	if experimentalCodexAccounts() {
		return "n new  a accounts  m Magpie  s stop  x remove  r refresh"
	}
	return "n new  m Magpie  s stop  x remove  r refresh"
}

func padLines(lines []string, n int) []string {
	for len(lines) < n {
		lines = append(lines, "")
	}
	return lines
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
	body := ""
	switch {
	case strings.HasPrefix(m.previous, "magpie"):
		body = "🐦 Magpie instances\n\n↑/k ↓/j  Move selection or scroll review\n/        Filter instances\ni        Inspect identity and warnings\nEnter    Review management / confirm action\ns        Start managed instance\nx        Stop managed instance\nb        Bind an external running gateway\nu        Use selected instance for new sessions\nw        Open managed web UI\nr        Refresh identity and status\nEsc      Back / cancel\nq        Close Harbor\n\n• External processes are never started or stopped."
	case strings.HasPrefix(m.previous, "new-"):
		body = "✨ Create a session\n\nTab / Shift+Tab  Move form focus\n↑ ↓      Move form focus or picker selection\nEnter    Edit focused field; Create saves\nEnter in editor  Accept only that field\nEsc      Cancel local edit; on form cancel draft\n/        Search instances or model catalog\ni        Inspect selected instance\n?        Help from the form or a picker\nCtrl+C   Close Harbor\n\n• Text fields own ordinary characters.\n• Initial focus is Create. Review fields on the form.\n• Magpie acknowledgement returns to Create."

	default:
		body = m.sessionsHelp()
	}
	return body
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
