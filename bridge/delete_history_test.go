package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDeleteHistoryRefusesUnconfirmedChangedOrUnverifiedConversation(t *testing.T) {
	for _, scenario := range []string{"cancelled", "wrong-name", "changed", "running", "home-changed", "ambiguous", "confirmed"} {
		t.Run(scenario, func(t *testing.T) {
			newTestRegistry(t)
			s := session{Version: 2, ID: "0123456789abcdef0123456789abcdef", Name: "API", CLI: "codex", CLIVersion: "codex-cli 0.167.0", CodexSessionID: "01912345-6789-7abc-8def-0123456789ab", Recovery: "ready", Started: true, CodexHome: filepath.Join(t.TempDir(), "original")}
			if scenario == "running" {
				s.ProcessGroup = 1
			}
			if scenario == "ambiguous" {
				s.CandidateIDs = []string{"01912345-6789-7abc-8def-0123456789ac"}
			}
			if err := withSessions(func([]session) ([]session, error) { return []session{s}, nil }); err != nil {
				t.Fatal(err)
			}
			history := filepath.Join(t.TempDir(), "history.jsonl")
			if err := os.WriteFile(history, []byte("preserved synthetic history"), 0600); err != nil {
				t.Fatal(err)
			}
			a := sessionApproval{Session: s, Confirmed: true}
			typed := "API"
			switch scenario {
			case "cancelled":
				a.Confirmed = false
			case "wrong-name":
				typed = "wrong"
			case "changed":
				a.Session.Name = "old"
				typed = "old"
			}
			err := (harborService{}).DeleteHistory(a, typed)
			if err == nil || !strings.Contains(err.Error(), "client’s own tools") {
				t.Fatal("deletion accepted")
			}
			if strings.Contains(err.Error(), "native Codex") {
				t.Fatalf("identity check deferred to CLI: %v", err)
			}
			if _, err := getSession(s.ID); err != nil {
				t.Fatal("record removed on refusal:", err)
			}
			if data, err := os.ReadFile(history); err != nil || string(data) != "preserved synthetic history" {
				t.Fatal("history changed", err)
			}
			root, _ := bridgeHome()
			if _, err := os.Stat(filepath.Join(root, "sessions.json")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRecordRemovalProtectsPendingLaunchAndHistory(t *testing.T) {
	newTestRegistry(t)
	now := time.Now()
	s := session{Version: 2, ID: "0123456789abcdef0123456789abcdef", Name: "API", Started: true, LastStartedAt: &now}
	if err := withSessions(func([]session) ([]session, error) { return []session{s}, nil }); err != nil {
		t.Fatal(err)
	}
	s, _ = getSession(s.ID)
	root, _ := bridgeHome()
	f, err := os.OpenFile(filepath.Join(root, "running-"+s.ID+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	err = (harborService{}).Remove(sessionApproval{Session: s, Confirmed: true})
	if err == nil {
		t.Fatal("removed a starting session")
	}
	if _, err := getSession(s.ID); err != nil {
		t.Fatal(err)
	}
}
