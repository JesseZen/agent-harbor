package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestAccountRowCursorHoverAndLogin(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("AGENT_HARBOR_ASCII", "0")
	t.Setenv("NO_COLOR", "")
	os.Unsetenv("NO_COLOR")
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })

	m := newHarborModel(&fakeApplicationService{})
	m.screen, m.accountRow, m.uiFocus = "accounts", "first", accountRowPrefix+"first"
	m.accountProfile = &profileBinding{ProfileID: "test"}
	m.accountSnapshot = &profileAccountResult{
		ProfileName: "Default", State: "selected", Selected: "first",
		Accounts: []savedCodexAccount{
			{Key: "first", Email: "first@example.test", Plan: "plus"},
			{Key: "second", Email: "second@example.test", Plan: "plus"},
		},
	}
	page, _ := m.accountsPage()
	var target uiRect
	for _, hit := range page.Hits {
		if hit.ID == accountRowPrefix+"second" {
			target = hit
		}
	}
	if target.W == 0 {
		t.Fatal("second row has no mouse target")
	}
	verify := func(email, mark, fgRole, bgRole string) {
		t.Helper()
		page, _ := m.accountsPage()
		for _, row := range page.Lines {
			if !strings.Contains(row, email) {
				continue
			}
			style := fg(fgRole).Background(lipgloss.Color(harborPalette[bgRole]))
			want := paint(style, mark+email)
			prefix := strings.TrimSuffix(want, "\x1b[0m")
			if !strings.HasPrefix(row, prefix) {
				t.Fatalf("%s: got %q, expected prefix %q", email, row, prefix)
			}
			if strings.Contains(ansi.Strip(row), "✓") != (email == "first@example.test") {
				t.Fatalf("wrong current login in %q", row)
			}
			return
		}
		t.Fatalf("no row for %s", email)
	}
	verify("first@example.test", "> ", "section", "bg")
	n, _ := m.Update(tea.MouseMsg{X: target.X, Y: target.Y, Action: tea.MouseActionMotion})
	m = n.(harborModel)
	verify("first@example.test", "> ", "section", "bg")
	verify("second@example.test", "  ", "text", "selBg")
	n, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = n.(harborModel)
	verify("first@example.test", "  ", "section", "bg")
	verify("second@example.test", "> ", "brand", "selBg")
	n, _ = m.Update(tea.MouseMsg{X: 0, Y: 0, Action: tea.MouseActionMotion})
	m = n.(harborModel)
	verify("second@example.test", "> ", "brand", "bg")
}

func TestAccountDetailsAlignFieldsAndReadCardExpiry(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	m.screen, m.accountRow, m.width = "accounts-detail", "A", 42
	var account savedCodexAccount
	err := json.Unmarshal([]byte(`{
	    "account_key": "A", "email": "a@example.test", "alias": "work", "plan": "plus",
	    "account_name": "多字节工作区名称超出详情值栏宽度",
	    "usage": {"source": "api", "refresh": {"status": "ok"}, "reset_credits": 1,
	        "reset_credit": [{"title":"Fixture reset card", "status":"available", "expires_at":"2090-12-15T15:21:00Z"}]}
	}`), &account)
	if err != nil || !account.Usage.hasResetCard() {
		t.Fatalf("did not read redeemable card: %v", err)
	}
	m.accountSnapshot = &profileAccountResult{
		ProfileName: "Default", State: "selected", Selected: "A", Accounts: []savedCodexAccount{account},
	}
	fields := map[string]string{
		"Profile": "Default", "Email": "a@example.test", "Alias": "work", "Plan": "plus",
		"5h left": "N/A", "7d left": "N/A", "Reset cards": "1 available",
		"Expires": account.Usage.ResetCredits[0].expiration().Local().Format(weeklyResetFormat),
	}
	lines := m.accountContent().Lines
	for _, line := range lines {
		line = ansi.Strip(line)
		if ansi.StringWidth(line) > m.width-2 || strings.Contains(line, "Current login") || strings.Contains(line, "Fixture reset card") {
			t.Fatalf("unexpected detail line %q", line)
		}
		for label, value := range fields {
			if line == label+strings.Repeat(" ", 14-len(label))+value {
				delete(fields, label)
				break
			}
		}
	}
	if len(fields) != 0 {
		t.Fatalf("missing aligned fields %v in %q", fields, lines)
	}
	if !strings.Contains(ansi.Strip(strings.Join(lines, "\n")), "\n              栏宽度") {
		t.Fatalf("wrapped workspace name did not continue in the value column: %q", lines)
	}
}

func TestAccountsShowsProfileQuotaByAccountKey(t *testing.T) {
	m := newHarborModel(&fakeApplicationService{})
	binding := profileBinding{ProfileID: "A"}
	m.screen, m.accountProfile, m.accountGeneration = "accounts", &binding, 2
	m.accountSnapshot = &profileAccountResult{
		State: "selected", Selected: "A",
		Accounts: []savedCodexAccount{
			{Key: "A", Email: "team@example.test", Plan: "team", Active: true},
			{Key: "B", Email: "other@example.test", Plan: "plus"},
		},
	}
	used := 73.0
	usage := &savedAccountUsage{Source: "api", Primary: &savedUsageWindow{UsedPercent: &used, Minutes: 10080, ResetsAt: 2000000000}}
	usage.Refresh.Status = "ok"
	other := *usage
	other.Refresh.Status = "http_error"
	refresh := appAccountRead{generation: 1, request: codexAccountRequest{Profile: binding, Action: "usage"}}
	refresh.result.Accounts = []savedCodexAccount{{Key: "B", Usage: &other}, {Key: "A", Usage: usage}}
	next, _, _ := m.accountsPageInput(refresh)
	if next.(harborModel).accountSnapshot.Accounts[0].Usage != nil {
		t.Fatal("stale refresh supplied quota")
	}
	refresh.generation = 2
	refresh.request.Profile.ProfileID = "another-profile"
	next, _, _ = m.accountsPageInput(refresh)
	if next.(harborModel).accountSnapshot.Accounts[0].Usage != nil {
		t.Fatal("another Profile supplied quota")
	}
	refresh.request.Profile = binding
	next, _, _ = m.accountsPageInput(refresh)
	m = next.(harborModel)
	for _, width := range []int{160, 100, 80, 70, 69, 60, 42, 36} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			m.width, m.height = width, 24
			page := ansi.Strip(m.View())
			if strings.Count(page, "27%") != 1 || strings.Contains(page, "73%") ||
				!strings.Contains(page, "Email") || !strings.Contains(page, "Plan") ||
				!strings.Contains(page, "5h left") || !strings.Contains(page, "7d left") ||
				!strings.Contains(page, "Claude Code") || !strings.Contains(page, "Antigravity") ||
				strings.Count(page, "Not supported") != 2 {
				t.Fatal(page)
			}
			for _, line := range strings.Split(page, "\n") {
				if !strings.Contains(line, "27%") {
					continue
				}
				if !strings.Contains(line, "team") || !strings.Contains(line, "N/A") ||
					strings.Contains(line, time.Unix(2000000000, 0).Local().Format("Jan 02 Mon 3:04 PM")) != (width >= 70) ||
					strings.ContainsAny(line, "▆=") != (width >= 88) {
					t.Fatal(line)
				}
			}
		})
	}
}

func TestAccountQuotaResetFormat(t *testing.T) {
	at := time.Date(2026, time.November, 18, 12, 42, 0, 0, time.Local).Unix()
	short, weekly := 12.0, 68.0
	usage := savedAccountUsage{Source: "api"}
	usage.Refresh.Status = "ok"
	usage.Primary = &savedUsageWindow{UsedPercent: &short, Minutes: 300, ResetsAt: at}
	usage.Secondary = &savedUsageWindow{UsedPercent: &weekly, Minutes: 10080, ResetsAt: at}
	for _, tc := range []struct {
		minutes, width int
		want           string
	}{
		{300, 7, "88%"},
		{300, shortQuotaWidth, "88%  12:42 PM"},
		{10080, 7, "32%"},
		{10080, weeklyQuotaWidth, "32%  Nov 18 Wed 12:42 PM"},
	} {
		if got := strings.TrimSpace(ansi.Strip(accountQuotaCell(&usage, false, tc.minutes, tc.width))); got != tc.want {
			t.Errorf("%d min at %d columns: got %q, want %q", tc.minutes, tc.width, got, tc.want)
		}
	}
}

func TestQuotaRemainingRole(t *testing.T) {
	for remaining, want := range map[int]string{0: "bad", 20: "bad", 21: "warn", 40: "warn", 41: "ok", 100: "ok"} {
		if got := quotaRemainingRole(remaining); got != want {
			t.Errorf("%d%% remaining = %s, want %s", remaining, got, want)
		}
	}
}

func TestExperimentalAccountEntryAndResult(t *testing.T) {
	t.Setenv("AGENT_HARBOR_EXPERIMENTAL_CODEX_ACCOUNTS", "1")
	m := newHarborModel(&fakeApplicationService{})
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m = next.(harborModel)
	if m.screen != "accounts" {
		t.Fatal(m.screen)
	}
	detail := m
	detail.screen = "accounts-detail"
	if !strings.Contains(detail.View(), "between replies") || !strings.Contains(detail.View(), "undocumented") {
		t.Fatalf("account entry lacks switching guidance: %s", m.View())
	}
	m.screen = "accounts-busy"
	next, _ = m.Update(appAccountDone{result: "Selected account confirmed in 2 running Harbor session(s)."})
	m = next.(harborModel)
	if m.screen != "accounts-result" || !strings.Contains(m.View(), "2 running Harbor") {
		t.Fatalf("account result is not visible: %s", m.View())
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if next.(harborModel).screen != "accounts" {
		t.Fatal("escape did not return to the account menu")
	}
}
