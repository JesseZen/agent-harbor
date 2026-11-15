package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func experimentalCodexAccounts() bool {
	return os.Getenv("AGENT_HARBOR_EXPERIMENTAL_CODEX_ACCOUNTS") == "1"
}

func codexAuthProgram() (string, error) {
	program := os.Getenv("AGENT_HARBOR_CODEX_AUTH_BIN")
	if program == "" {
		program = "codex-auth"
	}
	path, err := exec.LookPath(program)
	if err != nil {
		return "", errors.New("codex-auth is not installed; install it or set AGENT_HARBOR_CODEX_AUTH_BIN")
	}
	return path, nil
}

// The switcher remains responsible for saving/selecting credentials. Harbor
// reads only the selected account key to verify each official App Server.
func activeCodexAccount(program string, env []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, "list", "--skip-api", "--json")
	cmd.Env = env
	data, err := cmd.Output()
	if err != nil {
		return "", errors.New("could not read the selected codex-auth account")
	}
	var list struct {
		Active string `json:"active_account_key"`
	}
	if json.Unmarshal(data, &list) != nil || list.Active == "" {
		return "", errors.New("codex-auth did not report a selected account")
	}
	return list.Active, nil
}

type accountQuota struct {
	AccountID  string `json:"accountId"`
	RateLimits struct {
		Primary   *accountWindow `json:"primary"`
		Secondary *accountWindow `json:"secondary"`
	} `json:"rateLimits"`
}
type accountWindow struct {
	UsedPercent float64 `json:"usedPercent"`
	Minutes     int     `json:"windowDurationMins"`
	ResetsAt    int64   `json:"resetsAt"`
}

func quotaSummary(q accountQuota, selected string) string {
	if q.AccountID != "" && !strings.HasSuffix(selected, "::"+q.AccountID) {
		return "Quota unavailable: the returned account does not match the selection."
	}
	var windows []string
	for _, window := range []*accountWindow{q.RateLimits.Primary, q.RateLimits.Secondary} {
		if window == nil {
			continue
		}
		duration := fmt.Sprintf("%d min", window.Minutes)
		switch window.Minutes {
		case 300:
			duration = "5 hours"
		case 10080:
			duration = "weekly"
		case 0:
			duration = "unknown period"
		}
		reset := "unknown"
		if window.ResetsAt > 0 {
			reset = time.Unix(window.ResetsAt, 0).Local().Format("Jan 02 15:04")
		}
		windows = append(windows, fmt.Sprintf("%s: %.0f%% used, resets %s", duration, window.UsedPercent, reset))
	}
	if len(windows) == 0 {
		return "Official quota is currently unavailable."
	}
	if q.AccountID == "" {
		windows = append(windows, "Quota response did not identify its account.")
	}
	return "Official quota: " + strings.Join(windows, "; ")
}

func refreshCodexAccount(s session, selected string) (string, error) {
	if !verifyCodexServer(s) {
		return "", errors.New("the running Codex server could not be verified")
	}
	return refreshCodexAccountSocket(s.CodexServerSocket, s.WorkingDirectory, selected)
}

func refreshCodexAccountSocket(socket, directory, selected string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	rpc, err := connectCodexRPC(ctx, socket)
	if err != nil {
		return "", err
	}
	defer rpc.conn.CloseNow()
	var started struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := rpc.call(ctx, "thread/start", map[string]any{"cwd": directory, "ephemeral": true, "config": map[string]any{"features": map[string]bool{"apps": false}}}, &started); err != nil || started.Thread.ID == "" {
		return "", errors.New("could not prepare the experimental account refresh")
	}
	// Codex 0.169 reloads its own auth during this request. This is not an
	// upstream guarantee; verify the resulting identity before reporting it.
	if err := rpc.call(ctx, "app/list", map[string]any{"forceRefetch": true, "threadId": started.Thread.ID}, nil); err != nil {
		return "", err
	}
	var state struct {
		Sessions []struct {
			UserID      string `json:"userId"`
			WorkspaceID string `json:"selectedWorkspaceAccountId"`
			Active      bool   `json:"isActive"`
		} `json:"sessions"`
	}
	if err := rpc.call(ctx, "accountSession/list", map[string]bool{"refreshWorkspaceMetadata": false}, &state); err != nil {
		return "", err
	}
	matched := false
	for _, account := range state.Sessions {
		if account.Active && account.UserID+"::"+account.WorkspaceID == selected {
			matched = true
			break
		}
	}
	if !matched {
		return "", errors.New("Codex did not confirm the selected account")
	}
	var quota accountQuota
	quotaCtx, quotaCancel := context.WithTimeout(ctx, 5*time.Second)
	defer quotaCancel()
	if err := rpc.call(quotaCtx, "account/rateLimits/read", map[string]bool{"excludeResetCreditDetails": true}, &quota); err != nil {
		return "Official quota is currently unavailable.", nil
	}
	return quotaSummary(quota, selected), nil
}

func codexServerHasActiveTurn(s session) bool {
	if !verifyCodexServer(s) {
		return false // The UI asks the user to check sessions Harbor cannot inspect.
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rpc, err := connectCodexRPC(ctx, s.CodexServerSocket)
	if err != nil {
		return false
	}
	defer rpc.conn.CloseNow()
	var loaded struct {
		Data []string `json:"data"`
	}
	if rpc.call(ctx, "thread/loaded/list", map[string]any{}, &loaded) != nil {
		return false
	}
	for _, id := range loaded.Data {
		var state struct {
			Thread struct {
				Status struct {
					Type string `json:"type"`
				} `json:"status"`
			} `json:"thread"`
		}
		if rpc.call(ctx, "thread/read", map[string]any{"threadId": id, "includeTurns": false}, &state) == nil && state.Thread.Status.Type == "active" {
			return true
		}
	}
	return false
}

func (harborService) CodexAccountAction(action string, stdin io.Reader, stdout, stderr io.Writer) (string, error) {
	program, err := codexAuthProgram()
	if err != nil {
		return "", err
	}
	env, err := userEnvironment()
	if err != nil {
		return "", err
	}
	var args []string
	switch action {
	case "login":
		args = []string{"login"}
	case "switch":
		args = []string{"switch", "--skip-api", "--no-restart-daemon"}
	default:
		return "", errors.New("unknown Codex account action")
	}
	home := environmentValue(env, "CODEX_HOME")
	if home == "" {
		home = filepath.Join(environmentValue(env, "HOME"), ".codex")
	}
	home, err = filepath.Abs(home)
	if err != nil {
		return "", errors.New("could not determine the shared Codex home")
	}
	var records []session
	if err := withSessions(func(all []session) ([]session, error) { records = append(records, all...); return nil, nil }); err != nil {
		return "", errors.New("running Harbor sessions could not be checked")
	}
	for _, s := range records {
		if s.CLI == "codex" && s.CodexHome == home && processGroupActive(s.ProcessGroup) && codexServerHasActiveTurn(s) {
			return "", fmt.Errorf("%s is still responding; wait until its turn finishes before switching", s.Name)
		}
	}
	cmd := exec.Command(program, args...)
	cmd.Env, cmd.Stdin, cmd.Stdout, cmd.Stderr = env, stdin, stdout, stderr
	if err := cmd.Run(); err != nil {
		return "", errors.New("codex-auth did not complete; running session identities are unconfirmed")
	}
	selected, err := activeCodexAccount(program, env)
	if err != nil {
		return "", err
	}
	records = nil
	if err := withSessions(func(all []session) ([]session, error) { records = append(records, all...); return nil, nil }); err != nil {
		return "", errors.New("account selected, but running Harbor sessions could not be checked")
	}
	verified, uncertain, quota := 0, []string{}, ""
	var outcomes []string
	for _, s := range records {
		if s.CLI != "codex" || s.CodexHome != home || !processGroupActive(s.ProcessGroup) {
			continue
		}
		q, refreshErr := refreshCodexAccount(s, selected)
		if refreshErr != nil {
			uncertain = append(uncertain, s.Name)
			status := "Failed"
			if !verifyCodexServer(s) {
				status = "Unknown"
			}
			outcomes = append(outcomes, fmt.Sprintf("%s · %s: identity unconfirmed. %s. Check this session before continuing.", s.Name, status, refreshErr.Error()))
			continue
		}
		verified++
		outcomes = append(outcomes, fmt.Sprintf("%s · Verified: selected identity confirmed. %s", s.Name, q))
		if quota == "" {
			quota = q
		}
	}
	lines := []string{fmt.Sprintf("Selected account confirmed in %d running Harbor session(s).", verified)}
	lines = append(lines, outcomes...)
	if len(uncertain) > 0 {
		lines = append(lines, "Identity unconfirmed: "+strings.Join(uncertain, ", ")+". Check these sessions before continuing.")
	}
	if quota != "" {
		lines = append(lines, quota)
	}
	lines = append(lines, "Other Codex processes using this home may be affected; Harbor cannot verify them.", "Experimental refresh depends on Codex behavior that may change. Plugin state is unverified.")
	return strings.Join(lines, "\n"), nil
}
