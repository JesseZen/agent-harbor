package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const accountLoginKey = "n"

type accountActionService interface {
	CodexAccountAction(string, io.Reader, io.Writer, io.Writer) (string, error)
}

type accountExec struct {
	service  profileAccountService
	profile  profileBinding
	snapshot profileAccountResult
	action   string
	selected string
	input    io.Reader
	output   io.Writer
	errors   io.Writer
	result   string
	login    *stagedCodexLogin
}

func (e *accountExec) Run() error {
	result, err := e.service.ProfileCodexAccounts(context.Background(), codexAccountRequest{Profile: e.profile, Action: e.action, Selected: e.selected, login: e.login}, e.input, e.output, e.errors)
	e.snapshot = result
	e.result = accountResultText(result)
	return err
}
func (e *accountExec) SetStdin(r io.Reader)  { e.input = r }
func (e *accountExec) SetStdout(w io.Writer) { e.output = w }
func (e *accountExec) SetStderr(w io.Writer) { e.errors = w }

type appAccountDone struct {
	result    string
	profile   *profileBinding
	snapshot  *profileAccountResult
	err       error
	operation *accountExec
}

type appAccountRetry struct{ operation *accountExec }

func runAccountOperation(operation *accountExec) tea.Cmd {
	done := func(err error) tea.Msg {
		return appAccountDone{result: operation.result, err: err, profile: &operation.profile, snapshot: &operation.snapshot, operation: operation}
	}
	if operation.action == "login" && operation.login == nil {
		return tea.Exec(operation, done)
	}
	return func() tea.Msg { return done(operation.Run()) }
}

func retryAccountOperation(operation *accountExec) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return appAccountRetry{operation} })
}

func (m harborModel) updateAccounts(key string) (tea.Model, tea.Cmd) {
	if key == "esc" || key == "q" {
		if m.screen == "accounts-result" || m.screen == "accounts-detail" {
			m.screen = "accounts"
		} else {
			m.screen = "sessions"
		}
		return m, nil
	}
	if m.screen == "accounts" && key == "," {
		settings := newSettingsModel()
		settings.width, settings.height = m.width, m.height
		m.settings = &settings
		return m, nil
	}
	if m.screen == "accounts" && key == "p" {
		m.profileReturn, m.profileReturnScroll = "accounts", m.scroll
		m.scroll = 0
		m.screen = "profile-list"
		m.notice = ""
		return m, m.loadProfiles()
	}

	if m.screen != "accounts" || (key != "s" && key != accountLoginKey && key != "o" && key != "r") {
		return m, nil
	}
	if key == "r" {
		cmd := m.beginAccountInspect()
		return m, cmd
	}
	m.accountListScroll = m.scroll
	switch key {
	case accountLoginKey:
		return m.runAccountAction("login", "")
	case "s":
		if account, ok := m.selectedAccount(); ok {
			if m.currentAccount(account.Key) {
				return m, nil
			}
			return m.runAccountAction("switch", account.Key)
		}
		if m.uiFocus != accountRowPrefix {
			return m, nil
		}
	}
	if !m.canSignOutAccount() {
		return m, nil
	}
	m.screen, m.scroll = "accounts-signout-confirm", 0
	return m, nil
}

func (m harborModel) runAccountAction(action, selected string) (tea.Model, tea.Cmd) {
	service, ok := m.service.(profileAccountService)
	if !ok || m.accountProfile == nil {
		m.accountResult = "Select a Profile with p before managing accounts."
		if !ok {
			m.accountResult = "Account management is unavailable in this environment."
		}
		m.screen, m.scroll = "accounts-result", 0
		return m, nil
	}
	m.accountGeneration++
	if action == "signed-out" {
		selected = m.accountRow
	}
	captured := *m.accountProfile
	operation := &accountExec{service: service, action: action, selected: selected, profile: captured}
	m.screen, m.scroll = "accounts-busy", 0
	return m, runAccountOperation(operation)
}

func (m *harborModel) beginAccountInspect() tea.Cmd {
	m.accountGeneration++
	m.accountSnapshot = nil
	m.accountUsagePending = false
	m.accountUsageError = ""
	if m.accountProfile == nil {
		return nil
	}
	if cached, ok := m.accountCache[*m.accountProfile]; ok {
		m.accountSnapshot = &cached
		m.reconcileAccountRow()
	}
	m.accountUsagePending = true
	return m.inspectAccounts(*m.accountProfile)
}

func (m *harborModel) cacheAccountSnapshot(snapshot profileAccountResult) {
	if m.accountCache == nil {
		m.accountCache = make(map[profileBinding]profileAccountResult)
	}
	m.accountCache[*m.accountProfile] = snapshot
	m.accountSnapshot = &snapshot
}

// Roster reads also carry local or cached usage. Keep already displayed API
// values for surviving accounts until the next API refresh completes.
func (m *harborModel) setAccountRoster(snapshot profileAccountResult) {
	cached := m.accountCache[*m.accountProfile]
	usage := make(map[string]*savedAccountUsage, len(cached.Accounts))
	for _, account := range cached.Accounts {
		if account.Key != "" {
			usage[account.Key] = account.Usage
		}
	}
	snapshot.Accounts = append([]savedCodexAccount(nil), snapshot.Accounts...)
	for i := range snapshot.Accounts {
		account := &snapshot.Accounts[i]
		previous := usage[account.Key]
		if account.Usage == nil || (previous.current() && !account.Usage.current()) {
			account.Usage = previous
		}
	}
	m.cacheAccountSnapshot(snapshot)
}

func (m *harborModel) removeAccountFromRoster(key string) {
	if key == "" || m.accountProfile == nil {
		return
	}
	snapshot, ok := m.accountCache[*m.accountProfile]
	if !ok {
		return
	}
	filtered := snapshot.Accounts[:0]
	for _, account := range snapshot.Accounts {
		if account.Key != key {
			filtered = append(filtered, account)
		}
	}
	snapshot.Accounts = filtered
	m.accountCache[*m.accountProfile] = snapshot
	m.accountSnapshot = &snapshot
	if m.accountRow == key {
		m.accountRow = ""
	}
}

func (m harborModel) inspectAccounts(binding profileBinding) tea.Cmd {
	generation := m.accountGeneration
	return func() tea.Msg {
		service, ok := m.service.(profileAccountService)
		if !ok {
			return appAccountStatus{generation: generation, profile: binding, err: fmt.Errorf("account management unavailable")}
		}
		result, err := service.ProfileCodexAccounts(context.Background(), codexAccountRequest{Profile: binding, Action: "inspect"}, nil, nil, nil)
		return appAccountStatus{generation: generation, profile: binding, result: result, err: err}
	}
}

type appAccountStatus struct {
	generation uint64
	profile    profileBinding
	result     profileAccountResult
	err        error
}

type appAccountRead struct {
	generation uint64
	request    codexAccountRequest
	result     profileAccountResult
	err        error
}

func (m harborModel) readAccountData(req codexAccountRequest) tea.Cmd {
	service, ok := m.service.(profileAccountService)
	if !ok {
		return nil
	}
	generation := m.accountGeneration
	return func() tea.Msg {
		result, err := service.ProfileCodexAccounts(context.Background(), req, nil, nil, nil)
		return appAccountRead{generation: generation, request: req, result: result, err: err}
	}
}

func (m *harborModel) loadAccountUsage() tea.Cmd {
	m.accountUsageError = ""
	if m.accountProfile == nil || m.accountSnapshot == nil || len(m.accountSnapshot.Accounts) == 0 {
		m.accountUsagePending = false
		return nil
	}
	cmd := m.readAccountData(codexAccountRequest{Profile: *m.accountProfile, Action: "usage"})
	m.accountUsagePending = cmd != nil
	return cmd
}

func (m harborModel) currentAccount(key string) bool {
	return key != "" && m.accountSnapshot != nil && m.accountSnapshot.State == "selected" && key == m.accountSnapshot.Selected
}

func (m harborModel) canSignOutAccount() bool {
	return m.accountProfile != nil && m.accountSnapshot != nil && m.accountSnapshot.State != "signed-out" && m.accountSnapshot.State != "unavailable"
}

func (m harborModel) selectedAccount() (savedCodexAccount, bool) {
	if m.accountSnapshot != nil {
		for _, a := range m.accountSnapshot.Accounts {
			if a.Key != "" && a.Key == m.accountRow {
				return a, true
			}
		}
	}
	return savedCodexAccount{}, false
}

func (m harborModel) canResetAccount() bool {
	a, ok := m.selectedAccount()
	return ok && m.accountProfile != nil && !m.accountUsagePending && m.accountUsageError == "" && a.Usage.hasResetCard()
}

func (m harborModel) reviewAccountReset() (tea.Model, tea.Cmd) {
	if !m.canResetAccount() {
		return m, nil
	}
	m.accountResetReview = m.accountRow
	m.screen, m.scroll = "accounts-reset-confirm", 0
	return m, nil
}

func (m harborModel) submitAccountReset() (tea.Model, tea.Cmd) {
	if m.accountResetReview != m.accountRow || !m.canResetAccount() {
		return m, nil
	}
	cmd := m.readAccountData(codexAccountRequest{Profile: *m.accountProfile, Action: "reset", Selected: m.accountResetReview})
	if cmd != nil {
		m.screen, m.scroll = "accounts-resetting", 0
	}
	return m, cmd
}

func accountResultText(result profileAccountResult) string {
	var lines []string
	if result.ProfileName != "" {
		lines = append(lines, "Profile: "+result.ProfileName)
	}
	switch result.State {
	case "selected":
		for _, account := range result.Accounts {
			if account.Key == result.Selected {
				lines = append(lines, "Saved login: "+accountName(account))
				break
			}
		}
	case "signed-out":
		lines = append(lines, "Saved login: Not signed in")
	case "":
	default:
		lines = append(lines, "Login: "+result.State)
	}
	for _, s := range result.Sessions {
		lines = append(lines, fmt.Sprintf("%s · %s: %s", s.Name, s.State, s.Detail))
	}
	if result.Detail != "" {
		lines = append(lines, result.Detail)
	}
	return strings.Join(lines, "\n")
}
