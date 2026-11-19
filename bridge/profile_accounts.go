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

type codexAccountRequest struct {
	Profile  profileBinding
	Action   string // inspect, usage, reset, login, switch, signed-out
	Selected string
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
}
type accountSessionOutcome struct{ ID, Name, State, Detail string }

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
			result.Sessions = append(result.Sessions, accountSessionOutcome{ID: s.ID, Name: s.Name, State: "not applied", Detail: "Future start retains this Conversation and connection."})
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
		for _, s := range scope.Sessions {
			if sessionLaunchPending(s) {
				return fmt.Errorf("%s is starting; wait before changing this Profile's login", s.Name)
			}
			if processGroupActive(s.ProcessGroup) {
				idle, err := codexAccountIdle(s)
				if err != nil {
					return fmt.Errorf("%s: %w; explicitly stop this session before changing this Profile's login", s.Name, err)
				}
				if !idle {
					return fmt.Errorf("%s: a turn is active; finish it before changing this Profile's login", s.Name)
				}
			}
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
		childEnv, cleanup, err := accountLoginEnvironment(env)
		if err != nil {
			return err
		}
		defer cleanup()
		var cmd *exec.Cmd
		switch req.Action {
		case "login":
			cmd = exec.CommandContext(ctx, tool, "login")
		case "switch":
			cmd = exec.CommandContext(ctx, tool, "switch", "--skip-api", "--no-restart-daemon")
		case "signed-out":
			codex, err := profileAccountCodex(env)
			if err != nil {
				return err
			}
			args := append(append([]string{}, env.Arguments...), "logout")
			cmd = exec.CommandContext(ctx, codex, args...)
		}
		cmd.Env, cmd.Stdin, cmd.Stdout, cmd.Stderr = childEnv, in, out, diagnostic
		// Login runs outside project configuration. codex-auth owns its temporary
		// CODEX_HOME and grants; the scoped shim only enforces CLI backend policy.
		cmd.Dir = env.Home
		actionErr := cmd.Run()
		readErr := readProfileAccounts(context.WithoutCancel(ctx), tool, env, &result)
		for i, s := range scope.Sessions {
			outcome := &result.Sessions[i]
			if !processGroupActive(s.ProcessGroup) {
				continue
			}
			if s.Mode == "magpie" && !joinedCodex(s) {
				outcome.State = "connection retained"
				outcome.Detail = "API provider unchanged; no official identity adoption claimed."
			} else {
				outcome.State = "handoff required"
				outcome.Detail = "Stop and review exact Conversation recovery with this Profile's intended login; no process was restarted."
				if actionErr == nil && readErr == nil && result.State == "selected" && experimentalCodexAccounts() {
					if _, err := refreshCodexAccount(s, result.Selected); err == nil {
						outcome.State = "verified (experimental)"
						outcome.Detail = "Selected identity confirmed using experimental refresh."
					}
				}
			}
		}
		if actionErr != nil {
			return errors.New("account action cancelled or failed; the displayed state was reread, running identities remain unconfirmed")
		}
		if readErr != nil {
			return readErr
		}
		if req.Action == "signed-out" && result.State != "signed-out" {
			return errors.New("Codex did not confirm sign-out; repair this Profile's login")
		}
		return nil
	})
	return result, err
}

func resetProfileAccount(ctx context.Context, tool string, env profileClientEnvironment, selected string) error {
	list, err := listProfileAccounts(ctx, tool, env, "--skip-api")
	if err != nil {
		return err
	}
	// codex-auth tries the exact registry key before its other selector forms.
	for _, account := range list.Accounts {
		if selected != "" && account.Key == selected {
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, tool, "reset", selected)
			cmd.Env, cmd.Dir = env.Environment, env.Home
			if err := cmd.Run(); err != nil {
				return errors.New("codex-auth did not confirm the reset")
			}
			return nil
		}
	}
	return errors.New("the saved login is no longer in this Profile")
}

type savedAccountList struct {
	Version  int                 `json:"schema_version"`
	Active   string              `json:"active_account_key"`
	Accounts []savedCodexAccount `json:"accounts"`
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

// Unknown activity is an error, never evidence that a session is between turns.
func codexAccountIdle(s session) (bool, error) {
	if s.CodexServerPID <= 1 || s.CodexServerSocket == "" {
		return false, errors.New("no dedicated Codex App Server, so activity cannot be checked")
	}
	if !verifyCodexServer(s) {
		return false, errors.New("could not verify the dedicated Codex App Server")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rpc, err := connectCodexRPC(ctx, s.CodexServerSocket)
	if err != nil {
		return false, err
	}
	defer rpc.conn.CloseNow()
	var loaded struct {
		Data []string `json:"data"`
	}
	if err = rpc.call(ctx, "thread/loaded/list", map[string]any{}, &loaded); err != nil {
		return false, err
	}
	for _, id := range loaded.Data {
		var state struct {
			Thread struct {
				Status struct {
					Type string `json:"type"`
				} `json:"status"`
			} `json:"thread"`
		}
		if err = rpc.call(ctx, "thread/read", map[string]any{"threadId": id, "includeTurns": false}, &state); err != nil {
			return false, err
		}
		switch state.Thread.Status.Type {
		case "idle":
		case "active":
			return false, nil
		default:
			return false, errors.New("Codex thread activity could not be verified")
		}
	}
	return true, nil
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
