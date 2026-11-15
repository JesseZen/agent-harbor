package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreviousPaneReviewRefusesForegroundWorkAndChangedReview(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "hprev-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	t.Setenv("AGENT_HARBOR_BRIDGE_HOME", root)
	if err := ensureHome(root); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "herdr"), 0700); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(root, "herdr", "s.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			c, e := listener.Accept()
			if e != nil {
				return
			}
			go func() {
				defer c.Close()
				var req any
				json.NewDecoder(c).Decode(&req)
				json.NewEncoder(c).Encode(map[string]any{"result": map[string]any{"agents": []any{}}})
			}()
		}
	}()

	s := session{Version: 2, ID: "0123456789abcdef0123456789abcdef", Name: "API", HerdrPane: "w1:p2", HerdrTab: "w1:t2", HerdrSocket: socket, HerdrSocketIdentity: "previous-server", Started: true}
	if err := withSessions(func([]session) ([]session, error) { return []session{s}, nil }); err != nil {
		t.Fatal(err)
	}
	process := filepath.Join(root, "process.json")
	shell := `{"result":{"process_info":{"pane_id":"w1:p2","shell_pid":321,"foreground_processes":[{"pid":321,"name":"sh"}]}}}`
	if err := os.WriteFile(process, []byte(shell), 0600); err != nil {
		t.Fatal(err)
	}
	stub := `#!/bin/sh
case "$3 $4" in
 'pane get') printf '%s\n' '{"result":{"pane":{"pane_id":"w1:p2","tab_id":"w1:t2","terminal_id":"term-original"}}}' ;;
	 'pane process-info')
		if test -f '` + filepath.Join(root, "busy-after-evidence") + `' && grep -q '"ended_evidence"' '` + filepath.Join(root, "sessions.json") + `'; then
			printf '%s\n' '{"result":{"process_info":{"pane_id":"w1:p2","shell_pid":321,"foreground_processes":[{"pid":456,"name":"vim"}]}}}'
		else cat '` + process + `'; fi ;;
 'tab list') printf '%s\n' '{"result":{"tabs":[{"tab_id":"w1:t2","label":"API"}]}}' ;;
 'session list') printf '%s\n' '{"sessions":[{"name":"agent-harbor","running":true}]}' ;;
	'pane close')
		# Closing the active pane can terminate the caller immediately. The
		# reviewed exit must already be durable when the close is requested.
		grep -q '"ended_evidence"' '` + filepath.Join(root, "sessions.json") + `' || exit 42
		test ! -f '` + filepath.Join(root, "fail-close") + `' || exit 43
		touch '` + filepath.Join(root, "closed") + `' ;;
 *) exit 2 ;;
esac
`
	path := filepath.Join(root, "herdr-stub")
	if err := os.WriteFile(path, []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_HARBOR_HERDR_BIN", path)
	s, err = getSession(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	h := harborService{}
	review, err := h.PreviousPane(s)
	if err != nil {
		t.Fatal(err)
	}
	if review.ShellPID != 321 || review.SocketIdentity == s.HerdrSocketIdentity {
		t.Fatal("current shell identity not reviewed")
	}
	busy := strings.Replace(shell, `"pid":321,"name":"sh"`, `"pid":456,"name":"vim"`, 1)
	if err := os.WriteFile(process, []byte(busy), 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.ClosePreviousPane(review); err == nil {
		t.Fatal("closed foreground work")
	}
	if _, err := os.Stat(filepath.Join(root, "closed")); !os.IsNotExist(err) {
		t.Fatal("close invoked on changed pane")
	}
	if err := os.WriteFile(process, []byte(shell), 0600); err != nil {
		t.Fatal(err)
	}
	busyAfterEvidence := filepath.Join(root, "busy-after-evidence")
	if err := os.WriteFile(busyAfterEvidence, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.ClosePreviousPane(review); err == nil || !strings.Contains(err.Error(), "confirmed ended") {
		t.Fatalf("work started during evidence write was not refused: %v", err)
	}
	ended, err := getSession(s.ID)
	if err != nil || ended.Ended == nil || ended.Ended.Source != "herdr.reviewed_idle_shell" {
		t.Fatal("recheck failure lost approved end evidence", ended, err)
	}
	if _, err := os.Stat(filepath.Join(root, "closed")); !os.IsNotExist(err) {
		t.Fatal("closed work started during evidence write")
	}
	if err := os.Remove(busyAfterEvidence); err != nil {
		t.Fatal(err)
	}
	review, err = h.PreviousPane(ended)
	if err != nil {
		t.Fatal(err)
	}
	failClose := filepath.Join(root, "fail-close")
	if err := os.WriteFile(failClose, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.ClosePreviousPane(review); err == nil || !strings.Contains(err.Error(), "confirmed ended") {
		t.Fatalf("close failure did not explain durable end review: %v", err)
	}
	ended, err = getSession(s.ID)
	if err != nil || ended.Ended == nil || ended.Ended.Source != "herdr.reviewed_idle_shell" {
		t.Fatal("failed shell close lost approved end evidence", ended, err)
	}
	if _, err := os.Stat(filepath.Join(root, "closed")); !os.IsNotExist(err) {
		t.Fatal("failed close claimed the shell was closed")
	}
	if err := os.Remove(failClose); err != nil {
		t.Fatal(err)
	}
	review, err = h.PreviousPane(ended)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.ClosePreviousPane(review); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "closed")); err != nil {
		t.Fatal(err)
	}
	ended, err = getSession(s.ID)
	if err != nil || ended.Ended == nil || ended.Ended.Source != "herdr.reviewed_idle_shell" {
		t.Fatal("approved idle shell did not preserve ended evidence", ended, err)
	}
	view, err := h.Get(s.ID)
	if err != nil || view.Runtime.Presence != "confirmed_exit" {
		t.Fatal("approved end not visible through service", view, err)
	}
}
