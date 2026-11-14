package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSessionStatusExplainsMissingIdentity(t *testing.T) {
	b := newTestBridge(t)
	if out, err := b.command("create", "--directory", b.project); err != nil {
		t.Fatal(out, err)
	}
	out, err := b.command("show", b.sessions()[0].ID)
	if err != nil {
		t.Fatal(out, err)
	}
	var view struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(out), &view); err != nil {
		t.Fatal(err)
	}
	if view.Status != "Needs attention" || view.Reason == "" {
		t.Fatalf("unexplained status: %s", out)
	}
}

func TestStopTerminatesOnlyItsCodexGroup(t *testing.T) {
	b := newTestBridge(t)
	native, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.root, "bin", "codex"), native, 0700); err != nil {
		t.Fatal(err)
	}
	b.env = append(b.env, "BRIDGE_STUB_HOLD=1")
	cmd := exec.Command(os.Args[0], "-test.run=^TestBridgeProcess$", "--", "new", "--dir", b.project)
	cmd.Env = b.env
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	group := 0
	t.Cleanup(func() {
		input.Close()
		if group > 1 {
			_ = syscall.Kill(-group, syscall.SIGKILL)
		}
		_ = cmd.Wait()
	})
	deadline := time.Now().Add(5 * time.Second)
	var record session
	for time.Now().Before(deadline) {
		records := b.sessions()
		if len(records) == 1 && records[0].ProcessGroup > 1 {
			record = records[0]
			group = record.ProcessGroup
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if group <= 1 {
		t.Fatal("Codex group never recorded")
	}
	if group == cmd.Process.Pid {
		t.Fatal("Codex shares Harbor's group")
	}
	// The CLI/service must retain liveness independently of connection health.
	if err := os.Rename(b.project, b.project+"-moved"); err != nil {
		t.Fatal(err)
	}
	out, err := b.command("show", record.ID)
	if err != nil {
		t.Fatal(out, err)
	}
	var liveView sessionView
	if err := json.Unmarshal([]byte(out), &liveView); err != nil {
		t.Fatal(err)
	}
	if !liveView.Running || liveView.Status != "Needs attention" || liveView.Reason == "" {
		t.Fatalf("lost live process behind health warning: %s", out)
	}
	if out, err := b.command("stop-session", record.ID); err != nil {
		t.Fatal(out, err)
	}
	if processGroupActive(group) {
		t.Fatal("Codex group survived stop")
	}
}

func TestObservedRouteUsesConversationAndClassifiesAuxiliary(t *testing.T) {
	const conversation = "01912345-6789-7abc-8def-0123456789ab"
	queries := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries <- r.URL.Query().Get("session")
		fmt.Fprintf(w, `{"session":%q,"seq":1,"route":{"model":"actual-provider/actual-model","served":"actual-model","kind":"title","tries":[{"model":"actual-provider/actual-model","status":200}]}}`, conversation)
	}))
	defer server.Close()
	b := newTestBridge(t)
	b.sessions()
	records := []session{{Version: 2, ID: "0123456789abcdef0123456789abcdef", WorkingDirectory: b.project, CLI: "codex", Mode: "magpie", MagpieURL: server.URL, MagpieModel: "selected-group", CodexSessionID: conversation, Recovery: "ready"}}
	data, _ := json.Marshal(records)
	if err := os.WriteFile(filepath.Join(b.root, "bridge", "sessions.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	out, err := b.command("route", records[0].ID)
	if err != nil {
		t.Fatal(out, err)
	}
	query := <-queries
	if query != conversation || !strings.Contains(out, "actual-provider/actual-model") || !strings.Contains(out, "auxiliary") || strings.Contains(out, `"main"`) {
		t.Fatal(out, query)
	}
}

func TestInvalidDraftNameDoesNotCreateARecord(t *testing.T) {
	b := newTestBridge(t)
	if out, err := b.command("new", "--dir", b.project, "--name", "\n"); err == nil {
		t.Fatal(out)
	}
	if got := b.sessions(); len(got) != 0 {
		t.Fatalf("invalid draft created a record: %+v", got)
	}
}

func TestLegacyWriterStateDoesNotGrantRecoveryTrust(t *testing.T) {
	b := newTestBridge(t)
	b.sessions()
	const id = "01912345-6789-7abc-8def-0123456789ab"
	home := filepath.Join(b.root, "codex")
	if err := os.MkdirAll(filepath.Join(home, "sessions"), 0700); err != nil {
		t.Fatal(err)
	}
	meta, _ := json.Marshal(map[string]any{"type": "session_meta", "payload": map[string]string{"id": id, "cwd": b.project, "originator": "codex-tui"}})
	if err := os.WriteFile(filepath.Join(home, "sessions", "rollout-"+id+".jsonl"), meta, 0600); err != nil {
		t.Fatal(err)
	}
	record := session{Version: 2, ID: "0123456789abcdef0123456789abcdef", WorkingDirectory: b.project, CLI: "codex", Mode: "local", Started: true, Recovery: "ready", CodexSessionID: id, ConversationIDSource: "captured", CodexHome: home, CLIVersion: "codex-cli 0.167.0"}
	data, _ := json.Marshal([]session{record})
	if err := os.WriteFile(filepath.Join(b.root, "bridge", "sessions.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(home, "thread-writer-locks"), 0700); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(home, "thread-writer-locks", id+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	out, err := b.command("resume", record.ID)
	if err == nil || !strings.Contains(out, "upstream-observed reference") {
		t.Fatalf("legacy private state was trusted: %s %v", out, err)
	}
}

func TestResumeRefusesAnotherHarborSessionStillStarting(t *testing.T) {
	b := newTestBridge(t)
	b.sessions()
	const conversation = "01912345-6789-7abc-8def-0123456789ab"
	now := time.Now().UTC()
	records := []session{
		{Version: 2, ID: "0123456789abcdef0123456789abcdef", WorkingDirectory: b.project, CLI: "codex", Mode: "local", CodexSessionID: conversation, Recovery: "needs_review", Started: true, LastStartedAt: &now, RunToken: "isolated-starting-launch"},
		{Version: 2, ID: "fedcba9876543210fedcba9876543210", WorkingDirectory: b.project, CLI: "codex", Mode: "local", CodexSessionID: conversation, Recovery: "ready", Started: true},
	}
	data, _ := json.Marshal(records)
	if err := os.WriteFile(filepath.Join(b.root, "bridge", "sessions.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(b.root, "bridge", "running-"+records[0].ID+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	out, err := b.command("resume", records[1].ID)
	if err == nil || !strings.Contains(out, "running in another session") {
		t.Fatalf("starting writer admitted a second resume: %s %v", out, err)
	}
}

func TestNewSessionPreservesSelectedMagpieIdentity(t *testing.T) {
	b := newTestBridge(t)
	rootA, rootB := tempRoot(t), tempRoot(t)
	a := startFactsHelperProcess(t, rootA, map[string]string{"HOME": rootA, "XDG_CONFIG_HOME": filepath.Join(rootA, "config")})
	replacement := startFactsHelperProcess(t, rootB, map[string]string{"HOME": rootB, "XDG_CONFIG_HOME": filepath.Join(rootB, "config")})
	pid := a.pid()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/hello" {
			fmt.Fprint(w, `{"name":"magpie"}`)
			return
		}
		if r.URL.Path == "/v1/models" {
			fmt.Fprint(w, `{"data":[{"id":"test-model"}]}`)
			return
		}
		magpieSelfReport(pid, "serve")(w, r)
	}))
	defer server.Close()
	if out, err := b.command("magpie", "bind", "--address", server.URL); err != nil {
		t.Fatal(out, err)
	}
	path := filepath.Join(b.root, "bridge", "magpie.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var registry magpieRegistry
	if err := json.Unmarshal(before, &registry); err != nil {
		t.Fatal(err)
	}
	pid = replacement.pid()
	out, createErr := b.command("new", "--dir", b.project, "--connection", "magpie", "--instance", registry.Default, "--model", "test-model")
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("new silently rebound the registered Magpie identity")
	}
	if createErr == nil {
		t.Fatalf("replacement started without explicit override: %s", out)
	}
	if !strings.Contains(out, "--override-instance") {
		t.Fatalf("identity change not explained: %s", out)
	}
	listed, err := b.command("sessions")
	if err != nil {
		t.Fatal(listed, err)
	}
	for _, want := range []string{"Needs attention", registry.Default, "test-model"} {
		if !strings.Contains(listed, want) {
			t.Fatalf("missing %q: %s", want, listed)
		}
	}
	for _, s := range b.sessions() {
		if s.InstanceSnapshot == nil || s.InstanceSnapshot.ConfigDir != registry.Instances[0].ConfigDir {
			t.Fatalf("selected identity lost: %+v", s)
		}
	}
}

func TestPlainSessionListExplainsUnavailableDirectory(t *testing.T) {
	b := newTestBridge(t)
	if err := os.WriteFile(filepath.Join(b.root, "bin", "codex"), []byte("invalid executable"), 0700); err != nil {
		t.Fatal(err)
	}
	b.command("create", "--directory", b.project)
	if err := os.Rename(b.project, b.project+"-moved"); err != nil {
		t.Fatal(err)
	}
	out, err := b.command("sessions")
	if err != nil {
		t.Fatal(out, err)
	}
	for _, want := range []string{"Needs attention", "working directory is unavailable", b.sessions()[0].Name} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q: %s", want, out)
		}
	}
}
