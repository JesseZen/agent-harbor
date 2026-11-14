package main

import (
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
 'pane get') printf '%s\n' '{"result":{"pane":{"pane_id":"w1:p2","tab_id":"w1:t2"}}}' ;;
 'pane process-info') cat '` + process + `' ;;
 'tab list') printf '%s\n' '{"result":{"tabs":[{"tab_id":"w1:t2","label":"API"}]}}' ;;
 'session list') printf '%s\n' '{"sessions":[{"name":"agent-harbor","running":true}]}' ;;
 'pane close') touch '` + filepath.Join(root, "closed") + `' ;;
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
	if err := h.ClosePreviousPane(review); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "closed")); err != nil {
		t.Fatal(err)
	}
}
