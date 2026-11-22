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
	"slices"
	"strings"
	"time"
)

type codexAccountRequest struct {
	Profile  profileBinding
	Action   string // inspect, usage, reset, login, switch, signed-out
	Selected string
	login    *stagedCodexLogin
}
type savedCodexAccount struct {
	Key         string             `json:"account_key"`
	Email       string             `json:"email"`
	Plan        string             `json:"plan"`
	Alias       string             `json:"alias"`
	Workspace   string             `json:"account_name"`
	Active      bool               `json:"active"`
	Expires     *int64             `json:"access_token_expires_at"`
	PlanExpires *int64             `json:"plan_expires_at"`
	Usage       *savedAccountUsage `json:"usage"`
}

type savedAccountUsage struct {
	Source       string             `json:"source"`
	Primary      *savedUsageWindow  `json:"primary"`
	Secondary    *savedUsageWindow  `json:"secondary"`
	ResetCount   *int               `json:"reset_credits"`
	ResetCredits []savedResetCredit `json:"reset_credit"`
	Refresh      struct {
		Status     string `json:"status"`
		HTTPStatus int    `json:"http_status"`
	} `json:"refresh"`
}

type savedResetCredit struct {
	Status    string `json:"status"`
	ExpiresAt string `json:"expires_at"`
}

func (c savedResetCredit) expiration() time.Time {
	at, _ := time.Parse(time.RFC3339, c.ExpiresAt)
	return at
}

type savedUsageWindow struct {
	UsedPercent *float64 `json:"used_percent"`
	Minutes     int      `json:"window_minutes"`
	ResetsAt    int64    `json:"resets_at"`
}

// A failed usage request can include old quota and cards.
func (u *savedAccountUsage) current() bool {
	return u != nil && u.Source == "api" && u.Refresh.Status == "ok"
}

func (u *savedAccountUsage) hasResetCard() bool {
	if !u.current() || u.ResetCount == nil || *u.ResetCount <= 0 {
		return false
	}
	now := time.Now()
	for _, card := range u.ResetCredits {
		expiry := card.expiration()
		if card.Status == "available" && (expiry.IsZero() || expiry.After(now)) {
			return true
		}
	}
	return false
}

type profileAccountResult struct {
	Profile                            profileBinding
	ProfileName, Home, State, Selected string
	Accounts                           []savedCodexAccount
	Sessions                           []accountSessionOutcome
	Detail                             string
	login                              *stagedCodexLogin
}
type accountSessionOutcome struct{ ID, Name, State, Detail string }

type stagedCodexLogin struct {
	profile profileBinding
	home    string
	key     string
}

func (login *stagedCodexLogin) close() {
	if login != nil {
		_ = os.RemoveAll(login.home)
	}
}

type accountTurnPending struct{ name string }

func (pending *accountTurnPending) Error() string {
	return "Waiting for " + pending.name + " to finish…"
}

type profileAccountService interface {
	ProfileCodexAccounts(context.Context, codexAccountRequest, io.Reader, io.Writer, io.Writer) (profileAccountResult, error)
}

func (h harborService) ProfileCodexAccounts(ctx context.Context, req codexAccountRequest, in io.Reader, out, diagnostic io.Writer) (profileAccountResult, error) {
	return profileCodexAccounts(ctx, h, req, in, out, diagnostic)
}

// Uses the same service lock as Profile launch. The callback never resolves an
// ambient home or takes the lock recursively. Production retains #19's gate.
func profileCodexAccounts(ctx context.Context, service applicationService, req codexAccountRequest, in io.Reader, out, diagnostic io.Writer) (profileAccountResult, error) {
	result := profileAccountResult{Profile: req.Profile}
	operation := profileAccountMutation
	switch req.Action {
	case "inspect", "usage":
		operation = profileInspect
	case "reset", "login", "switch", "signed-out":
	default:
		return result, errors.New("unknown account action")
	}
	if req.Profile.Client != "codex" {
		return result, ErrProfileUnsupported
	}
	if req.Action == "login" && req.login == nil {
		var err error
		result.login, err = authorizeCodexLogin(ctx, service, req.Profile, in, out, diagnostic)
		return result, err
	}
	err := service.WithProfileClient(ctx, profileClientRequest{Binding: req.Profile}, operation, func(scope profileClientScope) error {
		env := scope.Environment
		result.ProfileName, result.Home = env.ProfileName, env.Home
		if env.Kind == profileExisting {
			result.Detail = "Existing environment: external Codex clients may share this login; Harbor cannot inspect or control them."
		}
		if env.Binding != req.Profile || environmentValue(env.Environment, "CODEX_HOME") != env.Home {
			return ErrProfileIdentity
		}
		for _, s := range scope.Sessions {
			if s.Profile == nil || *s.Profile != req.Profile || s.CLI != "codex" {
				return ErrProfileIdentity
			}
		}
		// Even status must not accidentally consult a global keychain before the
		// client runtime policy is enabled by the Profile implementation.
		if !env.Executable {
			return ErrProfileNotReady
		}
		tool, err := codexAuthProgram()
		if err != nil {
			if req.Action == "inspect" {
				result.State = "unavailable"
			}
			return err
		}
		switch req.Action {
		case "inspect":
			return readProfileAccounts(ctx, tool, env, &result)
		case "usage":
			list, err := listProfileAccounts(ctx, tool, env, "--api")
			result.Accounts = list.Accounts
			return err
		case "reset":
			return resetProfileAccount(ctx, tool, env, req.Selected)
		}
		// Sync the current credential with upstream's saved collection before logout.
		// This is an explicit mutation action, not a Harbor credential backup.
		if req.Action == "signed-out" {
			if err := readProfileAccounts(ctx, tool, env, &result); err != nil {
				return err
			}
			if result.State != "selected" && result.State != "expired" && result.State != "signed-out" {
				return errors.New("current login is unconfirmed; repair it before signing out")
			}
		}
		if req.Action == "switch" {
			list, err := listProfileAccounts(ctx, tool, env, "--skip-api")
			if err != nil {
				return err
			}
			if !list.hasKey(req.Selected) {
				return errors.New("the saved login is no longer in this Profile")
			}
		}
		selected := req.Selected
		if req.Action == "login" {
			if req.login.profile != req.Profile || req.login.key == "" {
				return ErrProfileIdentity
			}
			selected = req.login.key
		}
		var cmd *exec.Cmd
		switch req.Action {
		case "login", "switch":
			cmd = exec.CommandContext(ctx, tool, "switch", selected, "--json", "--no-restart-daemon")
		case "signed-out":
			codex, err := profileAccountCodex(env)
			if err != nil {
				return err
			}
			args := append(append([]string{}, env.Arguments...), "logout")
			cmd = exec.CommandContext(ctx, codex, args...)
		}
		cmd.Env, cmd.Dir = env.Environment, env.Home
		if req.Action == "login" {
			if _, err := accountIdleSessions(ctx, scope.Sessions); err != nil {
				return err
			}
			if _, err := listProfileAccounts(ctx, tool, env, "--skip-api"); err != nil {
				return err
			}
			importCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			importLogin := exec.CommandContext(importCtx, tool, "import", filepath.Join(req.login.home, "auth.json"))
			importLogin.Env, importLogin.Dir = env.Environment, env.Home
			importErr := importLogin.Run()
			cancel()
			if importErr != nil {
				return errors.New("codex-auth could not save the authorized login")
			}
			list, err := listProfileAccounts(ctx, tool, env, "--skip-api")
			if err != nil || !list.hasKey(selected) {
				return errors.New("codex-auth did not confirm the newly saved login")
			}
		}
		live, err := accountIdleSessions(ctx, scope.Sessions)
		if err != nil {
			return err
		}
		actionErr := cmd.Run()
		if actionErr == nil && req.Action == "signed-out" && result.Selected != "" {
			account := savedCodexAccount{Key: result.Selected}
			for _, candidate := range result.Accounts {
				if candidate.Key == result.Selected {
					account = candidate
					break
				}
			}
			if err := removeCodexSavedAccount(ctx, tool, env, account); err != nil {
				return err
			}
		}
		readErr := readProfileAccounts(context.WithoutCancel(ctx), tool, env, &result)
		if actionErr != nil {
			return errors.New("account action cancelled or failed; the displayed state was reread, running identities remain unconfirmed")
		}
		if readErr != nil {
			return readErr
		}
		if req.Action == "signed-out" && result.State != "signed-out" {
			return errors.New("Codex did not confirm sign-out; repair this Profile's login")
		}
		if req.Action != "signed-out" && (result.State != "selected" || result.Selected != selected) {
			return errors.New("Codex did not confirm the selected login")
		}
		for _, s := range live {
			result.Sessions = append(result.Sessions, applyRunningCodexLogin(ctx, s, result))
		}
		return nil
	})
	return result, err
}

func removeCodexSavedAccount(ctx context.Context, tool string, env profileClientEnvironment, account savedCodexAccount) error {
	selector := account.Email
	if selector == "" {
		selector = account.Key
	}
	if selector == "" {
		return errors.New("codex-auth could not identify the signed-out account")
	}
	removeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(removeCtx, tool, "remove", selector, "--json")
	cmd.Env, cmd.Dir = env.Environment, env.Home
	if err := cmd.Run(); err != nil {
		return errors.New("codex-auth could not remove the signed-out account")
	}
	return nil
}

func authorizeCodexLogin(ctx context.Context, service applicationService, binding profileBinding, in io.Reader, out, diagnostic io.Writer) (*stagedCodexLogin, error) {
	var env profileClientEnvironment
	err := service.WithProfileClient(ctx, profileClientRequest{Binding: binding}, profileAccountMutation, func(scope profileClientScope) error {
		env = scope.Environment
		if env.Binding != binding || environmentValue(env.Environment, "CODEX_HOME") != env.Home {
			return ErrProfileIdentity
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	tool, err := codexAuthProgram()
	if err != nil {
		return nil, err
	}
	home, err := os.MkdirTemp(env.Runtime, "account-login-")
	if err != nil {
		return nil, errors.New("could not prepare a private Codex login directory")
	}
	login := &stagedCodexLogin{profile: binding, home: home}
	defer func() {
		if login.key == "" {
			login.close()
		}
	}()
	env.Home = home
	if len(env.Arguments) == 0 {
		env.Arguments = []string{"-c", `cli_auth_credentials_store="file"`}
	}
	env.Environment = replaceEnvironment(env.Environment, map[string]*string{"CODEX_HOME": &home})
	childEnv, cleanup, err := accountLoginEnvironment(env)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	cmd := exec.CommandContext(ctx, tool, "login")
	cmd.Env, cmd.Dir, cmd.Stdin, cmd.Stdout, cmd.Stderr = childEnv, home, in, out, diagnostic
	if err := cmd.Run(); err != nil {
		return nil, errors.New("Codex login cancelled or failed; the Profile login was not changed")
	}
	result := profileAccountResult{}
	if err := readProfileAccounts(ctx, tool, env, &result); err != nil || result.State != "selected" {
		return nil, errors.New("Codex did not confirm the new ChatGPT login; the Profile login was not changed")
	}
	login.key = result.Selected
	return login, nil
}

func accountIdleSessions(ctx context.Context, sessions []session) ([]session, error) {
	var views []sessionView
	for _, s := range sessions {
		if s.Mode == "magpie" && !joinedCodex(s) {
			continue
		}
		if sessionLaunchPending(s) {
			return nil, &accountTurnPending{name: s.Name + " starting"}
		}
		if processGroupActive(s.ProcessGroup) {
			views = append(views, sessionView{session: s})
		}
	}
	if len(views) == 0 {
		return nil, ctx.Err()
	}
	socket, identity, agents, err := herdrAgentSnapshot(ctx)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("%s: Herdr activity is unavailable; the Profile login was not changed", views[0].Name)
	}
	observed := make(map[int]herdrAgent)
	for _, agent := range agents {
		if index := verifiedForegroundRun(ctx, socket, identity, agent, views); index >= 0 {
			observed[index] = agent
		}
	}
	var live []session
	var pending *accountTurnPending
	for index, view := range views {
		if !processGroupActive(view.ProcessGroup) {
			continue
		}
		agent, ok := observed[index]
		if !ok || !agent.LaunchPending && agent.Activity != "idle" && agent.Activity != "done" && agent.Activity != "working" && agent.Activity != "blocked" {
			return nil, fmt.Errorf("%s: Herdr could not verify window activity; the Profile login was not changed", view.Name)
		}
		if (agent.LaunchPending || agent.Activity == "working" || agent.Activity == "blocked") && pending == nil {
			pending = &accountTurnPending{name: view.Name}
		}
		live = append(live, view.session)
	}
	if pending != nil {
		return nil, pending
	}
	return live, ctx.Err()
}

func applyRunningCodexLogin(ctx context.Context, s session, result profileAccountResult) accountSessionOutcome {
	outcome := accountSessionOutcome{ID: s.ID, Name: s.Name, State: "not confirmed"}
	switch {
	case !processGroupActive(s.ProcessGroup):
		outcome.State, outcome.Detail = "closed", "The window is no longer running."
	case result.State == "signed-out":
		outcome.Detail = "Live sign-out was not verified."
	case !verifyCodexServer(s):
		outcome.Detail = "No reachable owned App Server. Reopen the same Conversation to use the saved login."
	default:
		check := func() error {
			live, err := accountIdleSessions(ctx, []session{s})
			if err != nil || len(live) != 1 || !verifyCodexServer(s) {
				return errors.New("window is no longer verified idle")
			}
			return nil
		}
		if err := check(); err != nil {
			outcome.Detail = err.Error()
		} else if _, err := refreshCodexAccountWithCheck(ctx, s.CodexServerSocket, s.WorkingDirectory, result.Selected, check); err != nil {
			outcome.Detail = err.Error()
		} else {
			outcome.State, outcome.Detail = "switched", "Selected login verified in this window."
		}
	}
	return outcome
}

func resetProfileAccount(ctx context.Context, tool string, env profileClientEnvironment, selected string) error {
	list, err := listProfileAccounts(ctx, tool, env, "--skip-api")
	if err != nil {
		return err
	}
	// codex-auth tries the exact registry key before its other selector forms.
	if !list.hasKey(selected) {
		return errors.New("the saved login is no longer in this Profile")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, tool, "reset", selected)
	cmd.Env, cmd.Dir = env.Environment, env.Home
	if err := cmd.Run(); err != nil {
		return errors.New("codex-auth did not confirm the reset")
	}
	return nil
}

type savedAccountList struct {
	Version  int                 `json:"schema_version"`
	Active   string              `json:"active_account_key"`
	Accounts []savedCodexAccount `json:"accounts"`
}

func (list savedAccountList) hasKey(key string) bool {
	return key != "" && slices.ContainsFunc(list.Accounts, func(account savedCodexAccount) bool { return account.Key == key })
}

func listProfileAccounts(ctx context.Context, tool string, env profileClientEnvironment, flag string) (savedAccountList, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, tool, "list", flag, "--json")
	cmd.Env = env.Environment
	cmd.Dir = env.Home
	data, err := cmd.Output()
	if err != nil {
		return savedAccountList{}, errors.New("codex-auth could not read saved account data")
	}
	var list savedAccountList
	if json.Unmarshal(data, &list) != nil || list.Version != 1 {
		return savedAccountList{}, errors.New("incompatible codex-auth account response; reinstall the pinned tool")
	}
	return list, nil
}

func readProfileAccounts(ctx context.Context, tool string, env profileClientEnvironment, result *profileAccountResult) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	list, err := listProfileAccounts(ctx, tool, env, "--skip-api")
	if err != nil {
		result.State = "unavailable"
		return err
	}
	result.Accounts = list.Accounts
	result.Selected = ""
	result.State = "unconfirmed"
	// codex-auth retains a stale registry active key after official logout.
	// A missing file alone is insufficient for Existing/keychain environments.
	codex, err := profileAccountCodex(env)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, codex, append(append([]string{}, env.Arguments...), "login", "status")...)
	cmd.Env = env.Environment
	cmd.Dir = env.Home
	status, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(status), "Not logged in") {
			result.Accounts = removeSavedAccount(list.Accounts, list.Active)
			result.State = "signed-out"
			return nil
		}
		return errors.New("official Codex login status is unavailable; no signed-out state assumed")
	}
	if !strings.Contains(string(status), "Logged in using ChatGPT") {
		result.Detail = "Codex uses another authentication method; explicitly review it before choosing Magpie."
		return nil
	}
	if info, err := os.Lstat(filepath.Join(env.Home, "auth.json")); err != nil || !info.Mode().IsRegular() {
		return errors.New("Codex login is not confirmed in this Profile's file backend")
	}
	for _, a := range list.Accounts {
		if a.Active && a.Key == list.Active && a.Key != "" {
			result.Selected = a.Key
			result.State = "selected"
			if a.Expires != nil && *a.Expires <= time.Now().Unix() {
				result.State = "expired"
			}
			return nil
		}
	}
	return errors.New("Codex login and saved account selection are unconfirmed")
}

func removeSavedAccount(accounts []savedCodexAccount, key string) []savedCodexAccount {
	if key == "" {
		return accounts
	}
	filtered := make([]savedCodexAccount, 0, len(accounts))
	for _, account := range accounts {
		if account.Key != key {
			filtered = append(filtered, account)
		}
	}
	return filtered
}

func accountLoginEnvironment(env profileClientEnvironment) ([]string, func(), error) {
	if len(env.Arguments) == 0 {
		return env.Environment, func() {}, nil
	}
	codex, err := profileAccountCodex(env)
	if err != nil {
		return nil, nil, err
	}
	dir, err := os.MkdirTemp(env.Runtime, "account-cli-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	parts := []string{shellQuote(codex)}
	for _, arg := range env.Arguments {
		parts = append(parts, shellQuote(arg))
	}
	script := "#!/bin/sh\nexec " + strings.Join(parts, " ") + " \"$@\"\n"
	if err = os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0700); err != nil {
		cleanup()
		return nil, nil, err
	}
	path := dir + string(os.PathListSeparator) + environmentValue(env.Environment, "PATH")
	return replaceEnvironment(env.Environment, map[string]*string{"PATH": &path}), cleanup, nil
}

// Resolve the native client from the captured environment, not the popup's PATH.
func profileAccountCodex(env profileClientEnvironment) (string, error) {
	for _, dir := range filepath.SplitList(environmentValue(env.Environment, "PATH")) {
		if !filepath.IsAbs(dir) {
			continue
		}
		path := filepath.Join(dir, "codex")
		resolved, err := resolveNativeCodex(path)
		if err == nil {
			return resolved, nil
		}
	}
	return "", errors.New("native Codex is unavailable in this Profile's tool environment")
}
