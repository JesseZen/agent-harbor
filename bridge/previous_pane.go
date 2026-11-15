package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

type previousPane struct {
	Session              session
	SocketIdentity       string
	ShellPID             int
	PaneID, TabID, Label string
	TerminalID           string
}

func previousPaneCommand(s session, args ...string) ([]byte, error) {
	root, err := bridgeHome()
	if err != nil {
		return nil, err
	}
	if !managedSocket(root, s.HerdrSocket) {
		return nil, errors.New("previous workspace is unavailable")
	}
	env, _, err := managedEnvironment(root)
	if err != nil {
		return nil, err
	}
	cmd := herdrCommand(env, append([]string{"--session", herdrSession}, args...)...)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	bounded := exec.CommandContext(ctx, cmd.Path, cmd.Args[1:]...)
	bounded.Env = env
	out, err := bounded.Output()
	if err != nil {
		return nil, errors.New("previous pane could not be inspected; nothing was closed")
	}
	return out, nil
}

// A restored pane is offered only when both stable pane/tab IDs and the saved
// session title match, the original CLI is gone, and the foreground is the
// shell itself. No terminal contents or process argv are exposed to the UI.
func (harborService) PreviousPane(s session) (previousPane, error) {
	result := previousPane{Session: s}
	current, err := getSession(s.ID)
	if err != nil {
		return result, err
	}
	if !sameSession(current, s) {
		return result, errors.New("session changed; refresh and review again")
	}
	if processGroupActive(s.ProcessGroup) || sessionLaunchPending(s) {
		return result, errors.New("session is running or starting; stop it before inspecting a previous pane")
	}
	if s.HerdrPane == "" || s.HerdrTab == "" {
		return result, errors.New("no previous Herdr pane recorded")
	}
	out, err := previousPaneCommand(s, "pane", "get", s.HerdrPane)
	if err != nil {
		return result, err
	}
	var info struct {
		Result struct {
			Pane struct {
				ID       string  `json:"pane_id"`
				Terminal string  `json:"terminal_id"`
				Tab      string  `json:"tab_id"`
				Agent    *string `json:"agent"`
			} `json:"pane"`
		}
	}
	if json.Unmarshal(out, &info) != nil || info.Result.Pane.ID != s.HerdrPane || info.Result.Pane.Tab != s.HerdrTab || info.Result.Pane.Agent != nil || info.Result.Pane.Terminal == "" {
		return result, errors.New("previous pane cannot be verified as an idle shell")
	}
	out, err = previousPaneCommand(s, "tab", "list")
	if err != nil {
		return result, err
	}
	var tabs struct {
		Result struct {
			Tabs []struct {
				ID    string `json:"tab_id"`
				Label string `json:"label"`
			} `json:"tabs"`
		}
	}
	if json.Unmarshal(out, &tabs) != nil {
		return result, errors.New("previous tab cannot be verified")
	}
	result.TerminalID = info.Result.Pane.Terminal
	matched := false
	for _, t := range tabs.Result.Tabs {
		if t.ID == s.HerdrTab && t.Label == s.Name {
			result.Label = t.Label
			matched = true
		}
	}
	if !matched {
		return result, errors.New("previous tab identity or title changed; inspect it in Herdr")
	}
	out, err = previousPaneCommand(s, "pane", "process-info", "--pane", s.HerdrPane)
	if err != nil {
		return result, err
	}
	var process struct {
		Result struct {
			Process struct {
				Pane       string `json:"pane_id"`
				Shell      int    `json:"shell_pid"`
				Foreground []struct {
					PID  int    `json:"pid"`
					Name string `json:"name"`
				} `json:"foreground_processes"`
			} `json:"process_info"`
		}
	}
	if json.Unmarshal(out, &process) != nil {
		return result, errors.New("previous shell process cannot be verified")
	}
	state := process.Result.Process
	if state.Pane != s.HerdrPane || state.Shell < 2 || len(state.Foreground) != 1 || state.Foreground[0].PID != state.Shell {
		return result, errors.New("previous pane has other foreground work; nothing will be closed")
	}
	switch filepath.Base(strings.TrimPrefix(state.Foreground[0].Name, "-")) {
	case "sh", "bash", "zsh", "fish", "dash":
	default:
		return result, errors.New("previous pane is not a recognized idle shell")
	}
	result.PaneID, result.TabID, result.ShellPID, result.SocketIdentity = s.HerdrPane, s.HerdrTab, state.Shell, socketIdentity(s.HerdrSocket)
	return result, nil
}
func (h harborService) ClosePreviousPane(review previousPane) error {
	current, err := h.PreviousPane(review.Session)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, review) {
		return errors.New("previous pane changed; review it again")
	}
	// Closing the pane that hosts this popup can terminate the caller. Persist
	// the already reviewed idle-shell evidence before requesting that close.
	// It establishes the previous run ended even if closing the shell fails.
	var saved session
	err = withSessions(func(all []session) ([]session, error) {
		for i := range all {
			if all[i].ID != review.Session.ID {
				continue
			}
			s := &all[i]
			if !sameSession(*s, review.Session) {
				return nil, errors.New("record changed; inspect the shell again before closing")
			}
			var ref *herdrIdentity
			if s.Conversation != nil {
				copy := s.Conversation.Reference
				ref = &copy
			}
			s.Ended = &endedEvidence{Source: "herdr.reviewed_idle_shell", At: time.Now().UTC(), StartedAt: s.LastStartedAt, Reference: ref, RunToken: s.RunToken, SocketIdentity: review.SocketIdentity, Pane: review.PaneID}
			saved = *s
			return all, nil
		}
		return nil, errors.New("record disappeared; shell was not closed")
	})
	if err != nil {
		return err
	}
	// Persisting can take time. Inspect again so work that started while the
	// evidence was being saved is not knowingly closed with the old shell.
	current, err = h.PreviousPane(saved)
	if err != nil {
		return fmt.Errorf("previous run confirmed ended, but the shell could not be rechecked before closing: %w", err)
	}
	expected := review
	expected.Session = saved
	if !reflect.DeepEqual(current, expected) {
		return errors.New("previous run confirmed ended, but the shell changed before closing; inspect it again")
	}
	// The generic session targeting helper is safe after explicitly approving the
	// currently inspected socket, rather than the socket from the original run.
	target := review.Session
	target.HerdrSocketIdentity = review.SocketIdentity
	if err := sessionHerdr(target, "pane", "close", review.PaneID); err != nil {
		return fmt.Errorf("previous run confirmed ended, but the idle shell could not be closed: %w", err)
	}
	return nil
}

// A native client can leave Harbor's own error prompt in the pane after its
// exit was recorded. Close only that exact runner, established from the public
// foreground PID plus its executable and managed session context.
func closeFinishedRunner(s session) error {
	if s.RunnerPID < 2 || s.ProcessGroup != 0 || s.LastExitAt == nil {
		return errors.New("finished runner evidence unavailable")
	}
	out, err := previousPaneCommand(s, "pane", "process-info", "--pane", s.HerdrPane)
	if err != nil {
		return err
	}
	var response struct {
		Result struct {
			Process struct {
				Pane       string `json:"pane_id"`
				Foreground []struct {
					PID int `json:"pid"`
				} `json:"foreground_processes"`
			} `json:"process_info"`
		} `json:"result"`
	}
	if json.Unmarshal(out, &response) != nil || response.Result.Process.Pane != s.HerdrPane || len(response.Result.Process.Foreground) != 1 || response.Result.Process.Foreground[0].PID != s.RunnerPID {
		return errors.New("pane has other foreground work")
	}
	facts, err := readProcessFacts(s.RunnerPID)
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return err
	}
	if facts.Executable != executable || facts.Variables["AGENT_HARBOR_BRIDGE_SESSION"] != s.ID || facts.Variables["HERDR_PANE_ID"] != s.HerdrPane || facts.Variables["HERDR_SOCKET_PATH"] != s.HerdrSocket {
		return errors.New("foreground runner ownership is unproven")
	}
	current, err := getSession(s.ID)
	if err != nil {
		return err
	}
	if !sameSession(current, s) {
		return errors.New("record changed before runner close")
	}
	return sessionHerdr(s, "pane", "close", s.HerdrPane)
}
