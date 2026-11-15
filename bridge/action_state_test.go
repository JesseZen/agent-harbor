package main

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestWaitForLaunchStateAllowsHerdrObservationToClear(t *testing.T) {
	calls := 0
	err := waitForLaunchStateFn(func() error {
		calls++
		if calls < 3 {
			return errConversationObserved
		}
		return nil
	}, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("stale observation did not clear: %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected two retries, got %d validation calls", calls)
	}
}

func TestWaitForLaunchStateDoesNotRetryUnrelatedErrors(t *testing.T) {
	want := errors.New("session changed; review it again")
	calls := 0
	err := waitForLaunchStateFn(func() error {
		calls++
		return want
	}, 500*time.Millisecond)
	if !errors.Is(err, want) || calls != 1 {
		t.Fatalf("unrelated launch error was retried or changed: err=%v calls=%d", err, calls)
	}
}

func TestFocusRechecksExternalRuntimeIdentity(t *testing.T) {
	root := visibilityTestRoot(t)
	path := filepath.Join(root, "herdr", "actions.sock")
	os.MkdirAll(filepath.Dir(path), 0700)
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	t.Setenv("HERDR_SOCKET_PATH", path)
	var replaced atomic.Bool
	var unknown atomic.Bool
	var focused atomic.Int32
	go func() {
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			go func() {
				defer c.Close()
				var req struct {
					Method string `json:"method"`
					Params struct {
						Target string `json:"target"`
					} `json:"params"`
				}
				json.NewDecoder(c).Decode(&req)
				if req.Method == "agent.focus" {
					if req.Params.Target != "w1:p1" {
						json.NewEncoder(c).Encode(map[string]any{"error": map[string]any{"code": "invalid_target"}})
						return
					}
					focused.Add(1)
					json.NewEncoder(c).Encode(map[string]any{"result": map[string]any{}})
					return
				}
				terminal := "first"
				if replaced.Load() {
					terminal = "replacement"
				}
				json.NewEncoder(c).Encode(map[string]any{"result": map[string]any{"agents": []map[string]any{{"terminal_id": terminal, "workspace_id": "w1", "pane_id": "w1:p1", "tab_id": "w1:t1", "agent": "codex", "agent_status": map[bool]string{false: "done", true: "unknown"}[unknown.Load()]}}}})
			}()
		}
	}()
	h := harborService{}
	rows, err := h.List()
	if err != nil {
		t.Fatal(err)
	}
	// Done remains focusable; it never enters recovery.
	if err = h.FocusObserved(rows[0]); err != nil || focused.Load() != 1 {
		t.Fatalf("done focus: %v", err)
	}
	unknown.Store(true)
	if err = h.FocusObserved(rows[0]); err != nil || focused.Load() != 2 {
		t.Fatal("observed unknown activity could not focus its verified target", err)
	}
	replaced.Store(true)
	if err = h.FocusObserved(rows[0]); err == nil || focused.Load() != 2 {
		t.Fatal("old pane selection focused replacement", err)
	}
}

func TestUnobservedStartedRecordCannotLaunch(t *testing.T) {
	root := visibilityTestRoot(t)
	s := session{ID: "0123456789abcdef0123456789abcdef", Started: true, CLI: "codex", WorkingDirectory: root, Mode: "local"}
	withSessions(func([]session) ([]session, error) { return []session{s}, nil })
	if err := validateLaunchState(s); err == nil {
		t.Fatal("absence treated as safe recovery")
	}
}

func TestConfirmationDoesNotRenameReusedTerminal(t *testing.T) {
	root := visibilityTestRoot(t)
	path := filepath.Join(root, "herdr", "review.sock")
	os.MkdirAll(filepath.Dir(path), 0700)
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	t.Setenv("HERDR_SOCKET_PATH", path)
	ref := herdrIdentity{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: "original"}
	s := session{Version: 3, ID: "0123456789abcdef0123456789abcdef", Name: "Original", CLI: "codex", Started: true, RunToken: "run", ProcessGroup: syscall.Getpgrp(), HerdrPane: "w1:p1", HerdrTab: "w1:t1", HerdrSocket: path, HerdrSocketIdentity: socketIdentity(path)}
	s.Conversation = &conversationEvidence{Reference: ref, RunToken: "run", Pane: s.HerdrPane, SocketIdentity: s.HerdrSocketIdentity, Status: "observed"}
	withSessions(func([]session) ([]session, error) { return []session{s}, nil })
	var replacement atomic.Bool
	go func() {
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			go func() {
				defer c.Close()
				var req any
				json.NewDecoder(c).Decode(&req)
				terminal := "original"
				if replacement.Load() {
					terminal = "replacement"
				}
				json.NewEncoder(c).Encode(map[string]any{"result": map[string]any{"agents": []map[string]any{{"terminal_id": terminal, "workspace_id": "w1", "pane_id": s.HerdrPane, "tab_id": s.HerdrTab, "agent": "codex", "agent_status": "idle", "agent_session": ref}}}})
			}()
		}
	}()
	h := harborService{}
	v, _, err := currentRuntime(s.ID)
	if err != nil || !v.Runtime.Associated {
		t.Fatal(v, err)
	}
	replacement.Store(true)
	if err = h.RenameObserved(v, "Wrong"); err == nil {
		t.Fatal("confirmation applied to reused terminal")
	}
	current, err := getSession(s.ID)
	if err != nil || current.Name != "Original" {
		t.Fatal("record changed before target verification", current, err)
	}
}
