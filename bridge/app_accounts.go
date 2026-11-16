package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type accountActionService interface {
	CodexAccountAction(string, io.Reader, io.Writer, io.Writer) (string, error)
}

type accountExec struct {
	service  profileAccountService
	profile  profileBinding
	snapshot profileAccountResult
	action   string
	input    io.Reader
	output   io.Writer
	errors   io.Writer
	result   string
}

func (e *accountExec) Run() error {
	result, err := e.service.ProfileCodexAccounts(context.Background(), codexAccountRequest{Profile: e.profile, Action: e.action}, e.input, e.output, e.errors)
	e.snapshot = result
	e.result = accountResultText(result)
	return err
}
func (e *accountExec) SetStdin(r io.Reader)  { e.input = r }
func (e *accountExec) SetStdout(w io.Writer) { e.output = w }
func (e *accountExec) SetStderr(w io.Writer) { e.errors = w }

type appAccountDone struct {
	result   string
	profile  *profileBinding
	snapshot *profileAccountResult
	err      error
}

func (m harborModel) updateAccounts(key string) (tea.Model, tea.Cmd) {
	if key == "esc" || key == "q" {
		if m.screen == "accounts-result" {
			m.screen = "accounts"
		} else {
			m.screen = "sessions"
		}
		return m, nil
	}
	if m.screen == "accounts" && key == "p" {
		m.screen = "profile-list"
		m.notice = ""
		return m, m.loadProfiles()
	}

	if m.screen != "accounts" || (key != "s" && key != "l" && key != "o" && key != "r") {
		return m, nil
	}
	service, ok := m.service.(profileAccountService)
	if !ok {
		m.accountResult = "Account management is unavailable in this environment."
		m.screen = "accounts-result"
		return m, nil
	}
	if m.accountProfile == nil {
		m.accountResult = "Select a Profile with p before managing accounts."
		m.screen = "accounts-result"
		return m, nil
	}
	if key == "r" {
		m.accountGeneration++
		return m, m.inspectAccounts(*m.accountProfile)
	}
	action := "switch"
	if key == "l" {
		action = "login"
	}
	if key == "o" {
		action = "signed-out"
	}
	m.accountGeneration++
	captured := *m.accountProfile
	operation := &accountExec{service: service, action: action, profile: captured}
	m.screen = "accounts-busy"
	return m, tea.Exec(operation, func(err error) tea.Msg {
		return appAccountDone{result: operation.result, err: err, profile: &captured, snapshot: &operation.snapshot}
	})
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

func accountResultText(result profileAccountResult) string {
	lines := []string{"Profile: " + result.ProfileName, "Codex home: " + result.Home, "Login: " + result.State}
	for _, s := range result.Sessions {
		lines = append(lines, fmt.Sprintf("%s · %s: %s", s.Name, s.State, s.Detail))
	}
	if result.Detail != "" {
		lines = append(lines, result.Detail)
	}
	return strings.Join(lines, "\n")
}
