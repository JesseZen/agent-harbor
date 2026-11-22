package main

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m harborModel) accountActions() []uiAction {
	switch m.screen {
	case "accounts-busy":
		if m.accountWaiting != nil {
			return []uiAction{{ID: "account:cancel-wait", Label: "Cancel switch", Key: "esc"}}
		}
		return nil
	case "accounts-resetting":
		return nil
	case "accounts-signout-confirm":
		return []uiAction{{ID: "account:confirm-signout", Label: "Sign out", Key: "y"}, {ID: "account:cancel-signout", Label: "Cancel", Key: "esc"}}
	case "accounts-reset-confirm":
		return []uiAction{{ID: "account:cancel-reset", Label: "Cancel", Key: "esc"}, {ID: "account:confirm-reset", Label: "Confirm use", Key: "y"}}
	case "accounts-detail":
		back := uiAction{ID: "account:back", Label: "Back to accounts", Key: "esc"}
		if m.canResetAccount() {
			return []uiAction{{ID: "account:use-reset", Label: "Use reset card", Key: "u"}, back}
		}
		return []uiAction{back}
	case "accounts-result":
		return []uiAction{{ID: "account:back", Label: "Back to accounts", Key: "esc"}}
	}
	var actions []uiAction
	if m.uiFocus != accountRowPrefix {
		actions = append(actions, uiAction{ID: "account:i", Label: "Details", Key: "enter"})
	} else if m.canSignOutAccount() {
		actions = append(actions, uiAction{ID: "account:o", Label: "Sign out", Key: "enter"})
	}
	if account, ok := m.selectedAccount(); ok && !m.currentAccount(account.Key) {
		actions = append(actions, uiAction{ID: "account:s", Label: "Use selected login", Key: "s", Primary: true})
	}
	actions = append(actions, []uiAction{
		{ID: "account:" + accountLoginKey, Label: "Add login", Key: accountLoginKey},
		{ID: "account:?", Label: "Help", Key: "?"},
		{ID: "account:p", Label: "Choose Profile", Key: "p"},
		{ID: "account:back", Label: "Back", Key: "esc"},
		{ID: "account:r", Label: "Refresh", Key: "r"},
	}...)
	if m.uiFocus != accountRowPrefix && m.canSignOutAccount() {
		actions = append(actions, uiAction{ID: "account:signout-shortcut", Label: "Sign out", Key: "o"})
	}
	return append(actions, uiAction{ID: "account:,", Label: "Settings", Key: ","})
}

func (m harborModel) accountContent() uiPage {
	p := newUIPage(m.width, 10000)
	wrap := func(text string) {
		for _, paragraph := range strings.Split(text, "\n") {
			for _, line := range strings.Split(ansi.Wrap(cleanUI(paragraph), max(1, m.width-2), ""), "\n") {
				p.line(documentLine(line))
			}
		}
	}
	p.line(paint(fg("brand"), "👤 Harbor · Accounts"))
	if m.screen == "accounts" {
		p.tabs("accounts", m.uiHover, m.uiFocus)
		m.accountOverview(&p)
	} else if m.screen == "accounts-signout-confirm" {
		p.line(paint(fg("section"), "Sign out of Codex"))
		if m.accountProfile != nil && m.accountSnapshot != nil {
			p.line(kvw("Profile", cleanUI(orUnknown(m.accountSnapshot.ProfileName)), 9))
		}
		wrap("Other saved logins stay available.")
	} else if m.screen == "accounts-detail" || m.screen == "accounts-reset-confirm" || m.screen == "accounts-resetting" {
		m.accountDetail(&p, wrap)
	} else if m.screen == "accounts-busy" {
		if m.accountWaiting != nil {
			p.line(paint(fg("section"), "Waiting to change login"))
			wrap(m.accountWaitReason)
		} else {
			wrap("Updating Codex login…")
		}
	} else {
		p.line(paint(fg("section"), "Account action result"))
		if m.accountResult == "" {
			wrap("Unknown · no account verification result was returned.")
		} else {
			wrap(m.accountResult)
		}
	}
	return p
}

func (m harborModel) accountDetail(p *uiPage, wrap func(string)) {
	p.line(paint(fg("section"), "Account details"))
	a, ok := m.selectedAccount()
	if !ok {
		if m.accountSnapshot != nil {
			wrap(accountResultText(*m.accountSnapshot))
			wrap("Codex home: " + m.accountSnapshot.Home)
		}
		wrap("Switch between replies. Wait for the result before sending another prompt.")
		return
	}

	const valueColumn = 14
	valueWidth := max(1, p.Width-2-valueColumn)
	field := func(label, value string) {
		for _, line := range strings.Split(ansi.Wrap(value, valueWidth, ""), "\n") {
			p.line(kvw(label, line, valueColumn))
			label = ""
		}
	}
	field("Profile", cleanUI(m.accountSnapshot.ProfileName))
	field("Email", accountName(a))
	if a.Alias != "" && a.Alias != a.Email {
		field("Alias", cleanUI(a.Alias))
	}
	if a.Workspace != "" {
		field("Workspace", cleanUI(a.Workspace))
	}
	plan := accountPlan(a)
	if a.PlanExpires != nil && *a.PlanExpires > 0 {
		plan += " · ends " + time.Unix(*a.PlanExpires, 0).Local().Format(weeklyResetFormat)
	}
	field("Plan", plan)
	if m.accountSnapshot.State == "expired" && m.accountSnapshot.Selected == a.Key {
		p.line(kvw("Login", paint(fg("warn"), "Expired"), valueColumn))
	}

	if m.screen == "accounts-resetting" {
		p.line("")
		p.line(paint(fg("warn"), "Using reset card…"))
		return
	}
	if m.screen == "accounts-reset-confirm" {
		p.line("")
		wrap("Use one reset card for this account?")
		return
	}

	if m.height > 16 {
		p.line("")
	}
	field("5h left", accountQuotaCell(a.Usage, m.accountUsagePending, 300, min(valueWidth, shortQuotaWidth)))
	field("7d left", accountQuotaCell(a.Usage, m.accountUsagePending, 10080, min(valueWidth, weeklyQuotaWidth)))
	switch {
	case m.accountUsageError != "":
		wrap(m.accountUsageError)
	case a.Usage != nil && !m.accountUsagePending:
		switch a.Usage.Refresh.Status {
		case "missing_auth":
			wrap("Usage lookup needs a valid saved ChatGPT login.")
		case "http_error":
			wrap(fmt.Sprintf("Usage lookup failed (HTTP %d).", a.Usage.Refresh.HTTPStatus))
		case "error":
			wrap("Usage lookup failed.")
		}
	}
	if m.height > 16 {
		p.line("")
	}
	status := "Unavailable"
	switch {
	case m.accountUsagePending && !a.Usage.current():
		status = "Checking…"
	case !a.Usage.current() || a.Usage.ResetCount == nil:
	case *a.Usage.ResetCount <= 0:
		status = "None available"
	default:
		status = fmt.Sprintf("%d available", *a.Usage.ResetCount)
	}
	field("Reset cards", status)
	if a.Usage.current() && a.Usage.ResetCount != nil && *a.Usage.ResetCount > 0 {
		label := "Expires"
		for _, card := range a.Usage.ResetCredits {
			if expiry := card.expiration(); card.Status == "available" && !expiry.IsZero() {
				field(label, expiry.Local().Format(weeklyResetFormat))
				label = ""
			}
		}
		if !a.Usage.hasResetCard() {
			field("", "Card details unavailable")
		}
	}
	if m.accountResetNotice != "" {
		wrap(m.accountResetNotice)
	}
}

func (m harborModel) accountOverview(p *uiPage) {
	label := "None selected"
	if m.accountProfile != nil {
		label = m.profileDisplayLabel(m.accountProfile)
		if m.accountSnapshot != nil && m.accountSnapshot.ProfileName != "" {
			label = m.accountSnapshot.ProfileName
		}
	}
	p.line(kvw("Profile", cleanUI(label), 9))
	if p.Width >= 60 {
		p.line("")
	}
	status := "Select a Profile"
	if m.accountProfile != nil {
		status = "Checking…"
		if m.accountSnapshot != nil {
			switch m.accountSnapshot.State {
			case "selected", "signed-out":
				status = ""
			case "expired":
				status = "Login expired"
			case "unavailable":
				status = "Unavailable"
			default:
				status = "Login unconfirmed"
			}
		}
	}
	appendAccountClient(p, "Codex", status)
	if snapshot := m.accountSnapshot; m.accountProfile != nil && snapshot != nil {
		m.appendCodexAccountTable(p)
		if len(snapshot.Accounts) == 0 && snapshot.State == "signed-out" {
			p.emptyState("  No saved logins")
		}
	}
	if p.Width >= 60 {
		p.line("")
	}
	appendAccountClient(p, "Claude Code", "Not supported")
	appendAccountClient(p, "Antigravity", "Not supported")
}

func appendAccountClient(p *uiPage, name, status string) {
	title := " " + name
	width := max(1, p.Width-2)
	status = ansi.Truncate(status, max(0, width-ansi.StringWidth(title)-3), "…")
	padding := max(0, width-ansi.StringWidth(title)-ansi.StringWidth(status)-1)
	band := lipgloss.Color(harborPalette["button"])
	p.line(paint(fg("text").Background(band).Bold(true), title) +
		paint(fg("value").Background(band), strings.Repeat(" ", padding)+status+" "))
}

const (
	shortResetFormat  = "3:04 PM"
	weeklyResetFormat = "Jan 02 Mon 3:04 PM"
	shortQuotaWidth   = 4 + 1 + len("12:42 PM")
	weeklyQuotaWidth  = 4 + 1 + len("Nov 18 Wed 12:42 PM")
)

const accountRowPrefix = "account-row:"

func (m harborModel) appendCodexAccountTable(p *uiPage) {
	snapshot := m.accountSnapshot
	// Four columns share one row. Extra space first buys reset timestamps,
	// then a meter in each quota column.
	const rowOverhead = 10 // page padding, selection marker, three column gaps
	planWidth, shortWidth, weeklyWidth := 5, 7, 7
	if p.Width >= 55 {
		planWidth = 8
		shortWidth = shortQuotaWidth
	}
	if p.Width >= 70 {
		weeklyWidth = weeklyQuotaWidth
	}
	if p.Width >= 88 {
		meterWidth := min(10, 5+(p.Width-88)/3)
		shortWidth += meterWidth
		weeklyWidth += meterWidth
	}
	emailWidth := max(1, p.Width-rowOverhead-planWidth-shortWidth-weeklyWidth)
	if len(snapshot.Accounts) > 0 {
		p.line("  " + accountTableCell(paint(fg("key"), "Email"), emailWidth) + "  " +
			accountTableCell(paint(fg("key"), "Plan"), planWidth) + "  " +
			accountTableCell(paint(fg("key"), "5h left"), shortWidth) + "  " + paint(fg("key"), "7d left"))
	}
	row := m.rowStyle(accountRowPrefix, m.uiFocus == accountRowPrefix, snapshot.State == "signed-out")
	const noLoginLabel = "Not signed in"
	label := row.marker() + accountTableCell(noLoginLabel, max(len(noLoginLabel)+1, emailWidth-2))
	line := paint(row.style("text"), label)
	if row.selected {
		line += paint(row.style("section"), "✓")
	}
	p.hit(accountRowPrefix, 1, len(p.Lines), p.Width-2, 1)
	p.line(line + row.line("", "value", max(0, p.Width-2-ansi.StringWidth(line))))
	for _, account := range snapshot.Accounts {
		id := accountRowPrefix + account.Key
		row := m.rowStyle(id, account.Key != "" && id == m.uiFocus, m.currentAccount(account.Key))
		usage := account.Usage
		nameWidth := emailWidth
		icons := ""
		if emailWidth >= 5 {
			nameWidth -= 3
			selected, reset := " ", " "
			if row.selected {
				selected = "✓"
			}
			if usage.current() && usage.ResetCount != nil && *usage.ResetCount > 0 {
				reset = "↻"
			}
			if asciiUI() {
				reset = strings.ReplaceAll(reset, "↻", "+")
			}
			icons = paint(row.style("value"), " ") + paint(row.style("section"), selected) + paint(row.style("warn"), reset)
		}
		gap := paint(row.style("value"), "  ")
		cell := func(minutes, width int) string {
			value := ansi.Truncate(accountQuotaCellStyled(usage, m.accountUsagePending, minutes, width, row.style), width, "…")
			return value + paint(row.style("value"), strings.Repeat(" ", width-ansi.StringWidth(value)))
		}
		line := paint(row.style("text"), row.marker()+accountTableCell(accountName(account), nameWidth)) + icons + gap +
			paint(row.style("value"), accountTableCell(accountPlan(account), planWidth)) + gap +
			cell(300, shortWidth) + gap + cell(10080, weeklyWidth)
		if account.Key != "" {
			p.hit(id, 1, len(p.Lines), p.Width-2, 1)
		}
		p.line(line)
	}
}

func accountName(a savedCodexAccount) string {
	if email := cleanUI(a.Email); email != "" {
		return email
	}
	return orUnknown(cleanUI(a.Alias))
}

func accountPlan(a savedCodexAccount) string {
	if plan := cleanUI(a.Plan); plan != "" {
		return plan
	}
	return "—"
}

func accountTableCell(value string, width int) string {
	value = ansi.Truncate(value, width, strings.Repeat(".", min(3, width)))
	return value + strings.Repeat(" ", max(0, width-ansi.StringWidth(value)))
}

func accountQuotaCell(usage *savedAccountUsage, pending bool, minutes, width int) string {
	return accountQuotaCellStyled(usage, pending, minutes, width, fg)
}

func accountQuotaCellStyled(usage *savedAccountUsage, pending bool, minutes, width int, style func(string) lipgloss.Style) string {
	// The roster's registry or local snapshot can supply display-only quota
	// while the API is in flight. Reset cards still require a live result.
	if usage.current() || (pending && usage != nil && (usage.Source == "cache" || usage.Source == "local")) {
		for _, window := range []*savedUsageWindow{usage.Primary, usage.Secondary} {
			if window == nil || window.UsedPercent == nil || window.Minutes != minutes {
				continue
			}
			remaining := int(math.Round(100 - min(100, max(0, *window.UsedPercent))))
			role := quotaRemainingRole(remaining)
			value := paint(style(role), fmt.Sprintf("%-4s", fmt.Sprint(remaining)+"%"))
			resetFormat, quotaWidth := shortResetFormat, shortQuotaWidth
			if minutes == 10080 {
				resetFormat, quotaWidth = weeklyResetFormat, weeklyQuotaWidth
			}
			if window.ResetsAt > 0 && width >= quotaWidth {
				reset := time.Unix(window.ResetsAt, 0).Local().Format(resetFormat)
				value += paint(style("value"), " "+reset)
			}
			if cells := width - quotaWidth; cells >= 5 {
				value = accountQuotaMeter(remaining, cells-1, role, style) + paint(style("value"), " ") + value
			}
			return value
		}
	}
	if pending {
		return paint(style("value"), "...")
	}
	return paint(style("value"), "N/A")
}

func quotaRemainingRole(remaining int) string {
	switch {
	case remaining <= 20:
		return "bad"
	case remaining <= 40:
		return "warn"
	default:
		return "ok"
	}
}

func accountQuotaMeter(remaining, cells int, role string, style func(string) lipgloss.Style) string {
	filled := (remaining*cells + 50) / 100
	if remaining > 0 && remaining < 100 {
		filled = min(cells-1, max(1, filled))
	}
	fill, track := "▆", "▆"
	if asciiUI() {
		fill, track = "=", "-"
	}
	return paint(style(role), strings.Repeat(fill, filled)) + paint(style("border"), strings.Repeat(track, cells-filled))
}

func (m harborModel) accountsPage() (uiPage, bool) {
	if m.screen != "accounts" && !strings.HasPrefix(m.screen, "accounts-") {
		return uiPage{}, false
	}
	content := m.accountContent()
	footer := newUIPage(m.width, 10000)
	actions := m.accountActions()
	footer.shortcuts(actions, m.uiFocus)
	if m.screen != "accounts-busy" && m.screen != "accounts-reset-confirm" && m.screen != "accounts-resetting" && m.screen != "accounts-signout-confirm" {
		footer.Hint = scrollHint
	}
	if m.screen == "accounts" {
		footer.Hint = "↑↓/jk choose · " + tabHint + " · PgUp/PgDn scroll"
	}
	return content.withFooter(footer, m.height, m.scroll), true
}

func (m harborModel) accountActivate(id string) (tea.Model, tea.Cmd) {
	if strings.HasPrefix(id, "nav:") {
		return m.activateSessions(id)
	}
	if m.screen == "accounts" && (id == "account:i" || strings.HasPrefix(id, accountRowPrefix)) {
		if id == accountRowPrefix {
			return m.updateAccounts("o")
		}
		if strings.HasPrefix(id, accountRowPrefix) {
			m.accountRow = strings.TrimPrefix(id, accountRowPrefix)
		}
		m.accountListScroll = m.scroll
		m.screen = "accounts-detail"
		m.scroll = 0
		m.accountResetNotice = ""
		return m, nil
	}
	switch id {
	case "account:cancel-wait":
		if m.accountWaiting == nil {
			return m, nil
		}
		m.accountWaiting.login.close()
		m.accountWaiting, m.accountWaitReason = nil, ""
		m.screen, m.scroll = "accounts", m.accountListScroll
		m.reconcileAccountRow()
		return m, m.loadAccountUsage()
	case "account:o", "account:signout-shortcut":
		return m.updateAccounts("o")
	case "account:confirm-signout":
		return m.runAccountAction("signed-out", "")
	case "account:cancel-signout":
		m.screen, m.scroll = "accounts", m.accountListScroll
		return m, nil
	case "account:use-reset":
		return m.reviewAccountReset()
	case "account:confirm-reset":
		return m.submitAccountReset()
	case "account:cancel-reset":
		m.accountResetReview = ""
		m.screen, m.scroll = "accounts-detail", 0
		return m, nil
	case "account:?":
		return m.Update(uiKey("?"))
	case "account:back":
		if m.screen == "accounts-detail" || m.screen == "accounts-result" {
			m.screen = "accounts"
			m.scroll = m.accountListScroll
			m.uiFocus = accountRowPrefix + m.accountRow
			m.reconcileAccountRow()
			return m, nil
		}
		m.scroll = 0
		return m.updateAccounts("esc")
	}
	for _, a := range m.accountActions() {
		if a.ID == id {
			return m.updateAccounts(a.Key)
		}
	}
	return m, nil
}

func (m *harborModel) reconcileAccountRow() {
	if m.accountSnapshot == nil {
		return
	}
	if _, ok := m.selectedAccount(); !ok {
		m.accountRow = ""
		if m.uiFocus != accountRowPrefix && m.accountSnapshot.State != "signed-out" {
			for _, a := range m.accountSnapshot.Accounts {
				if a.Key != "" && (m.accountRow == "" || m.currentAccount(a.Key)) {
					m.accountRow = a.Key
				}
			}
		}
	}
	m.uiFocus = accountRowPrefix + m.accountRow
}

func (m *harborModel) moveAccountRow(delta int) bool {
	if m.accountSnapshot == nil {
		return false
	}
	rows := []string{""}
	for _, a := range m.accountSnapshot.Accounts {
		if a.Key != "" {
			rows = append(rows, a.Key)
		}
	}
	index := 0
	for i, key := range rows {
		if key == m.accountRow {
			index = max(0, min(len(rows)-1, i+delta))
			break
		}
	}
	m.accountRow = rows[index]
	m.uiFocus = accountRowPrefix + m.accountRow
	content := m.accountContent()
	page, _ := m.accountsPage()
	visible := len(content.Lines) - page.MaxScroll
	for _, hit := range content.Hits {
		if hit.ID == m.uiFocus {
			m.scroll = min(hit.Y, max(m.scroll, hit.Y-visible+1))
			break
		}
	}
	return true
}

func (m *harborModel) applyAccountRead(read appAccountRead) tea.Cmd {
	if read.generation != m.accountGeneration || m.accountProfile == nil || read.request.Profile != *m.accountProfile || m.accountSnapshot == nil {
		return nil
	}
	snapshot := *m.accountSnapshot
	switch read.request.Action {
	case "usage":
		m.accountUsagePending = false
		m.accountUsageError = ""
		if read.err != nil {
			m.accountUsageError = read.err.Error()
			return nil
		}
		refreshed := make(map[string]savedCodexAccount, len(read.result.Accounts))
		for _, a := range read.result.Accounts {
			refreshed[a.Key] = a
		}
		snapshot.Accounts = append([]savedCodexAccount(nil), snapshot.Accounts...)
		for i := range snapshot.Accounts {
			a := &snapshot.Accounts[i]
			a.Usage = nil
			if batch, ok := refreshed[a.Key]; ok && a.Key != "" {
				a.Usage = batch.Usage
				if batch.Plan != "" {
					a.Plan = batch.Plan
				}
				if batch.Workspace != "" {
					a.Workspace = batch.Workspace
				}
			}
		}
	case "reset":
		if m.screen != "accounts-resetting" || m.accountResetReview != read.request.Selected {
			return nil
		}
		m.screen, m.scroll = "accounts-detail", 0
		m.accountResetReview = ""
		m.accountResetNotice = "Reset completed."
		if read.err != nil {
			m.accountResetNotice = read.err.Error() + "."
		}
		return m.loadAccountUsage()
	}
	m.cacheAccountSnapshot(snapshot)
	return nil
}

func (m harborModel) accountsPageInput(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if retry, ok := msg.(appAccountRetry); ok {
		if m.screen != "accounts-busy" || m.accountWaiting != retry.operation {
			return m, nil, true
		}
		m.accountWaiting, m.accountWaitReason = nil, ""
		return m, runAccountOperation(retry.operation), true
	}
	if status, ok := msg.(appAccountStatus); ok {
		if status.generation != m.accountGeneration || m.accountProfile == nil || status.profile != *m.accountProfile {
			return m, nil, true
		}
		if status.err != nil {
			m.accountUsagePending = false
			m.accountUsageError = status.err.Error()
			if m.accountSnapshot == nil {
				status.result.Detail = m.accountUsageError
				m.accountSnapshot = &status.result
			}
			return m, nil, true
		}
		m.setAccountRoster(status.result)
		if m.screen == "accounts" {
			m.reconcileAccountRow()
		}
		cmd := m.loadAccountUsage()
		return m, cmd, true
	}
	if read, ok := msg.(appAccountRead); ok {
		cmd := m.applyAccountRead(read)
		return m, cmd, true
	}
	if done, ok := msg.(appAccountDone); ok {
		if m.screen != "accounts-busy" || done.profile != nil && (m.accountProfile == nil || *done.profile != *m.accountProfile) {
			if done.snapshot != nil {
				done.snapshot.login.close()
			}
			if done.operation != nil {
				done.operation.login.close()
			}
			return m, nil, true
		}
		if done.operation != nil {
			if done.err == nil && done.snapshot.login != nil {
				done.operation.login = done.snapshot.login
				done.snapshot.login = nil
				return m, tea.Batch(tea.EnableMouseAllMotion, runAccountOperation(done.operation)), true
			}
			var pending *accountTurnPending
			if errors.As(done.err, &pending) {
				m.accountWaiting, m.accountWaitReason = done.operation, pending.Error()
				return m, retryAccountOperation(done.operation), true
			}
			done.operation.login.close()
		}
		var cmd tea.Cmd
		if done.snapshot != nil && m.accountProfile != nil && done.snapshot.State != "" {
			m.setAccountRoster(*done.snapshot)
			if done.err == nil && done.operation != nil && done.operation.action == "signed-out" {
				m.removeAccountFromRoster(done.operation.selected)
			}
			cmd = m.loadAccountUsage()
		} else if m.accountUsagePending {
			cmd = m.beginAccountInspect()
		}
		m.screen = "accounts-result"
		m.scroll = 0
		m.uiFocus = "account:back"
		m.accountResult = done.result
		if done.err != nil {
			m.accountResult = done.err.Error()
			if done.result != "" {
				m.accountResult += "\n" + done.result
			}
		}
		return m, tea.Batch(tea.EnableMouseAllMotion, cmd), true
	}
	if _, ok := m.accountsPage(); !ok {
		return m, nil, false
	}
	switch v := msg.(type) {
	case tea.MouseMsg:
		if m.screen == "accounts-busy" && m.accountWaiting != nil && v.Action == tea.MouseActionPress && v.Button == tea.MouseButtonLeft {
			page, _ := m.accountsPage()
			if page.target(v.X, v.Y) == "account:cancel-wait" {
				next, cmd := m.accountActivate("account:cancel-wait")
				return next, cmd, true
			}
		}
		if m.screen == "accounts-busy" || m.screen == "accounts-resetting" {
			return m, nil, true
		}
		if v.Button == tea.MouseButtonWheelDown {
			m.scroll++
			return m, nil, true
		}
		if v.Button == tea.MouseButtonWheelUp {
			m.scroll = max(0, m.scroll-1)
			return m, nil, true
		}
		p, _ := m.accountsPage()
		id := p.target(v.X, v.Y)
		if v.Action == tea.MouseActionMotion {
			m.uiHover = strings.TrimPrefix(id, "nav:")
			return m, nil, true
		}
		if v.Action == tea.MouseActionPress && v.Button == tea.MouseButtonLeft {
			if strings.HasPrefix(id, accountRowPrefix) && m.uiFocus != id {
				m.accountRow = strings.TrimPrefix(id, accountRowPrefix)
				m.uiFocus = id
				return m, nil, true
			}
			n, c := m.accountActivate(id)
			return n, c, true
		}
		return m, nil, true
	case tea.KeyMsg:
		if m.screen == "accounts-busy" && m.accountWaiting != nil && v.String() == "esc" {
			next, cmd := m.accountActivate("account:cancel-wait")
			return next, cmd, true
		}
		if m.screen == "accounts-busy" || m.screen == "accounts-resetting" {
			return m, nil, true
		}
		key := v.String()
		switch key {
		case "down", "j":
			if m.screen != "accounts" || !m.moveAccountRow(1) {
				m.scroll++
			}
		case "up", "k":
			if m.screen != "accounts" || !m.moveAccountRow(-1) {
				m.scroll = max(0, m.scroll-1)
			}
		case "pgdown":
			m.scroll += max(1, m.height/2)
		case "pgup":
			m.scroll = max(0, m.scroll-max(1, m.height/2))
		case "enter":
			target := m.uiFocus
			if m.screen == "accounts" && !strings.HasPrefix(target, "nav:") {
				target = "account:i"
				if m.uiFocus == accountRowPrefix {
					target = accountRowPrefix
				}
			}
			if m.screen == "accounts-reset-confirm" {
				target = "account:cancel-reset"
			}
			if m.screen == "accounts-signout-confirm" {
				target = "account:cancel-signout"
			}
			n, c := m.accountActivate(target)
			return n, c, true
		case "esc", "q":
			target := "account:back"
			if m.screen == "accounts-reset-confirm" {
				target = "account:cancel-reset"
			}
			if m.screen == "accounts-signout-confirm" {
				target = "account:cancel-signout"
			}
			n, c := m.accountActivate(target)
			return n, c, true
		case "i":
			n, c := m.accountActivate("account:i")
			return n, c, true
		case "s", "o":
			n, c := m.updateAccounts(key)
			return n, c, true
		default:
			for _, a := range m.accountActions() {
				if key == a.Key {
					n, c := m.accountActivate(a.ID)
					return n, c, true
				}
			}
		}
		return m, nil, true
	}
	return m, nil, false
}
