package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSessionsIncludeExternalHerdrAgentsWithoutAdopting(t *testing.T) {
	root := visibilityTestRoot(t)
	os.Chmod(root, 0700)
	t.Setenv("AGENT_HARBOR_BRIDGE_HOME", root)
	if err := ensureHome(root); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "herdr")
	os.MkdirAll(dir, 0700)
	socket := filepath.Join(dir, "herdr.sock")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	t.Setenv("HERDR_SOCKET_PATH", socket)
	go func() {
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			go func() {
				defer c.Close()
				var req map[string]any
				json.NewDecoder(c).Decode(&req)
				json.NewEncoder(c).Encode(map[string]any{"result": map[string]any{"agents": []map[string]any{{"pane_id": "w1:p3", "tab_id": "w1:t2", "agent": "codex", "name": "Manual", "cwd": "/project"}}}})
			}()
		}
	}()
	views, err := (harborService{}).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].Name != "Manual" || views[0].Mode != "unknown" || views[0].Status != "Unknown" {
		t.Fatalf("external agent missing or intent inferred: %+v", views)
	}
	var records []session
	withSessions(func(all []session) ([]session, error) { records = all; return nil, nil })
	if len(records) != 0 {
		t.Fatal("external agent adopted")
	}

	owned := session{ProcessGroup: syscall.Getpgrp(), RunToken: "fixture-owned", ID: "owned", Name: "Recorded", CLI: "codex", HerdrPane: "w1:p3", HerdrSocket: socket, HerdrSocketIdentity: socketIdentity(socket)}
	if err := withSessions(func([]session) ([]session, error) { return []session{owned}, nil }); err != nil {
		t.Fatal(err)
	}
	views, err = (harborService{}).List()
	if err != nil || len(views) != 2 || views[0].ID != "owned" || !views[1].External {
		t.Fatalf("owned pane duplicated: %+v %v", views, err)
	}

	owned.ProcessGroup = 0
	withSessions(func([]session) ([]session, error) { return []session{owned}, nil })
	views, err = (harborService{}).List()
	if err != nil || len(views) != 2 || !views[1].External {
		t.Fatalf("exited record claimed new runtime intent: %+v %v", views, err)
	}
	owned.ProcessGroup = syscall.Getpgrp()
	owned.HerdrSocketIdentity = "stale"
	withSessions(func([]session) ([]session, error) { return []session{owned}, nil })
	views, err = (harborService{}).List()
	if err != nil || len(views) != 2 || !views[1].External {
		t.Fatalf("stale ownership hid external pane: %+v %v", views, err)
	}
	l.Close()
	views, err = (harborService{}).List()
	if err != nil || len(views) != 1 || views[0].DiscoveryWarning == "" {
		t.Fatalf("upstream failure hid record/warning: %+v %v", views, err)
	}
}

func TestExternalAgentActionsAreReadOnly(t *testing.T) {
	visibilityTestRoot(t)
	s := session{ID: "external:unknown", HerdrPane: "w1:p3", Mode: "unknown"}
	a := sessionApproval{Session: s, Confirmed: true}
	h := harborService{}
	for name, action := range map[string]func() error{
		"start": func() error { return h.Start(a) }, "resume": func() error { return h.Resume(a) },
		"open": func() error { return h.Open(a) }, "stop": func() error { return h.Stop(a) },
		"remove": func() error { return h.Remove(a) }, "rename": func() error { return h.Rename(s.ID, "Changed") },
		"history": func() error { return h.DeleteHistory(a, "yes") }, "bind": func() error { return h.Bind(s.ID, "other") },
		"verify":      func() error { return h.Verify(s, "other", "/project") },
		"route":       func() error { _, e := h.ObservedRoute(s.ID); return e },
		"reassociate": func() error { return h.Reassociate(a, herdrIdentity{}, "review") },
	} {
		t.Run(name, func(t *testing.T) {
			if err := action(); err == nil {
				t.Fatal("external mutation accepted")
			}
		})
	}
}

func TestExternalAgentUIBlocksManagement(t *testing.T) {
	for _, key := range []string{"s", "x", "e", "c", "d"} {
		m, _ := formFixture()
		m.formActive = false
		m.screen = "sessions"
		m.sessions = []sessionView{{session: session{ID: "external:fixture", Name: "Manual", Mode: "unknown"}, External: true, Running: true, Status: "External / unmanaged"}}
		next := appKey(m, key).(harborModel)
		if next.screen != "sessions" || !strings.Contains(next.notice, "unmanaged") {
			t.Fatalf("%s allowed: %s %s", key, next.screen, next.notice)
		}
	}
}

func TestAgentDiscoveryFailureWithoutRecordsIsExplicit(t *testing.T) {
	root := visibilityTestRoot(t)
	os.Chmod(root, 0700)
	t.Setenv("AGENT_HARBOR_BRIDGE_HOME", root)
	t.Setenv("HERDR_SOCKET_PATH", filepath.Join(root, "missing.sock"))
	views, err := (harborService{}).List()
	if err == nil || len(views) != 0 || !strings.Contains(err.Error(), "discovery unavailable") {
		t.Fatalf("silent failure: %+v %v", views, err)
	}
}

func TestAgentDiscoveryRejectsMalformedAndOversizedFrames(t *testing.T) {
	for _, body := range []string{`{"result":{}}`, `{"result":{"agents":[{"pane_id":"w:p1"},{"pane_id":"w:p1"}]}}`, strings.Repeat(" ", 65537) + `{"result":{"agents":[]}}`} {
		t.Run(fmt.Sprint(len(body)), func(t *testing.T) {
			root := visibilityTestRoot(t)
			os.Chmod(root, 0700)
			t.Setenv("AGENT_HARBOR_BRIDGE_HOME", root)
			if err := ensureHome(root); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(root, "herdr")
			os.MkdirAll(dir, 0700)
			path := filepath.Join(dir, "h.sock")
			l, err := net.Listen("unix", path)
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			t.Setenv("HERDR_SOCKET_PATH", path)
			done := make(chan struct{})
			defer close(done)
			go func() {
				c, e := l.Accept()
				if e != nil {
					return
				}
				defer c.Close()
				var req any
				json.NewDecoder(c).Decode(&req)
				c.Write([]byte(body))
				<-done
			}()
			start := time.Now()
			_, err = (harborService{}).List()
			if err == nil || time.Since(start) > 3*time.Second {
				t.Fatalf("unbounded or silent malformed reply: %v", err)
			}
		})
	}
}

// Keep Unix socket names below both Darwin and Linux limits, independently of
// a long caller TMPDIR. Every service test also gets a private runtime context.
func visibilityTestRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "hav-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	// Darwin aliases /tmp to /private/tmp. Profile identity intentionally requires
	// canonical roots, so the fixture must use the real directory too.
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_HARBOR_BRIDGE_HOME", root)
	if err := ensureHome(root); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR", "TMPDIR", "CODEX_HOME", "CODEX_SQLITE_HOME"} {
		dir := filepath.Join(root, key)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		t.Setenv(key, dir)
	}
	t.Setenv("HERDR_SOCKET_PATH", "")
	return root
}

func TestStoppedManagedWorkspaceReportsUnavailableDiscovery(t *testing.T) {
	root := visibilityTestRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "herdr", "config"), 0700); err != nil {
		t.Fatal(err)
	}
	views, err := (harborService{}).List()
	if err == nil || len(views) != 0 {
		t.Fatalf("stopped empty workspace discovery silently succeeded: %+v %v", views, err)
	}
	if err := withSessions(func([]session) ([]session, error) { return []session{{ID: "saved", Name: "Saved"}}, nil }); err != nil {
		t.Fatal(err)
	}
	views, err = (harborService{}).List()
	if err != nil || len(views) != 1 || views[0].ID != "saved" || views[0].DiscoveryWarning == "" {
		t.Fatalf("stopped workspace lost saved record/warning: %+v %v", views, err)
	}
}
