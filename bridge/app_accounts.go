package main

import (
	"io"

	tea "github.com/charmbracelet/bubbletea"
)

type accountActionService interface {
	CodexAccountAction(string, io.Reader, io.Writer, io.Writer) (string, error)
}

type accountExec struct {
	service accountActionService
	action  string
	input   io.Reader
	output  io.Writer
	errors  io.Writer
	result  string
}

func (e *accountExec) Run() error {
	result, err := e.service.CodexAccountAction(e.action, e.input, e.output, e.errors)
	e.result = result
	return err
}
func (e *accountExec) SetStdin(r io.Reader)  { e.input = r }
func (e *accountExec) SetStdout(w io.Writer) { e.output = w }
func (e *accountExec) SetStderr(w io.Writer) { e.errors = w }

type appAccountDone struct {
	result string
	err    error
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
	if m.screen != "accounts" || (key != "s" && key != "l") {
		return m, nil
	}
	service, ok := m.service.(accountActionService)
	if !ok {
		m.accountResult = "Account management is unavailable in this environment."
		m.screen = "accounts-result"
		return m, nil
	}
	action := "switch"
	if key == "l" {
		action = "login"
	}
	operation := &accountExec{service: service, action: action}
	m.screen = "accounts-busy"
	return m, tea.Exec(operation, func(err error) tea.Msg { return appAccountDone{result: operation.result, err: err} })
}
