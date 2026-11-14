package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestAuthoritativeExternalActivity(t *testing.T) {
	for _, state := range []string{"idle", "working", "blocked", "done", "unknown", "future"} {
		t.Run(state, func(t *testing.T) {
			root := visibilityTestRoot(t)
			dir := filepath.Join(root, "herdr")
			os.MkdirAll(dir, 0700)
			socket := filepath.Join(dir, "state.sock")
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
						var req any
						json.NewDecoder(c).Decode(&req)
						json.NewEncoder(c).Encode(map[string]any{"result": map[string]any{"agents": []map[string]any{{"terminal_id": "term-1", "workspace_id": "w1", "pane_id": "w1:p1", "tab_id": "w1:t1", "agent": "codex", "agent_status": state}}}})
					}()
				}
			}()
			views, err := (harborService{}).List()
			if err != nil || len(views) != 1 {
				t.Fatalf("list: %+v %v", views, err)
			}
			raw, _ := json.Marshal(views[0])
			var v map[string]any
			json.Unmarshal(raw, &v)
			expected := state
			if state == "future" {
				expected = "unknown"
			}
			runtime, ok := v["runtime"].(map[string]any)
			if !ok || runtime["activity"] != expected || runtime["presence"] != "observed" || runtime["source"] != "herdr.agent.list" {
				t.Fatalf("missing authoritative runtime: %s", raw)
			}
			if views[0].Status == "External / unmanaged" {
				t.Fatal("ownership replaced activity")
			}
		})
	}
}

func TestOwnedActivityKeepsHealthAndAbsenceSeparate(t *testing.T) {
	root := visibilityTestRoot(t)
	socket := filepath.Join(root, "herdr", "state.sock")
	os.MkdirAll(filepath.Dir(socket), 0700)
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	t.Setenv("HERDR_SOCKET_PATH", socket)
	ref := herdrIdentity{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: "original"}
	c := conversationEvidence{Reference: ref, RunToken: "run", Pane: "w1:p1", SocketIdentity: socketIdentity(socket), Status: "observed"}
	s := session{ID: "owned", CLI: "codex", Started: true, ProcessGroup: syscall.Getpgrp(), RunToken: "run", HerdrPane: "w1:p1", HerdrTab: "w1:t1", HerdrSocket: socket, HerdrSocketIdentity: socketIdentity(socket), Conversation: &c, WorkingDirectory: "/missing-owned-project"}
	withSessions(func([]session) ([]session, error) { return []session{s}, nil })
	var present atomic.Bool
	present.Store(true)
	go func() {
		for {
			conn, e := l.Accept()
			if e != nil {
				return
			}
			go func() {
				defer conn.Close()
				var req any
				json.NewDecoder(conn).Decode(&req)
				agents := []map[string]any{}
				if present.Load() {
					agents = append(agents, map[string]any{"terminal_id": "term", "workspace_id": "w1", "pane_id": "w1:p1", "tab_id": "w1:t1", "agent": "codex", "agent_status": "idle", "agent_session": ref})
				}
				json.NewEncoder(conn).Encode(map[string]any{"result": map[string]any{"agents": agents}})
			}()
		}
	}()
	h := harborService{}
	v, err := h.Get("owned")
	if err != nil || !v.Runtime.Associated || v.Status != "Idle" || v.Reason == "" {
		t.Fatalf("activity/health: %+v %v", v, err)
	}
	if !strings.Contains(newHarborModel(h).details(v), "Idle") || !strings.Contains(newHarborModel(h).details(v), "Needs attention") {
		t.Fatal("UI hid activity or health")
	}
	present.Store(false)
	v, err = h.Get("owned")
	if err != nil || v.Runtime.Presence != "not_observed" || v.Running {
		t.Fatalf("absence became exit/live: %+v %v", v, err)
	}
	now := time.Now().UTC()
	s.ProcessGroup = 0
	s.LastExitAt = &now
	withSessions(func([]session) ([]session, error) { return []session{s}, nil })
	v, err = h.Get("owned")
	if err != nil || v.Runtime.Presence != "historical_exit" || v.Runtime.Activity != "unknown" {
		t.Fatalf("historical exit: %+v %v", v, err)
	}
	l.Close()
	v, err = h.Get("owned")
	if err != nil || v.DiscoveryWarning == "" || v.Runtime.Activity != "unknown" {
		t.Fatalf("disconnected: %+v %v", v, err)
	}
}
