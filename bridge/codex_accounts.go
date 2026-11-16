package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

func experimentalCodexAccounts() bool {
	return os.Getenv("AGENT_HARBOR_EXPERIMENTAL_CODEX_ACCOUNTS") == "1"
}

func codexAuthProgram() (string, error) {
	return findCodexAuth(os.Getenv("AGENT_HARBOR_CODEX_AUTH_BIN"))
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

// Kept for source compatibility; mutations require an explicit Profile binding.
func (harborService) CodexAccountAction(action string, stdin io.Reader, stdout, stderr io.Writer) (string, error) {
	return "", errors.New("select a Profile and use its scoped account action; ambient account changes are disabled")
}
